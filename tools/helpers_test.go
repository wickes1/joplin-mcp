package tools

import (
	"encoding/json"
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
