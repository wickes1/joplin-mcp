package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Wickes1/joplin-mcp/joplin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestToolError verifies that toolError returns IsError=true with correct JSON structure.
func TestToolError(t *testing.T) {
	result, out, err := toolError("Note abc not found.", "Use list_notes to find valid note IDs.")

	if err != nil {
		t.Fatalf("toolError() returned unexpected error: %v", err)
	}
	if out != nil {
		t.Errorf("toolError() out = %v, want nil", out)
	}
	if result == nil {
		t.Fatal("toolError() result is nil")
	}
	if !result.IsError {
		t.Error("toolError() result.IsError = false, want true")
	}
	if len(result.Content) != 1 {
		t.Fatalf("toolError() len(Content) = %d, want 1", len(result.Content))
	}

	// Verify Content[0] is *mcp.TextContent
	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("Content[0] is %T, want *mcp.TextContent", result.Content[0])
	}

	// Verify JSON structure
	var payload map[string]string
	if err := json.Unmarshal([]byte(tc.Text), &payload); err != nil {
		t.Fatalf("Content[0].Text is not valid JSON: %v\nText: %s", err, tc.Text)
	}
	if payload["error"] != "Note abc not found." {
		t.Errorf("payload[\"error\"] = %q, want %q", payload["error"], "Note abc not found.")
	}
	if payload["hint"] != "Use list_notes to find valid note IDs." {
		t.Errorf("payload[\"hint\"] = %q, want %q", payload["hint"], "Use list_notes to find valid note IDs.")
	}
}

// TestToolSuccess verifies that toolSuccess returns IsError=false with marshaled JSON.
func TestToolSuccess(t *testing.T) {
	payload := map[string]any{
		"id":    "abc123",
		"title": "My Note",
	}

	result, out, err := toolSuccess(payload)

	if err != nil {
		t.Fatalf("toolSuccess() returned unexpected error: %v", err)
	}
	if out != nil {
		t.Errorf("toolSuccess() out = %v, want nil", out)
	}
	if result == nil {
		t.Fatal("toolSuccess() result is nil")
	}
	if result.IsError {
		t.Error("toolSuccess() result.IsError = true, want false")
	}
	if len(result.Content) != 1 {
		t.Fatalf("toolSuccess() len(Content) = %d, want 1", len(result.Content))
	}

	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("Content[0] is %T, want *mcp.TextContent", result.Content[0])
	}

	var decoded map[string]any
	if err := json.Unmarshal([]byte(tc.Text), &decoded); err != nil {
		t.Fatalf("Content[0].Text is not valid JSON: %v\nText: %s", err, tc.Text)
	}
	if decoded["id"] != "abc123" {
		t.Errorf("decoded[\"id\"] = %v, want %q", decoded["id"], "abc123")
	}
	if decoded["title"] != "My Note" {
		t.Errorf("decoded[\"title\"] = %v, want %q", decoded["title"], "My Note")
	}
}

// TestFindTagByName verifies case-insensitive lookup and nil return for missing tags.
func TestFindTagByName(t *testing.T) {
	tags := []joplin.Tag{
		{ID: "t1", Title: "Project"},
		{ID: "t2", Title: "urgent"},
		{ID: "t3", Title: "Work-in-Progress"},
	}

	tests := []struct {
		name     string
		query    string
		wantID   string
		wantNil  bool
	}{
		{
			name:    "exact match lowercase",
			query:   "urgent",
			wantID:  "t2",
		},
		{
			name:    "case-insensitive match",
			query:   "project",
			wantID:  "t1",
		},
		{
			name:    "uppercase query",
			query:   "URGENT",
			wantID:  "t2",
		},
		{
			name:    "mixed case with hyphens",
			query:   "work-in-progress",
			wantID:  "t3",
		},
		{
			name:    "missing tag returns nil",
			query:   "nonexistent",
			wantNil: true,
		},
		{
			name:    "empty query returns nil",
			query:   "",
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FindTagByName(tags, tt.query)
			if tt.wantNil {
				if got != nil {
					t.Errorf("FindTagByName(%q) = %v, want nil", tt.query, got)
				}
				return
			}
			if got == nil {
				t.Fatalf("FindTagByName(%q) = nil, want tag with ID %q", tt.query, tt.wantID)
			}
			if got.ID != tt.wantID {
				t.Errorf("FindTagByName(%q).ID = %q, want %q", tt.query, got.ID, tt.wantID)
			}
		})
	}
}

