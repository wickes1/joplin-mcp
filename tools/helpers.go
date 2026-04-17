package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Wickes1/joplin-mcp/joplin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// toolError returns a CallToolResult with IsError=true and structured JSON content.
func toolError(errMsg, hint string) (*mcp.CallToolResult, any, error) {
	payload := map[string]string{
		"error": errMsg,
		"hint":  hint,
	}
	data, _ := json.Marshal(payload)
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
		IsError: true,
	}, nil, nil
}

// toolErrorFromAgent returns a CallToolResult from an AgentError.
func toolErrorFromAgent(ae *joplin.AgentError) (*mcp.CallToolResult, any, error) {
	return toolError(ae.ErrorMsg, ae.Hint)
}

// toolSuccess marshals v to JSON and returns a successful CallToolResult.
func toolSuccess(v any) (*mcp.CallToolResult, any, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return toolError("failed to serialize response", err.Error())
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
	}, nil, nil
}

// handleErr converts any error into a tool error result, using AgentError formatting when available.
func handleErr(err error) (*mcp.CallToolResult, any, error) {
	if ae, ok := err.(*joplin.AgentError); ok {
		return toolErrorFromAgent(ae)
	}
	return toolError(err.Error(), "")
}

// resolveFolderID resolves a folder from ID, name, or auto-creates one.
// If both folderID and folderName are empty, returns ("", "", nil).
func resolveFolderID(ctx context.Context, c joplin.API, fc *FolderCache,
	folderID, folderName string, autoCreate bool) (string, string, error) {
	if folderID != "" {
		title := fc.GetTitle(folderID)
		return folderID, title, nil
	}
	if folderName == "" {
		return "", "", nil
	}
	existing := fc.FindByName(folderName)
	if existing != nil {
		return existing.ID, existing.Title, nil
	}
	if !autoCreate {
		return "", "", joplin.FolderNameNotFound(folderName)
	}
	newFolder, err := c.CreateFolder(ctx, folderName, "")
	if err != nil {
		return "", "", fmt.Errorf("failed to create folder %q: %w", folderName, err)
	}
	fc.Invalidate()
	return newFolder.ID, newFolder.Title, nil
}

// applyTags applies a list of tag names to a note, auto-creating tags as needed.
// Returns the list of successfully applied tag titles and any warnings.
func applyTags(ctx context.Context, c joplin.API, noteID string, tagNames []string) ([]string, []string) {
	if len(tagNames) == 0 {
		return nil, nil
	}
	allTags, err := c.ListTags(ctx)
	if err != nil {
		return nil, []string{fmt.Sprintf("failed to list tags: %s", err.Error())}
	}
	var applied, warnings []string
	for _, name := range tagNames {
		tag := FindTagByName(allTags, name)
		if tag == nil {
			newTag, err := c.CreateTag(ctx, name)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("failed to create tag %q: %s", name, err.Error()))
				continue
			}
			tag = newTag
			allTags = append(allTags, *newTag)
		}
		if err := c.TagNote(ctx, tag.ID, noteID); err != nil {
			warnings = append(warnings, fmt.Sprintf("failed to apply tag %q: %s", name, err.Error()))
			continue
		}
		applied = append(applied, tag.Title)
	}
	return applied, warnings
}

// validateAbsPath checks that p is an absolute path without ".." components.
func validateAbsPath(p string) error {
	if !filepath.IsAbs(p) {
		return fmt.Errorf("path must be absolute, got %q", p)
	}
	if strings.Contains(p, "..") {
		return fmt.Errorf("path must not contain '..', got %q", p)
	}
	return nil
}

// FindTagByName does a case-insensitive search for a tag by name.
// Returns nil if not found.
func FindTagByName(tags []joplin.Tag, name string) *joplin.Tag {
	lower := strings.ToLower(name)
	for i := range tags {
		if strings.ToLower(tags[i].Title) == lower {
			return &tags[i]
		}
	}
	return nil
}

