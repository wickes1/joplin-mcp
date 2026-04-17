package joplin

import "fmt"

// AgentError is a structured error type with agent-friendly messages.
// Both ErrorMsg and Hint are surfaced to the LLM via tool result JSON.
type AgentError struct {
	ErrorMsg string
	Hint     string
}

// Error implements the error interface.
func (e *AgentError) Error() string {
	return e.ErrorMsg
}

// NoteNotFound returns an AgentError for a missing note.
func NoteNotFound(id string) *AgentError {
	return &AgentError{
		ErrorMsg: fmt.Sprintf("Note %s not found.", id),
		Hint:     "Use search_notes or list_notes to find valid note IDs.",
	}
}

// FolderNotFound returns an AgentError for a missing folder by ID.
func FolderNotFound(id string) *AgentError {
	return &AgentError{
		ErrorMsg: fmt.Sprintf("Folder %s not found.", id),
		Hint:     "Use list_folders to see available folders.",
	}
}

// FolderNameNotFound returns an AgentError for a missing folder by name.
func FolderNameNotFound(name string) *AgentError {
	return &AgentError{
		ErrorMsg: fmt.Sprintf("Folder %q not found.", name),
		Hint:     "create_note, update_note, and import_markdown auto-create folders when using folder_name. batch_move_notes requires an existing folder. Use list_folders to see available folders.",
	}
}

// ResourceNotFound returns an AgentError for a missing resource by ID.
func ResourceNotFound(id string) *AgentError {
	return &AgentError{
		ErrorMsg: fmt.Sprintf("Resource %s not found.", id),
		Hint:     "Use list_resources to see available resource IDs.",
	}
}

// TagNotFound returns an AgentError for a missing tag by name.
func TagNotFound(name string) *AgentError {
	return &AgentError{
		ErrorMsg: fmt.Sprintf("Tag %q not found.", name),
		Hint:     "Use list_tags to see available tags.",
	}
}

// JoplinUnavailable returns an AgentError when the Joplin API is unreachable.
func JoplinUnavailable(host string, port int, err error) *AgentError {
	return &AgentError{
		ErrorMsg: fmt.Sprintf("Cannot connect to Joplin at %s:%d. (%v)", host, port, err),
		Hint:     "Ensure Joplin desktop is running and Web Clipper is enabled in Preferences.",
	}
}

// JoplinForbidden returns an AgentError for 403 responses.
func JoplinForbidden() *AgentError {
	return &AgentError{
		ErrorMsg: "Joplin API returned 403 Forbidden.",
		Hint:     "Check that JOPLIN_TOKEN is set correctly. Find it in Joplin > Preferences > Web Clipper.",
	}
}

// InvalidFileExtension returns an AgentError for unsupported file extensions.
func InvalidFileExtension(ext string) *AgentError {
	return &AgentError{
		ErrorMsg: fmt.Sprintf("Only .md files can be imported, got %q.", ext),
		Hint:     "Provide a path to a Markdown (.md) file.",
	}
}
