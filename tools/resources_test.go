package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Wickes1/joplin-mcp/joplin"
)

// TestListResources verifies listing resources with pagination.
func TestListResources(t *testing.T) {
	mock := NewMockAPI()
	// Add 3 resources
	mock.Resources["r1"] = &joplin.Resource{ID: "r1", Title: "image.png", Mime: "image/png", Size: 1024}
	mock.Resources["r2"] = &joplin.Resource{ID: "r2", Title: "doc.pdf", Mime: "application/pdf", Size: 2048}
	mock.Resources["r3"] = &joplin.Resource{ID: "r3", Title: "data.csv", Mime: "text/csv", Size: 512}

	ctx := context.Background()

	t.Run("page 1 with limit 2", func(t *testing.T) {
		resp, err := mock.ListResources(ctx, 1, 2)
		if err != nil {
			t.Fatalf("ListResources failed: %v", err)
		}
		if len(resp.Items) != 2 {
			t.Errorf("expected 2 items, got %d", len(resp.Items))
		}
		if !resp.HasMore {
			t.Error("expected has_more = true")
		}
	})

	t.Run("page 2 with limit 2", func(t *testing.T) {
		resp, err := mock.ListResources(ctx, 2, 2)
		if err != nil {
			t.Fatalf("ListResources failed: %v", err)
		}
		if len(resp.Items) != 1 {
			t.Errorf("expected 1 item, got %d", len(resp.Items))
		}
		if resp.HasMore {
			t.Error("expected has_more = false")
		}
	})

	t.Run("all at once", func(t *testing.T) {
		resp, err := mock.ListResources(ctx, 1, 100)
		if err != nil {
			t.Fatalf("ListResources failed: %v", err)
		}
		if len(resp.Items) != 3 {
			t.Errorf("expected 3 items, got %d", len(resp.Items))
		}
		if resp.HasMore {
			t.Error("expected has_more = false")
		}
	})
}

// TestGetResource verifies getting a resource by ID.
func TestGetResource(t *testing.T) {
	mock := NewMockAPI()
	mock.Resources["r1"] = &joplin.Resource{
		ID:       "r1",
		Title:    "photo.jpg",
		Mime:     "image/jpeg",
		Filename: "photo.jpg",
		Size:     4096,
	}

	ctx := context.Background()

	t.Run("existing resource", func(t *testing.T) {
		resource, err := mock.GetResource(ctx, "r1")
		if err != nil {
			t.Fatalf("GetResource failed: %v", err)
		}
		if resource.ID != "r1" {
			t.Errorf("ID = %q, want %q", resource.ID, "r1")
		}
		if resource.Title != "photo.jpg" {
			t.Errorf("Title = %q, want %q", resource.Title, "photo.jpg")
		}
		if resource.Mime != "image/jpeg" {
			t.Errorf("Mime = %q, want %q", resource.Mime, "image/jpeg")
		}
		if resource.Size != 4096 {
			t.Errorf("Size = %d, want %d", resource.Size, 4096)
		}
	})

	t.Run("nonexistent resource", func(t *testing.T) {
		_, err := mock.GetResource(ctx, "nonexistent")
		if err == nil {
			t.Fatal("expected error for nonexistent resource, got nil")
		}
		ae, ok := err.(*joplin.AgentError)
		if !ok {
			t.Fatalf("expected *AgentError, got %T", err)
		}
		if ae.ErrorMsg == "" {
			t.Error("error message should not be empty")
		}
	})
}

// TestDownloadResource verifies downloading a resource file to disk.
func TestDownloadResource(t *testing.T) {
	mock := NewMockAPI()
	mock.Resources["r1"] = &joplin.Resource{
		ID:       "r1",
		Title:    "test-file.txt",
		Mime:     "text/plain",
		Filename: "test-file.txt",
		Size:     13,
	}
	fileContent := []byte("Hello, World!")
	mock.ResourceFiles["r1"] = fileContent

	ctx := context.Background()
	outputDir := t.TempDir()
	outputPath := filepath.Join(outputDir, "downloaded.txt")

	// Fetch file content
	data, err := mock.GetResourceFile(ctx, "r1")
	if err != nil {
		t.Fatalf("GetResourceFile failed: %v", err)
	}

	// Write to disk
	if err := os.WriteFile(outputPath, data, 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	// Verify content
	readBack, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("failed to read downloaded file: %v", err)
	}
	if string(readBack) != string(fileContent) {
		t.Errorf("downloaded content = %q, want %q", string(readBack), string(fileContent))
	}
}

// TestUploadResource verifies uploading a file as a resource.
func TestUploadResource(t *testing.T) {
	mock := NewMockAPI()

	ctx := context.Background()
	tempDir := t.TempDir()

	// Create a temp file to "upload"
	tempFile := filepath.Join(tempDir, "upload-test.txt")
	content := []byte("This is test content for upload.")
	if err := os.WriteFile(tempFile, content, 0644); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	// Upload via mock
	resource, err := mock.CreateResource(ctx, tempFile, "My Upload")
	if err != nil {
		t.Fatalf("CreateResource failed: %v", err)
	}

	if resource.ID == "" {
		t.Error("resource ID should not be empty")
	}
	if resource.Title != "My Upload" {
		t.Errorf("Title = %q, want %q", resource.Title, "My Upload")
	}

	// Verify resource is stored in mock
	stored, err := mock.GetResource(ctx, resource.ID)
	if err != nil {
		t.Fatalf("failed to get uploaded resource: %v", err)
	}
	if stored.Title != "My Upload" {
		t.Errorf("stored Title = %q, want %q", stored.Title, "My Upload")
	}
}

// TestResourceToSlim verifies the ToSlim conversion for resources.
func TestResourceToSlim(t *testing.T) {
	r := joplin.Resource{
		ID:          "r1",
		Title:       "test.png",
		Mime:        "image/png",
		Filename:    "test.png",
		Size:        1024,
		UpdatedTime: 1700000000000,
	}

	slim := r.ToSlim()

	if slim.ID != "r1" {
		t.Errorf("ID = %q, want %q", slim.ID, "r1")
	}
	if slim.Title != "test.png" {
		t.Errorf("Title = %q, want %q", slim.Title, "test.png")
	}
	if slim.Mime != "image/png" {
		t.Errorf("Mime = %q, want %q", slim.Mime, "image/png")
	}
	if slim.Size != 1024 {
		t.Errorf("Size = %d, want %d", slim.Size, 1024)
	}
	if slim.UpdatedTime == "" {
		t.Error("UpdatedTime should not be empty for non-zero timestamp")
	}
}

// TestDeleteResource verifies resource deletion.
func TestDeleteResource(t *testing.T) {
	mock := NewMockAPI()
	mock.Resources["r1"] = &joplin.Resource{ID: "r1", Title: "to-delete.txt"}
	mock.ResourceFiles["r1"] = []byte("content")

	ctx := context.Background()

	// Delete the resource
	if err := mock.DeleteResource(ctx, "r1"); err != nil {
		t.Fatalf("DeleteResource failed: %v", err)
	}

	// Verify it's gone
	_, err := mock.GetResource(ctx, "r1")
	if err == nil {
		t.Error("expected error after deletion, got nil")
	}

	// Verify file data is gone too
	_, err = mock.GetResourceFile(ctx, "r1")
	if err == nil {
		t.Error("expected error for resource file after deletion, got nil")
	}
}
