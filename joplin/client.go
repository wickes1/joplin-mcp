package joplin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// API defines the interface for interacting with the Joplin REST API.
// All tool implementations depend on this interface for testability.
type API interface {
	Ping(ctx context.Context) error
	GetNote(ctx context.Context, id string) (*Note, error)
	GetNoteTags(ctx context.Context, noteID string) ([]Tag, error)
	ListNotes(ctx context.Context, folderID string, page, limit int) (*PaginatedResponse[Note], error)
	SearchNotes(ctx context.Context, query string, page, limit int) (*PaginatedResponse[Note], error)
	CreateNote(ctx context.Context, params NoteCreateParams) (*Note, error)
	UpdateNote(ctx context.Context, id string, params NoteUpdateParams) (*Note, error)
	DeleteNote(ctx context.Context, id string, permanent bool) error
	ListFolders(ctx context.Context) ([]*Folder, error)
	CreateFolder(ctx context.Context, title string, parentID string) (*Folder, error)
	UpdateFolder(ctx context.Context, id string, params FolderUpdateParams) (*Folder, error)
	DeleteFolder(ctx context.Context, id string, permanent bool) error
	ListTags(ctx context.Context) ([]Tag, error)
	CreateTag(ctx context.Context, title string) (*Tag, error)
	DeleteTag(ctx context.Context, id string) error
	TagNote(ctx context.Context, tagID, noteID string) error
	UntagNote(ctx context.Context, tagID, noteID string) error
	GetNotesByTag(ctx context.Context, tagID string, page, limit int) (*PaginatedResponse[Note], error)
	ListResources(ctx context.Context, page, limit int) (*PaginatedResponse[Resource], error)
	GetResource(ctx context.Context, id string) (*Resource, error)
	GetResourceFile(ctx context.Context, id string) ([]byte, error)
	CreateResource(ctx context.Context, filePath, title string) (*Resource, error)
	DeleteResource(ctx context.Context, id string) error
	Host() string
	Port() int
}

// Verify that Client implements API at compile time.
var _ API = (*Client)(nil)

// maxPages is the upper bound on internal pagination loops.
// Exceeding this limit triggers a warning and returns what was collected.
const maxPages = 1000

// Client is an HTTP client for the Joplin REST API.
type Client struct {
	baseURL      string
	token        string
	host         string
	port         int
	httpClient   *http.Client
	readTimeout  time.Duration
	writeTimeout time.Duration
}

// Option is a functional option for configuring a Client.
type Option func(*Client)

// WithReadTimeout sets the per-request timeout for GET operations.
// The default is 30 seconds.
func WithReadTimeout(d time.Duration) Option {
	return func(c *Client) {
		c.readTimeout = d
	}
}

// WithWriteTimeout sets the per-request timeout for POST, PUT, and DELETE operations.
// The default is 120 seconds. Joplin re-renders markdown and re-indexes FTS on writes,
// so write operations can be significantly slower than reads.
func WithWriteTimeout(d time.Duration) Option {
	return func(c *Client) {
		c.writeTimeout = d
	}
}

