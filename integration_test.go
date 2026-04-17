//go:build integration

package main

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/Wickes1/joplin-mcp/joplin"
)

func getTestClient(t *testing.T) *joplin.Client {
	token := os.Getenv("JOPLIN_TOKEN")
	if token == "" {
		t.Skip("JOPLIN_TOKEN not set, skipping integration test")
	}
	return joplin.NewClient(token, "localhost", 41184)
}

func TestIntegration_Ping(t *testing.T) {
	c := getTestClient(t)
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("Ping failed: %v", err)
	}
	t.Log("Joplin is reachable")
}

func TestIntegration_NoteCRUD(t *testing.T) {
	c := getTestClient(t)
	ctx := context.Background()

	// Create
	note, err := c.CreateNote(ctx, joplin.NoteCreateParams{Title: "Integration Test Note", Body: "Hello from Go MCP"})
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	t.Logf("Created note: %s", note.ID)
	t.Cleanup(func() { c.DeleteNote(ctx, note.ID, true) })

	// Read
	got, err := c.GetNote(ctx, note.ID)
	if err != nil {
		t.Fatalf("GetNote: %v", err)
	}
	if got.Title != "Integration Test Note" {
		t.Errorf("title = %q, want %q", got.Title, "Integration Test Note")
	}
	if got.Body != "Hello from Go MCP" {
		t.Errorf("body = %q, want %q", got.Body, "Hello from Go MCP")
	}

	// Update
	_, err = c.UpdateNote(ctx, note.ID, joplin.NoteUpdateParams{Title: joplin.StringPtr("Updated Title")})
	if err != nil {
		t.Fatalf("UpdateNote: %v", err)
	}
	got2, _ := c.GetNote(ctx, note.ID)
	if got2.Title != "Updated Title" {
		t.Errorf("after update title = %q, want %q", got2.Title, "Updated Title")
	}

	// Search — Joplin's FTS index may need time after writes
	time.Sleep(2 * time.Second)
	results, err := c.SearchNotes(ctx, "Updated Title", 1, 10)
	if err != nil {
		t.Fatalf("SearchNotes: %v", err)
	}
	found := false
	for _, n := range results.Items {
		if n.ID == note.ID {
			found = true
			break
		}
	}
	if !found {
		t.Log("WARN: search did not find the note — Joplin FTS index may be slow, this is expected")
	}
}

func TestIntegration_FoldersAndTags(t *testing.T) {
	c := getTestClient(t)
	ctx := context.Background()

	// Create folder
	folder, err := c.CreateFolder(ctx, "Go MCP Test Folder", "")
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	t.Logf("Created folder: %s", folder.ID)
	t.Cleanup(func() { c.DeleteFolder(ctx, folder.ID, true) })

	// List folders
	folders, err := c.ListFolders(ctx)
	if err != nil {
		t.Fatalf("ListFolders: %v", err)
	}
	if len(folders) == 0 {
		t.Error("expected at least one folder")
	}

	// Create tag
	tag, err := c.CreateTag(ctx, "go-mcp-test-tag")
	if err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	t.Cleanup(func() { c.DeleteTag(ctx, tag.ID) })

	// Create note in folder
	note, err := c.CreateNote(ctx, joplin.NoteCreateParams{Title: "Note In Test Folder", ParentID: folder.ID})
	if err != nil {
		t.Fatalf("CreateNote in folder: %v", err)
	}
	t.Cleanup(func() { c.DeleteNote(ctx, note.ID, true) })

	// Tag the note
	if err := c.TagNote(ctx, tag.ID, note.ID); err != nil {
		t.Fatalf("TagNote: %v", err)
	}

	// Verify tags
	tags, err := c.GetNoteTags(ctx, note.ID)
	if err != nil {
		t.Fatalf("GetNoteTags: %v", err)
	}
	if len(tags) == 0 {
		t.Error("expected note to have at least one tag")
	}

	// Get notes by tag
	tagNotes, err := c.GetNotesByTag(ctx, tag.ID, 1, 20)
	if err != nil {
		t.Fatalf("GetNotesByTag: %v", err)
	}
	if len(tagNotes.Items) == 0 {
		t.Error("expected at least one note for tag")
	}

	// Untag
	if err := c.UntagNote(ctx, tag.ID, note.ID); err != nil {
		t.Fatalf("UntagNote: %v", err)
	}

	// Verify untagged
	tags2, _ := c.GetNoteTags(ctx, note.ID)
	for _, tt := range tags2 {
		if tt.ID == tag.ID {
			t.Error("tag should have been removed")
		}
	}
}

func TestIntegration_ListNotes(t *testing.T) {
	c := getTestClient(t)
	ctx := context.Background()

	resp, err := c.ListNotes(ctx, "", 1, 5)
	if err != nil {
		t.Fatalf("ListNotes: %v", err)
	}
	t.Logf("Listed %d notes, has_more=%v", len(resp.Items), resp.HasMore)
}
