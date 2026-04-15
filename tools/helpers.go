package tools

import (
	"context"
	"encoding/json"
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
	client *joplin.Client
	entry  *folderCacheEntry
}

// NewFolderCache creates a new FolderCache backed by the given client.
func NewFolderCache(client *joplin.Client) *FolderCache {
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