// folderCacheEntry holds cached folder data with expiry.
type folderCacheEntry struct {
	folders   []*joplin.Folder
	flatByID  map[string]*joplin.Folder  // id → folder
	flatByName map[string]*joplin.Folder // lowercase title → folder
	expiresAt time.Time
}

// FolderCache is a session-level, thread-safe cache for folder data with a 30s TTL.
type FolderCache struct {
	mu     sync.RWMutex
	client joplin.API
	entry  *folderCacheEntry
}

// NewFolderCache creates a new FolderCache backed by the given API client.
func NewFolderCache(client joplin.API) *FolderCache {
	return &FolderCache{client: client}
}

// Invalidate clears the cache, forcing a reload on next access.
func (fc *FolderCache) Invalidate() {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	fc.entry = nil
}

// load fetches and caches folders if the cache is expired or missing.
// Must be called with at least a read lock; acquires write lock if refresh needed.
func (fc *FolderCache) load() (*folderCacheEntry, error) {
	// Fast path: valid cache exists
	fc.mu.RLock()
	if fc.entry != nil && time.Now().Before(fc.entry.expiresAt) {
		e := fc.entry
		fc.mu.RUnlock()
		return e, nil
	}
	fc.mu.RUnlock()

	// Slow path: acquire write lock and refresh
	fc.mu.Lock()
	defer fc.mu.Unlock()

	// Double-check after acquiring write lock
	if fc.entry != nil && time.Now().Before(fc.entry.expiresAt) {
		return fc.entry, nil
	}

	folders, err := fc.client.ListFolders(context.Background())
	if err != nil {
		return nil, err
	}

	entry := &folderCacheEntry{
		folders:    folders,
		flatByID:   make(map[string]*joplin.Folder),
		flatByName: make(map[string]*joplin.Folder),
		expiresAt:  time.Now().Add(30 * time.Second),
	}
	flattenFolders(folders, entry)
	fc.entry = entry
	return entry, nil
}

// flattenFolders recursively flattens a folder tree into the entry's maps.
func flattenFolders(folders []*joplin.Folder, entry *folderCacheEntry) {
	for _, f := range folders {
		if f == nil {
			continue
		}
		entry.flatByID[f.ID] = f
		entry.flatByName[strings.ToLower(f.Title)] = f
		if len(f.Children) > 0 {
			flattenFolders(f.Children, entry)
		}
	}
}

// GetTitle returns a folder's title by ID. Returns "" if not found.
func (fc *FolderCache) GetTitle(id string) string {
	entry, err := fc.load()
	if err != nil || entry == nil {
		return ""
	}
	if f, ok := entry.flatByID[id]; ok {
		return f.Title
	}
	return ""
}

// FindByName looks up a folder by name (case-insensitive). Returns nil if not found.
func (fc *FolderCache) FindByName(name string) *joplin.Folder {
	entry, err := fc.load()
	if err != nil || entry == nil {
		return nil
	}
	return entry.flatByName[strings.ToLower(name)]
}

// ComputePath computes the full path of a folder by ID (e.g. "Work/Projects").
// Returns the folder title alone if it has no parent, or "" if not found.
func (fc *FolderCache) ComputePath(id string) string {
	entry, err := fc.load()
	if err != nil || entry == nil {
		return ""
	}
	return computePath(id, entry.flatByID)
}

// computePath recursively builds a slash-separated path from folder ID.
func computePath(id string, byID map[string]*joplin.Folder) string {
	f, ok := byID[id]
	if !ok {
		return ""
	}
	if f.ParentID == "" {
		return f.Title
	}
	parent := computePath(f.ParentID, byID)
	if parent == "" {
		return f.Title
	}
	return parent + "/" + f.Title
}

// AllFolders returns all folders from the cache (loads if needed).
func (fc *FolderCache) AllFolders() ([]*joplin.Folder, error) {
	entry, err := fc.load()
	if err != nil {
		return nil, err
	}
	return entry.folders, nil
}
