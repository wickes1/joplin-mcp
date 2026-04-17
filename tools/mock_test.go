package tools

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/Wickes1/joplin-mcp/joplin"
)

// MockAPI is an in-memory implementation of joplin.API for unit testing.
// It stores notes, folders, tags, resources, and tag-note associations in maps.
type MockAPI struct {
	mu sync.RWMutex

	Notes     map[string]*joplin.Note
	Folders   []*joplin.Folder
	Tags      map[string]*joplin.Tag
	Resources map[string]*joplin.Resource
	// ResourceFiles maps resource ID to binary content.
	ResourceFiles map[string][]byte
	// NoteTags maps note ID to a set of tag IDs.
	NoteTags map[string]map[string]bool

	nextID int
}

// NewMockAPI creates a MockAPI with initialized maps.
func NewMockAPI() *MockAPI {
	return &MockAPI{
		Notes:         make(map[string]*joplin.Note),
		Folders:       nil,
		Tags:          make(map[string]*joplin.Tag),
		Resources:     make(map[string]*joplin.Resource),
		ResourceFiles: make(map[string][]byte),
		NoteTags:      make(map[string]map[string]bool),
		nextID:        1,
	}
}

// genID generates a unique mock ID.
func (m *MockAPI) genID() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := fmt.Sprintf("mock-%d", m.nextID)
	m.nextID++
	return id
}

// Ping always succeeds.
func (m *MockAPI) Ping(_ context.Context) error {
	return nil
}

// Host returns a test host.
func (m *MockAPI) Host() string { return "localhost" }

// Port returns a test port.
func (m *MockAPI) Port() int { return 41184 }

// GetNote returns a note from the map or NoteNotFound.
func (m *MockAPI) GetNote(_ context.Context, id string) (*joplin.Note, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n, ok := m.Notes[id]
	if !ok {
		return nil, joplin.NoteNotFound(id)
	}
	// Return a copy to avoid test mutations
	copy := *n
	return &copy, nil
}

// GetNoteTags returns the tags associated with a note.
func (m *MockAPI) GetNoteTags(_ context.Context, noteID string) ([]joplin.Tag, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	tagIDs, ok := m.NoteTags[noteID]
	if !ok {
		return nil, nil
	}
	var tags []joplin.Tag
	for tagID := range tagIDs {
		if t, ok := m.Tags[tagID]; ok {
			tags = append(tags, *t)
		}
	}
	return tags, nil
}

// ListNotes returns notes optionally filtered by folderID, with pagination.
func (m *MockAPI) ListNotes(_ context.Context, folderID string, page, limit int) (*joplin.PaginatedResponse[joplin.Note], error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var all []joplin.Note
	for _, n := range m.Notes {
		if folderID == "" || n.ParentID == folderID {
			all = append(all, *n)
		}
	}

	start := (page - 1) * limit
	if start >= len(all) {
		return &joplin.PaginatedResponse[joplin.Note]{Items: nil, HasMore: false}, nil
	}
	end := start + limit
	hasMore := false
	if end < len(all) {
		hasMore = true
	} else {
		end = len(all)
	}

	return &joplin.PaginatedResponse[joplin.Note]{
		Items:   all[start:end],
		HasMore: hasMore,
	}, nil
}

// SearchNotes does a simple substring match on title and body.
func (m *MockAPI) SearchNotes(_ context.Context, query string, page, limit int) (*joplin.PaginatedResponse[joplin.Note], error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	lower := strings.ToLower(query)
	var matches []joplin.Note
	for _, n := range m.Notes {
		if strings.Contains(strings.ToLower(n.Title), lower) ||
			strings.Contains(strings.ToLower(n.Body), lower) {
			matches = append(matches, *n)
		}
	}

	start := (page - 1) * limit
	if start >= len(matches) {
		return &joplin.PaginatedResponse[joplin.Note]{Items: nil, HasMore: false}, nil
	}
	end := start + limit
	hasMore := false
	if end < len(matches) {
		hasMore = true
	} else {
		end = len(matches)
	}

	return &joplin.PaginatedResponse[joplin.Note]{
		Items:   matches[start:end],
		HasMore: hasMore,
	}, nil
}

