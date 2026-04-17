package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wickes1/joplin-mcp/joplin"
)

// TestExportNotes verifies that exporting 2 notes creates files with correct content.
func TestExportNotes(t *testing.T) {
	mock := NewMockAPI()
	mock.Folders = []*joplin.Folder{
		{ID: "f1", Title: "TestFolder"},
	}
	mock.Notes["n1"] = &joplin.Note{
		ID:       "n1",
		Title:    "First Note",
		Body:     "Body of note one.",
		ParentID: "f1",
	}
	mock.Notes["n2"] = &joplin.Note{
		ID:       "n2",
		Title:    "Second Note",
		Body:     "Body of note two.",
		ParentID: "f1",
	}

	ctx := context.Background()
	outputDir := t.TempDir()
	folderDir := filepath.Join(outputDir, "TestFolder")

	// Simulate export: list notes in folder, write each to disk
	resp, err := mock.ListNotes(ctx, "f1", 1, 100)
	if err != nil {
		t.Fatalf("ListNotes failed: %v", err)
	}
	if len(resp.Items) != 2 {
		t.Fatalf("expected 2 notes, got %d", len(resp.Items))
	}

	// Create folder directory
	if err := os.MkdirAll(folderDir, 0755); err != nil {
		t.Fatalf("failed to create folder dir: %v", err)
	}

	for _, note := range resp.Items {
		full, err := mock.GetNote(ctx, note.ID)
		if err != nil {
			t.Fatalf("GetNote(%s) failed: %v", note.ID, err)
		}
		fileName := sanitizeFilename(full.Title) + ".md"
		filePath := filepath.Join(folderDir, fileName)
		if err := os.WriteFile(filePath, []byte(full.Body), 0644); err != nil {
			t.Fatalf("failed to write %s: %v", filePath, err)
		}
	}

	// Verify files exist with correct content
	for _, tc := range []struct {
		title string
		body  string
	}{
		{"First Note", "Body of note one."},
		{"Second Note", "Body of note two."},
	} {
		fileName := sanitizeFilename(tc.title) + ".md"
		filePath := filepath.Join(folderDir, fileName)
		data, err := os.ReadFile(filePath)
		if err != nil {
			t.Fatalf("failed to read %s: %v", filePath, err)
		}
		if string(data) != tc.body {
			t.Errorf("file %s content = %q, want %q", fileName, string(data), tc.body)
		}
	}
}

// TestExportNotes_WithMetadata verifies that YAML frontmatter is correctly prepended.
func TestExportNotes_WithMetadata(t *testing.T) {
	mock := NewMockAPI()
	mock.Folders = []*joplin.Folder{
		{ID: "f1", Title: "Work"},
	}
	mock.Notes["n1"] = &joplin.Note{
		ID:          "n1",
		Title:       "Meeting Notes",
		Body:        "Discussed project status.",
		ParentID:    "f1",
		CreatedTime: 1700000000000,
		UpdatedTime: 1700000001000,
	}
	mock.Tags["t1"] = &joplin.Tag{ID: "t1", Title: "meeting"}
	mock.NoteTags["n1"] = map[string]bool{"t1": true}

	ctx := context.Background()
	outputDir := t.TempDir()

	// Fetch note and its tags
	note, _ := mock.GetNote(ctx, "n1")
	tagObjs, _ := mock.GetNoteTags(ctx, "n1")
	tagTitles := make([]string, 0, len(tagObjs))
	for _, tg := range tagObjs {
		tagTitles = append(tagTitles, tg.Title)
	}

	// Build frontmatter + body
	content := buildFrontmatter(note, "Work", tagTitles) + note.Body

	// Write to disk
	filePath := filepath.Join(outputDir, sanitizeFilename(note.Title)+".md")
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	// Read back and verify
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	text := string(data)

	// Verify frontmatter fields
	if !strings.HasPrefix(text, "---\n") {
		t.Error("file should start with YAML frontmatter delimiter '---'")
	}
	if !strings.Contains(text, "joplin_id: n1") {
		t.Error("frontmatter should contain joplin_id")
	}
	if !strings.Contains(text, `title: "Meeting Notes"`) {
		t.Error("frontmatter should contain title")
	}
	if !strings.Contains(text, `folder: "Work"`) {
		t.Error("frontmatter should contain folder")
	}
	if !strings.Contains(text, "- meeting") {
		t.Error("frontmatter should contain tag 'meeting'")
	}
	if !strings.Contains(text, "created:") {
		t.Error("frontmatter should contain created timestamp")
	}
	if !strings.Contains(text, "updated:") {
		t.Error("frontmatter should contain updated timestamp")
	}

	// Verify body follows frontmatter
	if !strings.Contains(text, "Discussed project status.") {
		t.Error("file should contain the note body after frontmatter")
	}
}