// TestHandleErr verifies that handleErr produces correct output for AgentError vs regular error.
func TestHandleErr(t *testing.T) {
	t.Run("AgentError includes hint", func(t *testing.T) {
		ae := joplin.NoteNotFound("abc")
		result, _, err := handleErr(ae)
		if err != nil {
			t.Fatalf("handleErr returned unexpected error: %v", err)
		}
		if !result.IsError {
			t.Error("result.IsError = false, want true")
		}
		tc := result.Content[0].(*mcp.TextContent)
		var payload map[string]string
		if err := json.Unmarshal([]byte(tc.Text), &payload); err != nil {
			t.Fatalf("failed to parse JSON: %v", err)
		}
		if !strings.Contains(payload["error"], "abc") {
			t.Errorf("error %q should contain note ID 'abc'", payload["error"])
		}
		if payload["hint"] == "" {
			t.Error("hint should not be empty for AgentError")
		}
	})

	t.Run("regular error has empty hint", func(t *testing.T) {
		regularErr := errors.New("something went wrong")
		result, _, err := handleErr(regularErr)
		if err != nil {
			t.Fatalf("handleErr returned unexpected error: %v", err)
		}
		if !result.IsError {
			t.Error("result.IsError = false, want true")
		}
		tc := result.Content[0].(*mcp.TextContent)
		var payload map[string]string
		if err := json.Unmarshal([]byte(tc.Text), &payload); err != nil {
			t.Fatalf("failed to parse JSON: %v", err)
		}
		if payload["error"] != "something went wrong" {
			t.Errorf("error = %q, want %q", payload["error"], "something went wrong")
		}
		if payload["hint"] != "" {
			t.Errorf("hint = %q, want empty for regular error", payload["hint"])
		}
	})
}

