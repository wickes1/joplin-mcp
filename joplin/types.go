package joplin

import "time"

// Note is the raw Joplin API note representation.
// Joplin uses int for booleans and int64 (Unix ms) for timestamps.
type Note struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	Body          string `json:"body"`
	ParentID      string `json:"parent_id"`
	IsTodo        int    `json:"is_todo"`
	TodoCompleted int64  `json:"todo_completed"`
	CreatedTime   int64  `json:"created_time"`
	UpdatedTime   int64  `json:"updated_time"`
}

// Folder is the raw Joplin API folder representation.
type Folder struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	ParentID string    `json:"parent_id"`
	Children []*Folder `json:"children,omitempty"`
}

// Tag is the raw Joplin API tag representation.
type Tag struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// PaginatedResponse is a generic wrapper for Joplin paginated list responses.
type PaginatedResponse[T any] struct {
	Items   []T  `json:"items"`
	HasMore bool `json:"has_more"`
}

// SlimNote is the MCP response for list and search operations (minimal fields).
type SlimNote struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	ParentID    string `json:"parent_id"`
	FolderTitle string `json:"folder_title,omitempty"`
	IsTodo      bool   `json:"is_todo"`
	UpdatedTime string `json:"updated_time,omitempty"`
}

// FullNote is the MCP response for read operations (all fields).
type FullNote struct {
	SlimNote
	Body          string   `json:"body"`
	CreatedTime   string   `json:"created_time,omitempty"`
	TodoCompleted *string  `json:"todo_completed"`
	Tags          []string `json:"tags"`
}

// Resource is the raw Joplin API resource (attachment) representation.
type Resource struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Mime        string `json:"mime"`
	Filename    string `json:"filename"`
	Size        int64  `json:"size"`
	UpdatedTime int64  `json:"updated_time"`
}

// ResourceResponse is the MCP response for resource operations (formatted timestamps).
type ResourceResponse struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Mime        string `json:"mime"`
	Filename    string `json:"filename"`
	Size        int64  `json:"size"`
	UpdatedTime string `json:"updated_time"`
}

// ToSlim converts a raw Resource to a ResourceResponse with formatted timestamps.
func (r *Resource) ToSlim() ResourceResponse {
	return ResourceResponse{
		ID: r.ID, Title: r.Title, Mime: r.Mime, Filename: r.Filename,
		Size: r.Size, UpdatedTime: FormatTimestamp(r.UpdatedTime),
	}
}

// FolderUpdateParams holds parameters for updating a folder via the Joplin API.
// Pointer fields allow partial updates (only non-nil fields are sent).
type FolderUpdateParams struct {
	Title    *string `json:"title,omitempty"`
	ParentID *string `json:"parent_id,omitempty"`
}

// BatchResult holds the outcome of a batch operation.
type BatchResult struct {
	Succeeded int              `json:"succeeded"`
	Failed    int              `json:"failed"`
	Errors    []BatchItemError `json:"errors,omitempty"`
}

// BatchItemError records a single failure within a batch operation.
type BatchItemError struct {
	ID    string `json:"id"`
	Error string `json:"error"`
}

// PreviewNote is the MCP response for search operations (slim + body preview).
type PreviewNote struct {
	SlimNote
	Preview string `json:"preview,omitempty"`
}

// FolderTree is the MCP response for folder listing, with computed path.
type FolderTree struct {
	ID       string        `json:"id"`
	Title    string        `json:"title"`
	ParentID string        `json:"parent_id"`
	Path     string        `json:"path"`
	Children []*FolderTree `json:"children"`
}

// NoteCreateParams holds parameters for creating a note via the Joplin API.
type NoteCreateParams struct {
	Title    string `json:"title"`
	Body     string `json:"body,omitempty"`
	ParentID string `json:"parent_id,omitempty"`
	IsTodo   int    `json:"is_todo,omitempty"`
}

// NoteUpdateParams holds parameters for updating a note via the Joplin API.
// Pointer fields allow partial updates (only non-nil fields are sent).
type NoteUpdateParams struct {
	Title    *string `json:"title,omitempty"`
	Body     *string `json:"body,omitempty"`
	ParentID *string `json:"parent_id,omitempty"`
	IsTodo   *int    `json:"is_todo,omitempty"`
}

// FormatTimestamp converts a Unix milliseconds timestamp to ISO 8601 string.
// Returns "" for zero values (omit from response rather than returning epoch).
func FormatTimestamp(ms int64) string {
	if ms == 0 {
		return ""
	}
	return time.UnixMilli(ms).UTC().Format(time.RFC3339)
}

// ToSlim converts a raw Note to a SlimNote. folderTitle is resolved externally.
func (n *Note) ToSlim(folderTitle string) SlimNote {
	return SlimNote{
		ID:          n.ID,
		Title:       n.Title,
		ParentID:    n.ParentID,
		FolderTitle: folderTitle,
		IsTodo:      n.IsTodo != 0,
		UpdatedTime: FormatTimestamp(n.UpdatedTime),
	}
}

// ToFull converts a raw Note to a FullNote. folderTitle and tags are resolved externally.
func (n *Note) ToFull(folderTitle string, tags []string) FullNote {
	full := FullNote{
		SlimNote:    n.ToSlim(folderTitle),
		Body:        n.Body,
		CreatedTime: FormatTimestamp(n.CreatedTime),
		Tags:        tags,
	}
	// todo_completed: 0 → null, >0 → ISO 8601 timestamp string
	if n.TodoCompleted > 0 {
		ts := FormatTimestamp(n.TodoCompleted)
		full.TodoCompleted = &ts
	}
	return full
}

// ToPreview converts a raw Note to a PreviewNote with a body preview.
// The preview is truncated to 200 characters.
func (n *Note) ToPreview(folderTitle string) PreviewNote {
	preview := n.Body
	if len([]rune(preview)) > 200 {
		preview = string([]rune(preview)[:200]) + "..."
	}
	return PreviewNote{
		SlimNote: n.ToSlim(folderTitle),
		Preview:  preview,
	}
}

// StringPtr returns a pointer to the given string value.
func StringPtr(s string) *string { return &s }

// IntPtr returns a pointer to the given int value.
func IntPtr(i int) *int { return &i }