// NewClient creates a new Joplin API client.
// opts are applied after defaults; unknown options are no-ops by design.
func NewClient(token, host string, port int, opts ...Option) *Client {
	c := &Client{
		baseURL:      fmt.Sprintf("http://%s:%d", host, port),
		token:        token,
		host:         host,
		port:         port,
		httpClient:   &http.Client{}, // no global Timeout; enforced per-request via context
		readTimeout:  30 * time.Second,
		writeTimeout: 120 * time.Second,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Host returns the configured Joplin host.
func (c *Client) Host() string { return c.host }

// Port returns the configured Joplin port.
func (c *Client) Port() int { return c.port }

// isWriteMethod reports whether the HTTP method mutates state on the server.
func isWriteMethod(method string) bool {
	return method == http.MethodPost || method == http.MethodPut || method == http.MethodDelete
}

// doRequest performs an HTTP request to the Joplin API.
// It injects the token, handles error status codes (with one retry on 5xx),
// and decodes the response body into result (if non-nil).
func (c *Client) doRequest(ctx context.Context, method, path string, query url.Values, body any) ([]byte, error) {
	return c.doRequestWithRetry(ctx, method, path, query, body, true)
}

func (c *Client) doRequestWithRetry(ctx context.Context, method, path string, query url.Values, body any, allowRetry bool) ([]byte, error) {
	// Derive a per-attempt context with the appropriate timeout.
	// context.WithTimeout returns the earlier of parent deadline and now+opTimeout,
	// so a tighter caller context (e.g. batch with 30 s budget) still caps correctly.
	opTimeout := c.readTimeout
	if isWriteMethod(method) {
		opTimeout = c.writeTimeout
	}
	// If the caller's context has a tighter deadline (e.g. a batch budget),
	// report that remaining budget in any timeout error rather than the larger
	// per-op default.
	if dl, ok := ctx.Deadline(); ok {
		if remaining := time.Until(dl); remaining > 0 && remaining < opTimeout {
			opTimeout = remaining
		}
	}
	opCtx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()

	// Build URL with token
	rawURL := c.baseURL + path
	params := make(url.Values)
	for k, vs := range query {
		for _, v := range vs {
			params.Add(k, v)
		}
	}
	params.Set("token", c.token)
	rawURL += "?" + params.Encode()

	// Encode body if provided
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encoding request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(opCtx, method, rawURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(opCtx.Err(), context.DeadlineExceeded) {
			return nil, JoplinTimeout(c.host, c.port, isWriteMethod(method), opTimeout)
		}
		return nil, JoplinUnavailable(c.host, c.port, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 100*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	// Status code checks in priority order: 403 → 404 → 5xx (retry once) → 4xx
	switch {
	case resp.StatusCode == http.StatusForbidden:
		return nil, JoplinForbidden()
	case resp.StatusCode == http.StatusNotFound:
		return nil, fmt.Errorf("not found: %s", path)
	case resp.StatusCode >= 500:
		if allowRetry {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(500 * time.Millisecond):
			}
			// Recurse with the ORIGINAL ctx so each retry derives a fresh deadline.
			return c.doRequestWithRetry(ctx, method, path, query, body, false)
		}
		return nil, fmt.Errorf("joplin server error %d: %s", resp.StatusCode, string(respBody))
	case resp.StatusCode >= 400:
		return nil, fmt.Errorf("joplin client error %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// Ping checks that the Joplin API is reachable.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.doRequest(ctx, http.MethodGet, "/ping", nil, nil)
	return err
}

// GetNote fetches a single note by ID.
func (c *Client) GetNote(ctx context.Context, id string) (*Note, error) {
	q := url.Values{"fields": []string{"id,title,body,parent_id,is_todo,todo_completed,created_time,updated_time"}}
	data, err := c.doRequest(ctx, http.MethodGet, "/notes/"+id, q, nil)
	if err != nil {
		return nil, mapNoteError(err, id)
	}
	var note Note
	if err := json.Unmarshal(data, &note); err != nil {
		return nil, fmt.Errorf("decoding note: %w", err)
	}
	return &note, nil
}

// ListNotes fetches a page of notes, optionally filtered by folder ID.
func (c *Client) ListNotes(ctx context.Context, folderID string, page, limit int) (*PaginatedResponse[Note], error) {
	var path string
	if folderID != "" {
		path = "/folders/" + folderID + "/notes"
	} else {
		path = "/notes"
	}
	q := url.Values{
		"fields": []string{"id,title,parent_id,is_todo,updated_time"},
		"page":   []string{strconv.Itoa(page)},
		"limit":  []string{strconv.Itoa(limit)},
	}
	data, err := c.doRequest(ctx, http.MethodGet, path, q, nil)
	if err != nil {
		return nil, err
	}
	var resp PaginatedResponse[Note]
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("decoding notes list: %w", err)
	}
	return &resp, nil
}

// CreateNote creates a new note and returns it.
func (c *Client) CreateNote(ctx context.Context, params NoteCreateParams) (*Note, error) {
	data, err := c.doRequest(ctx, http.MethodPost, "/notes", nil, params)
	if err != nil {
		return nil, err
	}
	var note Note
	if err := json.Unmarshal(data, &note); err != nil {
		return nil, fmt.Errorf("decoding created note: %w", err)
	}
	return &note, nil
}

// UpdateNote applies a partial update to a note.
func (c *Client) UpdateNote(ctx context.Context, id string, params NoteUpdateParams) (*Note, error) {
	data, err := c.doRequest(ctx, http.MethodPut, "/notes/"+id, nil, params)
	if err != nil {
		return nil, mapNoteError(err, id)
	}
	var note Note
	if err := json.Unmarshal(data, &note); err != nil {
		return nil, fmt.Errorf("decoding updated note: %w", err)
	}
	return &note, nil
}

// DeleteNote deletes a note. If permanent is true, bypasses the trash.
func (c *Client) DeleteNote(ctx context.Context, id string, permanent bool) error {
	q := url.Values{}
	if permanent {
		q.Set("permanent", "1")
	}
	_, err := c.doRequest(ctx, http.MethodDelete, "/notes/"+id, q, nil)
	if err != nil {
		return mapNoteError(err, id)
	}
	return nil
}

// SearchNotes performs a full-text search. It requests body in fields to avoid N+1 lookups.
func (c *Client) SearchNotes(ctx context.Context, query string, page, limit int) (*PaginatedResponse[Note], error) {
	q := url.Values{
		"query":  []string{query},
		"page":   []string{strconv.Itoa(page)},
		"limit":  []string{strconv.Itoa(limit)},
		"fields": []string{"id,title,parent_id,is_todo,updated_time,body"},
	}
	data, err := c.doRequest(ctx, http.MethodGet, "/search", q, nil)
	if err != nil {
		return nil, err
	}
	var resp PaginatedResponse[Note]
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("decoding search results: %w", err)
	}
	return &resp, nil
}

// ListFolders fetches all folders, paginating internally.
// Returns the top-level tree structure Joplin provides.
// Pagination is capped at maxPages to prevent unbounded loops on unexpected API behaviour.
func (c *Client) ListFolders(ctx context.Context) ([]*Folder, error) {
	var all []*Folder
	page := 1
	for {
		if page > maxPages {
			slog.Warn("ListFolders: pagination limit reached, returning partial results",
				"pages_fetched", maxPages,
				"folders_collected", len(all),
			)
			break
		}
		q := url.Values{
			"page":  []string{strconv.Itoa(page)},
			"limit": []string{"100"},
		}
		data, err := c.doRequest(ctx, http.MethodGet, "/folders", q, nil)
		if err != nil {
			return nil, err
		}
		var resp PaginatedResponse[*Folder]
		if err := json.Unmarshal(data, &resp); err != nil {
			return nil, fmt.Errorf("decoding folders: %w", err)
		}
		all = append(all, resp.Items...)
		if !resp.HasMore {
			break
		}
		page++
	}
	return all, nil
}

// CreateFolder creates a new folder.
func (c *Client) CreateFolder(ctx context.Context, title, parentID string) (*Folder, error) {
	body := map[string]string{"title": title}
	if parentID != "" {
		body["parent_id"] = parentID
	}
	data, err := c.doRequest(ctx, http.MethodPost, "/folders", nil, body)
	if err != nil {
		return nil, err
	}
	var folder Folder
	if err := json.Unmarshal(data, &folder); err != nil {
		return nil, fmt.Errorf("decoding created folder: %w", err)
	}
	return &folder, nil
}

// DeleteFolder deletes a folder. If permanent is true, bypasses the trash.
func (c *Client) DeleteFolder(ctx context.Context, id string, permanent bool) error {
	q := url.Values{}
	if permanent {
		q.Set("permanent", "1")
	}
	_, err := c.doRequest(ctx, http.MethodDelete, "/folders/"+id, q, nil)
	if err != nil {
		return mapFolderError(err, id)
	}
	return nil
}

// ListTags fetches all tags, paginating internally.
// Pagination is capped at maxPages to prevent unbounded loops on unexpected API behaviour.
func (c *Client) ListTags(ctx context.Context) ([]Tag, error) {
	var all []Tag
	page := 1
	for {
		if page > maxPages {
			slog.Warn("ListTags: pagination limit reached, returning partial results",
				"pages_fetched", maxPages,
				"tags_collected", len(all),
			)
			break
		}
		q := url.Values{
			"page":  []string{strconv.Itoa(page)},
			"limit": []string{"100"},
		}
		data, err := c.doRequest(ctx, http.MethodGet, "/tags", q, nil)
		if err != nil {
			return nil, err
		}
		var resp PaginatedResponse[Tag]
		if err := json.Unmarshal(data, &resp); err != nil {
			return nil, fmt.Errorf("decoding tags: %w", err)
		}
		all = append(all, resp.Items...)
		if !resp.HasMore {
			break
		}
		page++
	}
	return all, nil
}

// CreateTag creates a new tag.
func (c *Client) CreateTag(ctx context.Context, title string) (*Tag, error) {
	data, err := c.doRequest(ctx, http.MethodPost, "/tags", nil, map[string]string{"title": title})
	if err != nil {
		return nil, err
	}
	var tag Tag
	if err := json.Unmarshal(data, &tag); err != nil {
		return nil, fmt.Errorf("decoding created tag: %w", err)
	}
	return &tag, nil
}

// DeleteTag deletes a tag by ID.
func (c *Client) DeleteTag(ctx context.Context, id string) error {
	_, err := c.doRequest(ctx, http.MethodDelete, "/tags/"+id, nil, nil)
	return err
}

// TagNote associates a tag with a note.
func (c *Client) TagNote(ctx context.Context, tagID, noteID string) error {
	_, err := c.doRequest(ctx, http.MethodPost, "/tags/"+tagID+"/notes", nil, map[string]string{"id": noteID})
	return err
}

// UntagNote removes a tag from a note.
func (c *Client) UntagNote(ctx context.Context, tagID, noteID string) error {
	_, err := c.doRequest(ctx, http.MethodDelete, "/tags/"+tagID+"/notes/"+noteID, nil, nil)
	return err
}

// GetNoteTags returns the tags associated with a note.
func (c *Client) GetNoteTags(ctx context.Context, noteID string) ([]Tag, error) {
	data, err := c.doRequest(ctx, http.MethodGet, "/notes/"+noteID+"/tags", nil, nil)
	if err != nil {
		return nil, err
	}
	var resp PaginatedResponse[Tag]
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("decoding note tags: %w", err)
	}
	return resp.Items, nil
}

// GetNotesByTag returns notes associated with a tag, with pagination.
func (c *Client) GetNotesByTag(ctx context.Context, tagID string, page, limit int) (*PaginatedResponse[Note], error) {
	q := url.Values{
		"fields": []string{"id,title,parent_id,is_todo,updated_time"},
		"page":   []string{strconv.Itoa(page)},
		"limit":  []string{strconv.Itoa(limit)},
	}
	data, err := c.doRequest(ctx, http.MethodGet, "/tags/"+tagID+"/notes", q, nil)
	if err != nil {
		return nil, err
	}
	var resp PaginatedResponse[Note]
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("decoding tagged notes: %w", err)
	}
	return &resp, nil
}

// UpdateFolder applies a partial update to a folder.
func (c *Client) UpdateFolder(ctx context.Context, id string, params FolderUpdateParams) (*Folder, error) {
	data, err := c.doRequest(ctx, http.MethodPut, "/folders/"+id, nil, params)
	if err != nil {
		return nil, mapFolderError(err, id)
	}
	var folder Folder
	if err := json.Unmarshal(data, &folder); err != nil {
		return nil, fmt.Errorf("decoding updated folder: %w", err)
	}
	return &folder, nil
}

// ListResources fetches a page of resources (attachments).
func (c *Client) ListResources(ctx context.Context, page, limit int) (*PaginatedResponse[Resource], error) {
	q := url.Values{
		"fields": []string{"id,title,mime,filename,size,updated_time"},
		"page":   []string{strconv.Itoa(page)},
		"limit":  []string{strconv.Itoa(limit)},
	}
	data, err := c.doRequest(ctx, http.MethodGet, "/resources", q, nil)
	if err != nil {
		return nil, err
	}
	var resp PaginatedResponse[Resource]
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("decoding resources list: %w", err)
	}
	return &resp, nil
}