// TestResolveFolderID verifies folder resolution by ID, by name, and auto-create.
func TestResolveFolderID(t *testing.T) {
	ctx := context.Background()

	t.Run("by ID returns ID and title from cache", func(t *testing.T) {
		mock := NewMockAPI()
		mock.Folders = []*joplin.Folder{
			{ID: "f1", Title: "Work"},
		}
		fc := NewFolderCache(mock)

		id, title, err := resolveFolderID(ctx, mock, fc, "f1", "", false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id != "f1" {
			t.Errorf("id = %q, want %q", id, "f1")
		}
		if title != "Work" {
			t.Errorf("title = %q, want %q", title, "Work")
		}
	})

	t.Run("by name found", func(t *testing.T) {
		mock := NewMockAPI()
		mock.Folders = []*joplin.Folder{
			{ID: "f1", Title: "Work"},
			{ID: "f2", Title: "Personal"},
		}
		fc := NewFolderCache(mock)

		id, title, err := resolveFolderID(ctx, mock, fc, "", "Personal", false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id != "f2" {
			t.Errorf("id = %q, want %q", id, "f2")
		}
		if title != "Personal" {
			t.Errorf("title = %q, want %q", title, "Personal")
		}
	})

	t.Run("by name not found without auto-create returns error", func(t *testing.T) {
		mock := NewMockAPI()
		mock.Folders = []*joplin.Folder{
			{ID: "f1", Title: "Work"},
		}
		fc := NewFolderCache(mock)

		_, _, err := resolveFolderID(ctx, mock, fc, "", "NonExistent", false)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		ae, ok := err.(*joplin.AgentError)
		if !ok {
			t.Fatalf("expected *AgentError, got %T", err)
		}
		if !strings.Contains(ae.ErrorMsg, "NonExistent") {
			t.Errorf("error %q should contain folder name", ae.ErrorMsg)
		}
	})

	t.Run("by name auto-create", func(t *testing.T) {
		mock := NewMockAPI()
		mock.Folders = []*joplin.Folder{
			{ID: "f1", Title: "Work"},
		}
		fc := NewFolderCache(mock)

		id, title, err := resolveFolderID(ctx, mock, fc, "", "NewFolder", true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id == "" {
			t.Error("expected a non-empty folder ID after auto-create")
		}
		if title != "NewFolder" {
			t.Errorf("title = %q, want %q", title, "NewFolder")
		}
	})

	t.Run("both empty returns empty strings", func(t *testing.T) {
		mock := NewMockAPI()
		fc := NewFolderCache(mock)

		id, title, err := resolveFolderID(ctx, mock, fc, "", "", false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id != "" || title != "" {
			t.Errorf("expected empty id and title, got id=%q title=%q", id, title)
		}
	})
}

// TestApplyTags verifies tag application logic.
func TestApplyTags(t *testing.T) {
	ctx := context.Background()

	t.Run("empty list returns nil", func(t *testing.T) {
		mock := NewMockAPI()
		mock.Notes["n1"] = &joplin.Note{ID: "n1", Title: "Test"}

		applied, warnings := applyTags(ctx, mock, "n1", nil)
		if len(applied) != 0 {
			t.Errorf("expected 0 applied tags, got %d", len(applied))
		}
		if len(warnings) != 0 {
			t.Errorf("expected 0 warnings, got %d", len(warnings))
		}
	})

	t.Run("new tags are auto-created and applied", func(t *testing.T) {
		mock := NewMockAPI()
		mock.Notes["n1"] = &joplin.Note{ID: "n1", Title: "Test"}

		applied, warnings := applyTags(ctx, mock, "n1", []string{"urgent", "review"})
		if len(applied) != 2 {
			t.Fatalf("expected 2 applied tags, got %d", len(applied))
		}
		if len(warnings) != 0 {
			t.Errorf("expected 0 warnings, got %v", warnings)
		}
		// Verify tags were created in the mock
		tags, _ := mock.ListTags(ctx)
		if len(tags) != 2 {
			t.Errorf("expected 2 tags in mock, got %d", len(tags))
		}
	})

	t.Run("existing tags are reused", func(t *testing.T) {
		mock := NewMockAPI()
		mock.Notes["n1"] = &joplin.Note{ID: "n1", Title: "Test"}
		mock.Tags["t1"] = &joplin.Tag{ID: "t1", Title: "existing"}

		applied, warnings := applyTags(ctx, mock, "n1", []string{"existing"})
		if len(applied) != 1 {
			t.Fatalf("expected 1 applied tag, got %d", len(applied))
		}
		if len(warnings) != 0 {
			t.Errorf("expected 0 warnings, got %v", warnings)
		}
		if applied[0] != "existing" {
			t.Errorf("applied[0] = %q, want %q", applied[0], "existing")
		}
	})
}

// TestValidateAbsPath verifies path validation.
func TestValidateAbsPath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"absolute path ok", "/tmp/test", false},
		{"relative path fails", "tmp/test", true},
		{"dot-dot fails", "/tmp/../etc/passwd", true},
		{"root path ok", "/", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAbsPath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateAbsPath(%q) error = %v, wantErr = %v", tt.path, err, tt.wantErr)
			}
		})
	}
}

// TestSanitizeFilename verifies filename sanitization.
func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"special chars replaced", "my:note/with*chars", "my_note_with_chars"},
		{"pipe and quotes", `say "hello" | world`, "say _hello_ _ world"},
		{"angle brackets", "note<1>", "note_1_"},
		{"backslash", `path\to\file`, "path_to_file"},
		{"question mark", "what?", "what_"},
		{"leading/trailing dots stripped", "..hidden..", "hidden"},
		{"leading/trailing spaces stripped", "  spaced  ", "spaced"},
		{"long name truncated", strings.Repeat("a", 250), strings.Repeat("a", 200)},
		{"empty input", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeFilename(tt.input)
			if got != tt.expected {
				t.Errorf("sanitizeFilename(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