// CreateNote generates an ID, stores the note, and returns it.
func (m *MockAPI) CreateNote(_ context.Context, params joplin.NoteCreateParams) (*joplin.Note, error) {
	id := m.genID()
	note := &joplin.Note{
		ID:          id,
		Title:       params.Title,
		Body:        params.Body,
		ParentID:    params.ParentID,
		IsTodo:      params.IsTodo,
		CreatedTime: 1700000000000,
		UpdatedTime: 1700000001000,
	}
	m.mu.Lock()
	m.Notes[id] = note
	m.mu.Unlock()
	copy := *note
	return &copy, nil
}

// UpdateNote modifies a stored note with the provided params.
func (m *MockAPI) UpdateNote(_ context.Context, id string, params joplin.NoteUpdateParams) (*joplin.Note, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, ok := m.Notes[id]
	if !ok {
		return nil, joplin.NoteNotFound(id)
	}
	if params.Title != nil {
		n.Title = *params.Title
	}
	if params.Body != nil {
		n.Body = *params.Body
	}
	if params.ParentID != nil {
		n.ParentID = *params.ParentID
	}
	if params.IsTodo != nil {
		n.IsTodo = *params.IsTodo
	}
	n.UpdatedTime = 1700000002000
	copy := *n
	return &copy, nil
}

// DeleteNote removes a note from the map.
func (m *MockAPI) DeleteNote(_ context.Context, id string, _ bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.Notes[id]; !ok {
		return joplin.NoteNotFound(id)
	}
	delete(m.Notes, id)
	delete(m.NoteTags, id)
	return nil
}

// ListFolders returns the stored folder tree.
func (m *MockAPI) ListFolders(_ context.Context) ([]*joplin.Folder, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.Folders, nil
}

// CreateFolder generates an ID, stores the folder, and returns it.
func (m *MockAPI) CreateFolder(_ context.Context, title, parentID string) (*joplin.Folder, error) {
	id := m.genID()
	folder := &joplin.Folder{
		ID:       id,
		Title:    title,
		ParentID: parentID,
	}
	m.mu.Lock()
	m.Folders = append(m.Folders, folder)
	m.mu.Unlock()
	return folder, nil
}

// UpdateFolder modifies a stored folder.
func (m *MockAPI) UpdateFolder(_ context.Context, id string, params joplin.FolderUpdateParams) (*joplin.Folder, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, f := range m.Folders {
		if f.ID == id {
			if params.Title != nil {
				f.Title = *params.Title
			}
			if params.ParentID != nil {
				f.ParentID = *params.ParentID
			}
			return f, nil
		}
	}
	return nil, joplin.FolderNotFound(id)
}

// DeleteFolder removes a folder from the list.
func (m *MockAPI) DeleteFolder(_ context.Context, id string, _ bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, f := range m.Folders {
		if f.ID == id {
			m.Folders = append(m.Folders[:i], m.Folders[i+1:]...)
			return nil
		}
	}
	return joplin.FolderNotFound(id)
}

// ListTags returns all stored tags.
func (m *MockAPI) ListTags(_ context.Context) ([]joplin.Tag, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var tags []joplin.Tag
	for _, t := range m.Tags {
		tags = append(tags, *t)
	}
	return tags, nil
}

// CreateTag generates an ID, stores the tag, and returns it.
func (m *MockAPI) CreateTag(_ context.Context, title string) (*joplin.Tag, error) {
	id := m.genID()
	tag := &joplin.Tag{ID: id, Title: title}
	m.mu.Lock()
	m.Tags[id] = tag
	m.mu.Unlock()
	return tag, nil
}