// GetResource fetches a single resource by ID.
func (c *Client) GetResource(ctx context.Context, id string) (*Resource, error) {
	q := url.Values{"fields": []string{"id,title,mime,filename,size,updated_time"}}
	data, err := c.doRequest(ctx, http.MethodGet, "/resources/"+id, q, nil)
	if err != nil {
		return nil, mapResourceError(err, id)
	}
	var resource Resource
	if err := json.Unmarshal(data, &resource); err != nil {
		return nil, fmt.Errorf("decoding resource: %w", err)
	}
	return &resource, nil
}

// GetResourceFile fetches the binary content of a resource.
func (c *Client) GetResourceFile(ctx context.Context, id string) ([]byte, error) {
	data, err := c.doRequest(ctx, http.MethodGet, "/resources/"+id+"/file", nil, nil)
	if err != nil {
		return nil, mapResourceError(err, id)
	}
	return data, nil
}

// CreateResource uploads a file as a new Joplin resource via multipart form.
// A write-timeout context is applied because large uploads combined with
// Joplin's post-upload indexing can exceed a read-oriented deadline.
func (c *Client) CreateResource(ctx context.Context, filePath, title string) (*Resource, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("opening file %q: %w", filePath, err)
	}
	defer f.Close()

	// Build multipart form
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Add the "props" field with JSON metadata
	props := map[string]string{"title": title}
	propsJSON, _ := json.Marshal(props)
	if err := writer.WriteField("props", string(propsJSON)); err != nil {
		return nil, fmt.Errorf("writing props field: %w", err)
	}

	// Add the file part
	part, err := writer.CreateFormFile("data", filepath.Base(filePath))
	if err != nil {
		return nil, fmt.Errorf("creating form file: %w", err)
	}
	if _, err := io.Copy(part, f); err != nil {
		return nil, fmt.Errorf("copying file data: %w", err)
	}
	writer.Close()

	// Derive a write-timeout context for the upload request, honoring a tighter
	// caller deadline so any timeout error reports the budget actually applied.
	uploadTimeout := c.writeTimeout
	if dl, ok := ctx.Deadline(); ok {
		if remaining := time.Until(dl); remaining > 0 && remaining < uploadTimeout {
			uploadTimeout = remaining
		}
	}
	uploadCtx, cancel := context.WithTimeout(ctx, uploadTimeout)
	defer cancel()

	// Build URL with token
	rawURL := fmt.Sprintf("%s/resources?token=%s", c.baseURL, url.QueryEscape(c.token))
	req, err := http.NewRequestWithContext(uploadCtx, http.MethodPost, rawURL, &buf)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(uploadCtx.Err(), context.DeadlineExceeded) {
			return nil, JoplinTimeout(c.host, c.port, true, uploadTimeout)
		}
		return nil, JoplinUnavailable(c.host, c.port, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 100*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("joplin error %d creating resource: %s", resp.StatusCode, string(respBody))
	}

	var resource Resource
	if err := json.Unmarshal(respBody, &resource); err != nil {
		return nil, fmt.Errorf("decoding created resource: %w", err)
	}
	return &resource, nil
}

// DeleteResource deletes a resource by ID.
func (c *Client) DeleteResource(ctx context.Context, id string) error {
	_, err := c.doRequest(ctx, http.MethodDelete, "/resources/"+id, nil, nil)
	if err != nil {
		return mapResourceError(err, id)
	}
	return nil
}

// mapNoteError converts "not found" API errors to NoteNotFound AgentErrors.
func mapNoteError(err error, id string) error {
	if isNotFound(err) {
		return NoteNotFound(id)
	}
	return err
}

// mapFolderError converts "not found" API errors to FolderNotFound AgentErrors.
func mapFolderError(err error, id string) error {
	if isNotFound(err) {
		return FolderNotFound(id)
	}
	return err
}

// mapResourceError converts "not found" API errors to ResourceNotFound AgentErrors.
func mapResourceError(err error, id string) error {
	if isNotFound(err) {
		return ResourceNotFound(id)
	}
	return err
}

// isNotFound reports whether an error from doRequest is a 404 "not found".
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	// doRequest returns fmt.Errorf("not found: %s", path) for 404s
	// We check the prefix to avoid importing errors just for this.
	return len(err.Error()) >= 9 && err.Error()[:9] == "not found"
}
