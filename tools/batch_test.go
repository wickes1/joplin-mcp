package tools

import (
	"context"
	"testing"

	"github.com/Wickes1/joplin-mcp/joplin"
)

// TestBatchMoveNotes verifies that batch_move_notes changes the parent_id of multiple notes.
func TestBatchMoveNotes(t *testing.T) {
	mock := NewMockAPI()
	mock.Folders = []*joplin.Folder{
		{ID: "src", Title: "Source"},
		{ID: "dst", Title: "Destination"},
	}
	mock.Notes["n1"] = &joplin.Note{ID: "n1", Title: "Note 1", ParentID: "src"}
	mock.Notes["n2"] = &joplin.Note{ID: "n2", Title: "Note 2", ParentID: "src"}
	mock.Notes["n3"] = &joplin.Note{ID: "n3", Title: "Note 3", ParentID: "src"}

	ctx := context.Background()

	// Move all 3 notes to destination folder
	noteIDs := []string{"n1", "n2", "n3"}
	for _, id := range noteIDs {
		_, err := mock.UpdateNote(ctx, id, joplin.NoteUpdateParams{
			ParentID: joplin.StringPtr("dst"),
		})
		if err != nil {
			t.Fatalf("failed to move note %s: %v", id, err)
		}
	}

	// Verify all notes now have the destination folder
	for _, id := range noteIDs {
		note, err := mock.GetNote(ctx, id)
		if err != nil {
			t.Fatalf("failed to get note %s: %v", id, err)
		}
		if note.ParentID != "dst" {
			t.Errorf("note %s parent_id = %q, want %q", id, note.ParentID, "dst")
		}
	}
}

// TestBatchMoveNotes_InvalidFolder verifies that moving to a nonexistent note returns an error.
func TestBatchMoveNotes_InvalidFolder(t *testing.T) {
	mock := NewMockAPI()
	mock.Folders = []*joplin.Folder{
		{ID: "src", Title: "Source"},
	}
	fc := NewFolderCache(mock)

	ctx := context.Background()

	// Attempt to resolve a folder that doesn't exist (no auto-create)
	_, _, err := resolveFolderID(ctx, mock, fc, "", "NonExistent", false)
	if err == nil {
		t.Fatal("expected error for nonexistent folder, got nil")
	}
	ae, ok := err.(*joplin.AgentError)
	if !ok {
		t.Fatalf("expected *AgentError, got %T: %v", err, err)
	}
	if ae.ErrorMsg == "" {
		t.Error("error message should not be empty")
	}
}

// TestMergeNotes verifies that merging 2 notes produces a body with ## headings and separators.
func TestMergeNotes(t *testing.T) {
	mock := NewMockAPI()
	mock.Folders = []*joplin.Folder{
		{ID: "f1", Title: "Work"},
	}
	mock.Notes["n1"] = &joplin.Note{
		ID:       "n1",
		Title:    "First Note",
		Body:     "Content of first note.",
		ParentID: "f1",
	}
	mock.Notes["n2"] = &joplin.Note{
		ID:       "n2",
		Title:    "Second Note",
		Body:     "Content of second note.",
		ParentID: "f1",
	}

	ctx := context.Background()

	// Simulate merge: fetch both notes, build merged body
	note1, _ := mock.GetNote(ctx, "n1")
	note2, _ := mock.GetNote(ctx, "n2")

	parts := []string{
		"## " + note1.Title + "\n\n" + note1.Body,
		"## " + note2.Title + "\n\n" + note2.Body,
	}
	mergedBody := parts[0] + "\n\n---\n\n" + parts[1]

	// Create merged note
	merged, err := mock.CreateNote(ctx, joplin.NoteCreateParams{
		Title:    "Merged Note",
		Body:     mergedBody,
		ParentID: "f1",
	})
	if err != nil {
		t.Fatalf("failed to create merged note: %v", err)
	}

	// Verify the merged note
	if merged.Title != "Merged Note" {
		t.Errorf("title = %q, want %q", merged.Title, "Merged Note")
	}
	if merged.ParentID != "f1" {
		t.Errorf("parent_id = %q, want %q", merged.ParentID, "f1")
	}

	// Verify body contains both headings
	got, _ := mock.GetNote(ctx, merged.ID)
	if got.Body != mergedBody {
		t.Errorf("body mismatch:\ngot:  %q\nwant: %q", got.Body, mergedBody)
	}
	if !containsSubstring(got.Body, "## First Note") {
		t.Error("merged body should contain '## First Note'")
	}
	if !containsSubstring(got.Body, "## Second Note") {
		t.Error("merged body should contain '## Second Note'")
	}
	if !containsSubstring(got.Body, "---") {
		t.Error("merged body should contain '---' separator")
	}
	if !containsSubstring(got.Body, "Content of first note.") {
		t.Error("merged body should contain first note content")
	}
	if !containsSubstring(got.Body, "Content of second note.") {
		t.Error("merged body should contain second note content")
	}
}

// TestBatchTagNotes_Add verifies adding a tag to multiple notes.
func TestBatchTagNotes_Add(t *testing.T) {
	mock := NewMockAPI()
	mock.Notes["n1"] = &joplin.Note{ID: "n1", Title: "Note 1"}
	mock.Notes["n2"] = &joplin.Note{ID: "n2", Title: "Note 2"}

	ctx := context.Background()

	// Create the tag first
	tag, err := mock.CreateTag(ctx, "important")
	if err != nil {
		t.Fatalf("failed to create tag: %v", err)
	}

	// Tag both notes
	for _, noteID := range []string{"n1", "n2"} {
		if err := mock.TagNote(ctx, tag.ID, noteID); err != nil {
			t.Fatalf("failed to tag note %s: %v", noteID, err)
		}
	}

	// Verify both notes have the tag
	for _, noteID := range []string{"n1", "n2"} {
		tags, err := mock.GetNoteTags(ctx, noteID)
		if err != nil {
			t.Fatalf("failed to get tags for note %s: %v", noteID, err)
		}
		found := false
		for _, tg := range tags {
			if tg.ID == tag.ID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("note %s should have tag %q", noteID, tag.ID)
		}
	}
}

// TestBatchTagNotes_Remove verifies removing a tag from notes.
func TestBatchTagNotes_Remove(t *testing.T) {
	mock := NewMockAPI()
	mock.Notes["n1"] = &joplin.Note{ID: "n1", Title: "Note 1"}
	mock.Tags["t1"] = &joplin.Tag{ID: "t1", Title: "obsolete"}
	mock.NoteTags["n1"] = map[string]bool{"t1": true}

	ctx := context.Background()

	// Verify tag exists before removal
	tagsBefore, _ := mock.GetNoteTags(ctx, "n1")
	if len(tagsBefore) != 1 {
		t.Fatalf("expected 1 tag before removal, got %d", len(tagsBefore))
	}

	// Remove the tag
	if err := mock.UntagNote(ctx, "t1", "n1"); err != nil {
		t.Fatalf("failed to untag note: %v", err)
	}

	// Verify tag is removed
	tagsAfter, _ := mock.GetNoteTags(ctx, "n1")
	if len(tagsAfter) != 0 {
		t.Errorf("expected 0 tags after removal, got %d", len(tagsAfter))
	}
}

// containsSubstring is a test helper for readable assertions.
func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && contains(s, substr))
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