// DeleteTag removes a tag from the map.
func (m *MockAPI) DeleteTag(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.Tags[id]; !ok {
		return joplin.TagNotFound(id)
	}
	delete(m.Tags, id)
	return nil
}

// TagNote associates a tag with a note.
func (m *MockAPI) TagNote(_ context.Context, tagID, noteID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.Tags[tagID]; !ok {
		return joplin.TagNotFound(tagID)
	}
	if _, ok := m.Notes[noteID]; !ok {
		return joplin.NoteNotFound(noteID)
	}
	if m.NoteTags[noteID] == nil {
		m.NoteTags[noteID] = make(map[string]bool)
	}
	m.NoteTags[noteID][tagID] = true
	return nil
}

// UntagNote removes a tag from a note.
func (m *MockAPI) UntagNote(_ context.Context, tagID, noteID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if tags, ok := m.NoteTags[noteID]; ok {
		delete(tags, tagID)
	}
	return nil
}

// GetNotesByTag returns notes that have a given tag.
func (m *MockAPI) GetNotesByTag(_ context.Context, tagID string, page, limit int) (*joplin.PaginatedResponse[joplin.Note], error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var notes []joplin.Note
	for noteID, tagIDs := range m.NoteTags {
		if tagIDs[tagID] {
			if n, ok := m.Notes[noteID]; ok {
				notes = append(notes, *n)
			}
		}
	}

	start := (page - 1) * limit
	if start >= len(notes) {
		return &joplin.PaginatedResponse[joplin.Note]{Items: nil, HasMore: false}, nil
	}
	end := start + limit
	hasMore := false
	if end < len(notes) {
		hasMore = true
	} else {
		end = len(notes)
	}

	return &joplin.PaginatedResponse[joplin.Note]{
		Items:   notes[start:end],
		HasMore: hasMore,
	}, nil
}

// ListResources returns a page of stored resources.
func (m *MockAPI) ListResources(_ context.Context, page, limit int) (*joplin.PaginatedResponse[joplin.Resource], error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var all []joplin.Resource
	for _, r := range m.Resources {
		all = append(all, *r)
	}

	start := (page - 1) * limit
	if start >= len(all) {
		return &joplin.PaginatedResponse[joplin.Resource]{Items: nil, HasMore: false}, nil
	}
	end := start + limit
	hasMore := false
	if end < len(all) {
		hasMore = true
	} else {
		end = len(all)
	}

	return &joplin.PaginatedResponse[joplin.Resource]{
		Items:   all[start:end],
		HasMore: hasMore,
	}, nil
}

// GetResource returns a resource by ID.
func (m *MockAPI) GetResource(_ context.Context, id string) (*joplin.Resource, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.Resources[id]
	if !ok {
		return nil, joplin.ResourceNotFound(id)
	}
	copy := *r
	return &copy, nil
}

// GetResourceFile returns the binary content for a resource.
func (m *MockAPI) GetResourceFile(_ context.Context, id string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	data, ok := m.ResourceFiles[id]
	if !ok {
		return nil, joplin.ResourceNotFound(id)
	}
	return data, nil
}

// CreateResource stores a resource (reads from the real filesystem for the upload case).
func (m *MockAPI) CreateResource(_ context.Context, filePath, title string) (*joplin.Resource, error) {
	id := m.genID()
	resource := &joplin.Resource{
		ID:       id,
		Title:    title,
		Filename: filePath,
		Mime:     "application/octet-stream",
		Size:     0,
	}
	m.mu.Lock()
	m.Resources[id] = resource
	m.mu.Unlock()
	return resource, nil
}

// DeleteResource removes a resource from the map.
func (m *MockAPI) DeleteResource(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.Resources[id]; !ok {
		return joplin.ResourceNotFound(id)
	}
	delete(m.Resources, id)
	delete(m.ResourceFiles, id)
	return nil
}

// Verify MockAPI implements joplin.API at compile time.
var _ joplin.API = (*MockAPI)(nil)
