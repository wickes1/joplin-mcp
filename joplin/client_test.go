package joplin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// newTestClient creates a Client pointing at the given test server URL.
func newTestClient(t *testing.T, serverURL string) *Client {
	t.Helper()
	// Parse host and port from the test server URL (format: http://127.0.0.1:PORT)
	addr := strings.TrimPrefix(serverURL, "http://")
	lastColon := strings.LastIndex(addr, ":")
	host := addr[:lastColon]
	port, _ := strconv.Atoi(addr[lastColon+1:])
	return NewClient("test-token", host, port)
}

// TestPing verifies that Ping succeeds against a mock server.
func TestPing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ping" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("token") != "test-token" {
			t.Errorf("expected token query param, got: %s", r.URL.Query().Get("token"))
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("JoplinClipperServer"))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
}

// TestGetNote verifies correct deserialization of a note from the API.
func TestGetNote(t *testing.T) {
	noteJSON := `{
		"id": "abc123",
		"title": "Test Note",
		"body": "Hello world",
		"parent_id": "folder1",
		"is_todo": 1,
		"todo_completed": 0,
		"created_time": 1700000000000,
		"updated_time": 1700000001000
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/notes/abc123") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(noteJSON))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	note, err := c.GetNote(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("GetNote() error = %v", err)
	}

	if note.ID != "abc123" {
		t.Errorf("ID = %q, want %q", note.ID, "abc123")
	}
	if note.Title != "Test Note" {
		t.Errorf("Title = %q, want %q", note.Title, "Test Note")
	}
	if note.Body != "Hello world" {
		t.Errorf("Body = %q, want %q", note.Body, "Hello world")
	}
	if note.IsTodo != 1 {
		t.Errorf("IsTodo = %d, want 1", note.IsTodo)
	}
	if note.TodoCompleted != 0 {
		t.Errorf("TodoCompleted = %d, want 0", note.TodoCompleted)
	}
	if note.CreatedTime != 1700000000000 {
		t.Errorf("CreatedTime = %d, want 1700000000000", note.CreatedTime)
	}
}

// TestGetNote_NotFound verifies that a 404 returns a NoteNotFound AgentError.
func TestGetNote_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"not found"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	_, err := c.GetNote(context.Background(), "missing-id")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	ae, ok := err.(*AgentError)
	if !ok {
		t.Fatalf("expected *AgentError, got %T: %v", err, err)
	}
	if !strings.Contains(ae.ErrorMsg, "missing-id") {
		t.Errorf("ErrorMsg %q does not contain note ID", ae.ErrorMsg)
	}
}

// Test403ReturnsForbiddenError verifies that a 403 response returns JoplinForbidden.
func Test403ReturnsForbiddenError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	err := c.Ping(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	ae, ok := err.(*AgentError)
	if !ok {
		t.Fatalf("expected *AgentError, got %T: %v", err, err)
	}
	if !strings.Contains(ae.ErrorMsg, "403") {
		t.Errorf("ErrorMsg %q does not mention 403", ae.ErrorMsg)
	}
	if !strings.Contains(ae.Hint, "JOPLIN_TOKEN") {
		t.Errorf("Hint %q does not mention JOPLIN_TOKEN", ae.Hint)
	}
}

// TestFormatTimestamp verifies timestamp conversion.
func TestFormatTimestamp(t *testing.T) {
	tests := []struct {
		name         string
		ms           int64
		wantEmpty    bool
		wantContains string
	}{
		{
			name:      "zero returns empty",
			ms:        0,
			wantEmpty: true,
		},
		{
			name:         "non-zero returns ISO 8601",
			ms:           1700000000000,
			wantEmpty:    false,
			wantContains: "2023",
		},
		{
			name:         "recent timestamp",
			ms:           1712000000000,
			wantEmpty:    false,
			wantContains: "T",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatTimestamp(tt.ms)
			if tt.wantEmpty && got != "" {
				t.Errorf("FormatTimestamp(%d) = %q, want empty", tt.ms, got)
			}
			if !tt.wantEmpty && got == "" {
				t.Errorf("FormatTimestamp(%d) = empty, want non-empty", tt.ms)
			}
			if tt.wantContains != "" && !strings.Contains(got, tt.wantContains) {
				t.Errorf("FormatTimestamp(%d) = %q, want to contain %q", tt.ms, got, tt.wantContains)
			}
		})
	}
}

// TestNoteToSlim verifies int→bool conversion for is_todo and folder_title injection.
func TestNoteToSlim(t *testing.T) {
	tests := []struct {
		name        string
		note        Note
		folderTitle string
		wantIsTodo  bool
	}{
		{
			name:        "is_todo=0 maps to false",
			note:        Note{ID: "n1", Title: "Note", IsTodo: 0, UpdatedTime: 1700000000000},
			folderTitle: "Inbox",
			wantIsTodo:  false,
		},
		{
			name:        "is_todo=1 maps to true",
			note:        Note{ID: "n2", Title: "Todo", IsTodo: 1, UpdatedTime: 1700000000000},
			folderTitle: "Tasks",
			wantIsTodo:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			slim := tt.note.ToSlim(tt.folderTitle)
			if slim.IsTodo != tt.wantIsTodo {
				t.Errorf("IsTodo = %v, want %v", slim.IsTodo, tt.wantIsTodo)
			}
			if slim.FolderTitle != tt.folderTitle {
				t.Errorf("FolderTitle = %q, want %q", slim.FolderTitle, tt.folderTitle)
			}
			if slim.UpdatedTime == "" {
				t.Error("UpdatedTime should not be empty for non-zero timestamp")
			}
		})
	}
}

// TestJoplinTimeout_ReadHint verifies that JoplinTimeout for a read op omits the write-safety warning.
func TestJoplinTimeout_ReadHint(t *testing.T) {
	ae := JoplinTimeout("localhost", 41184, false, 30*time.Second)
	if ae == nil {
		t.Fatal("expected non-nil AgentError")
	}
	if !strings.Contains(ae.ErrorMsg, "localhost") {
		t.Errorf("ErrorMsg %q does not mention host", ae.ErrorMsg)
	}
	if !strings.Contains(ae.ErrorMsg, "30s") {
		t.Errorf("ErrorMsg %q does not mention timeout duration", ae.ErrorMsg)
	}
	if strings.Contains(ae.Hint, "may already have been applied") {
		t.Errorf("read timeout hint should not warn about writes: %q", ae.Hint)
	}
	if !strings.Contains(ae.Hint, "JOPLIN_READ_TIMEOUT") {
		t.Errorf("read timeout hint should mention JOPLIN_READ_TIMEOUT: %q", ae.Hint)
	}
}

// TestJoplinTimeout_WriteHint verifies that JoplinTimeout for a write op includes idempotency warning.
func TestJoplinTimeout_WriteHint(t *testing.T) {
	ae := JoplinTimeout("localhost", 41184, true, 120*time.Second)
	if ae == nil {
		t.Fatal("expected non-nil AgentError")
	}
	if !strings.Contains(ae.Hint, "may already have been applied") {
		t.Errorf("write timeout hint should warn about idempotency: %q", ae.Hint)
	}
	if !strings.Contains(ae.Hint, "JOPLIN_WRITE_TIMEOUT") {
		t.Errorf("write timeout hint should mention JOPLIN_WRITE_TIMEOUT: %q", ae.Hint)
	}
}

// TestTimeout_RequestExceedsDeadline verifies that a hung server returns a JoplinTimeout AgentError.
func TestTimeout_RequestExceedsDeadline(t *testing.T) {
	// Server that blocks until the client gives up.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until the test context is done (client will cancel first).
		<-r.Context().Done()
	}))
	defer srv.Close()

	addr := strings.TrimPrefix(srv.URL, "http://")
	lastColon := strings.LastIndex(addr, ":")
	host := addr[:lastColon]
	port, _ := strconv.Atoi(addr[lastColon+1:])

	// Configure a very short read timeout so the test completes quickly.
	c := NewClient("test-token", host, port, WithReadTimeout(50*time.Millisecond))

	_, err := c.GetNote(context.Background(), "any-id")
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	ae, ok := err.(*AgentError)
	if !ok {
		t.Fatalf("expected *AgentError, got %T: %v", err, err)
	}
	if !strings.Contains(ae.ErrorMsg, "timed out") {
		t.Errorf("ErrorMsg %q should mention timed out", ae.ErrorMsg)
	}
}

// TestNewClient_VariadicOpts verifies that option defaults can be overridden.
func TestNewClient_VariadicOpts(t *testing.T) {
	c := NewClient("tok", "localhost", 41184,
		WithReadTimeout(5*time.Second),
		WithWriteTimeout(60*time.Second),
	)
	if c.readTimeout != 5*time.Second {
		t.Errorf("readTimeout = %v, want 5s", c.readTimeout)
	}
	if c.writeTimeout != 60*time.Second {
		t.Errorf("writeTimeout = %v, want 60s", c.writeTimeout)
	}
}

// TestNewClient_Defaults verifies that default timeouts are applied when no opts given.
func TestNewClient_Defaults(t *testing.T) {
	c := NewClient("tok", "localhost", 41184)
	if c.readTimeout != 30*time.Second {
		t.Errorf("default readTimeout = %v, want 30s", c.readTimeout)
	}
	if c.writeTimeout != 120*time.Second {
		t.Errorf("default writeTimeout = %v, want 120s", c.writeTimeout)
	}
}

// TestNoteToFull_TodoCompleted verifies that todo_completed=0 → nil, >0 → timestamp string.
func TestNoteToFull_TodoCompleted(t *testing.T) {
	t.Run("todo_completed=0 maps to nil", func(t *testing.T) {
		note := Note{
			ID:            "n1",
			Title:         "Pending Todo",
			IsTodo:        1,
			TodoCompleted: 0,
			UpdatedTime:   1700000000000,
		}
		full := note.ToFull("Inbox", nil)
		if full.TodoCompleted != nil {
			t.Errorf("TodoCompleted = %v, want nil", *full.TodoCompleted)
		}
		// Verify JSON marshaling produces null
		data, err := json.Marshal(full)
		if err != nil {
			t.Fatalf("json.Marshal error: %v", err)
		}
		if !strings.Contains(string(data), `"todo_completed":null`) {
			t.Errorf("JSON %s does not contain todo_completed:null", data)
		}
	})

	t.Run("todo_completed>0 maps to ISO 8601 string", func(t *testing.T) {
		note := Note{
			ID:            "n2",
			Title:         "Done Todo",
			IsTodo:        1,
			TodoCompleted: 1700000000000,
			UpdatedTime:   1700000001000,
		}
		full := note.ToFull("Tasks", []string{"done"})
		if full.TodoCompleted == nil {
			t.Fatal("TodoCompleted should not be nil for completed todo")
		}
		if !strings.Contains(*full.TodoCompleted, "2023") {
			t.Errorf("TodoCompleted = %q, expected ISO 8601 timestamp from 2023", *full.TodoCompleted)
		}
	})
}