// TestBatchImportMarkdown verifies importing .md files from a temp directory.
func TestBatchImportMarkdown(t *testing.T) {
	mock := NewMockAPI()
	mock.Folders = []*joplin.Folder{
		{ID: "f1", Title: "Import"},
	}

	ctx := context.Background()
	inputDir := t.TempDir()

	// Create 2 test markdown files
	files := map[string]string{
		"note-one.md": "# First Imported\n\nContent of first import.",
		"note-two.md": "# Second Imported\n\nContent of second import.",
	}
	for name, content := range files {
		filePath := filepath.Join(inputDir, name)
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			t.Fatalf("failed to create test file %s: %v", name, err)
		}
	}

	// Read each .md file and create notes
	entries, err := os.ReadDir(inputDir)
	if err != nil {
		t.Fatalf("failed to read input dir: %v", err)
	}

	imported := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		filePath := filepath.Join(inputDir, entry.Name())
		data, err := os.ReadFile(filePath)
		if err != nil {
			t.Fatalf("failed to read %s: %v", filePath, err)
		}
		body := string(data)
		title := extractMarkdownTitle(body, filePath)

		_, err = mock.CreateNote(ctx, joplin.NoteCreateParams{
			Title:    title,
			Body:     body,
			ParentID: "f1",
		})
		if err != nil {
			t.Fatalf("failed to create note from %s: %v", filePath, err)
		}
		imported++
	}

	if imported != 2 {
		t.Errorf("expected 2 imported notes, got %d", imported)
	}

	// Verify notes were created with correct titles
	allNotes, _ := mock.ListNotes(ctx, "f1", 1, 100)
	if len(allNotes.Items) != 2 {
		t.Fatalf("expected 2 notes in mock, got %d", len(allNotes.Items))
	}

	titles := make(map[string]bool)
	for _, n := range allNotes.Items {
		titles[n.Title] = true
	}
	if !titles["First Imported"] {
		t.Error("expected note with title 'First Imported'")
	}
	if !titles["Second Imported"] {
		t.Error("expected note with title 'Second Imported'")
	}
}

// TestSanitizeFilenameForExport tests sanitizeFilename specifically for export use cases.
func TestSanitizeFilenameForExport(t *testing.T) {
	t.Run("normal title", func(t *testing.T) {
		got := sanitizeFilename("My Notes About Go")
		if got != "My Notes About Go" {
			t.Errorf("got %q, want %q", got, "My Notes About Go")
		}
	})

	t.Run("title with colons", func(t *testing.T) {
		got := sanitizeFilename("Meeting: Q4 Review")
		if got != "Meeting_ Q4 Review" {
			t.Errorf("got %q, want %q", got, "Meeting_ Q4 Review")
		}
	})
}

// TestExtractMarkdownTitle verifies title extraction from markdown content.
func TestExtractMarkdownTitle(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		filePath string
		want     string
	}{
		{
			name:     "heading on first line",
			body:     "# My Title\n\nSome content.",
			filePath: "/tmp/test.md",
			want:     "My Title",
		},
		{
			name:     "heading on line 5",
			body:     "\n\n\n\n# Deeper Title\n\nContent.",
			filePath: "/tmp/test.md",
			want:     "Deeper Title",
		},
		{
			name:     "no heading falls back to filename",
			body:     "Just plain text without any heading.",
			filePath: "/tmp/my-notes.md",
			want:     "my-notes",
		},
		{
			name:     "heading beyond line 10 falls back to filename",
			body:     "\n\n\n\n\n\n\n\n\n\n# Too Deep\n\nContent.",
			filePath: "/tmp/fallback.md",
			want:     "fallback",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractMarkdownTitle(tt.body, tt.filePath)
			if got != tt.want {
				t.Errorf("extractMarkdownTitle() = %q, want %q", got, tt.want)
			}
		})
	}
}
