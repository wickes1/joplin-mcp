package tools

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Wickes1/joplin-mcp/joplin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/sync/errgroup"
)

// RegisterNoteTools registers the 6 note-related MCP tools onto the server.
func RegisterNoteTools(s *mcp.Server, c joplin.API, fc *FolderCache) {
	mcp.AddTool(s, &mcp.Tool{Name: "list_notes", Description: "List notes, optionally filtered by folder. Returns slim notes and a has_more flag."},
		func(ctx context.Context, req *mcp.CallToolRequest, args struct {
			FolderID string `json:"folder_id,omitempty" jsonschema:"Filter by folder ID (optional)"`
			Limit    int    `json:"limit,omitempty"    jsonschema:"Max results per page (default 20 max 100)"`
			Page     int    `json:"page,omitempty"     jsonschema:"Page number 1-indexed (default 1)"`
		}) (*mcp.CallToolResult, any, error) {
			limit := args.Limit
			if limit <= 0 {
				limit = 20
			}
			if limit > 100 {
				limit = 100
			}
			page := args.Page
			if page <= 0 {
				page = 1
			}

			resp, err := c.ListNotes(ctx, args.FolderID, page, limit)
			if err != nil {
				return handleErr(err)
			}

			slim := make([]joplin.SlimNote, 0, len(resp.Items))
			for i := range resp.Items {
				n := &resp.Items[i]
				folderTitle := fc.GetTitle(n.ParentID)
				slim = append(slim, n.ToSlim(folderTitle))
			}

			return toolSuccess(map[string]any{
				"notes":    slim,
				"has_more": resp.HasMore,
			})
		})

	mcp.AddTool(s, &mcp.Tool{Name: "get_note", Description: "Get a single note by ID with full body and tags."},
		func(ctx context.Context, req *mcp.CallToolRequest, args struct {
			NoteID string `json:"note_id" jsonschema:"The note ID to retrieve"`
		}) (*mcp.CallToolResult, any, error) {
			if args.NoteID == "" {
				return toolError("note_id is required", "")
			}

			note, err := c.GetNote(ctx, args.NoteID)
			if err != nil {
				return handleErr(err)
			}

			tags, err := c.GetNoteTags(ctx, args.NoteID)
			if err != nil {
				tags = []joplin.Tag{}
			}
			tagNames := make([]string, 0, len(tags))
			for _, t := range tags {
				tagNames = append(tagNames, t.Title)
			}

			folderTitle := fc.GetTitle(note.ParentID)
			full := note.ToFull(folderTitle, tagNames)
			return toolSuccess(full)
		})

	mcp.AddTool(s, &mcp.Tool{Name: "get_notes", Description: "Batch-read up to 50 notes by ID. Failed lookups are included as error entries."},
		func(ctx context.Context, req *mcp.CallToolRequest, args struct {
			NoteIDs []string `json:"note_ids" jsonschema:"List of note IDs to fetch (max 50)"`
		}) (*mcp.CallToolResult, any, error) {
			if len(args.NoteIDs) == 0 {
				return toolError("note_ids is required and must not be empty", "")
			}
			if len(args.NoteIDs) > 50 {
				return toolError("note_ids must contain at most 50 IDs", "Split into smaller batches.")
			}

			batchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()

			type result struct {
				idx  int
				note *joplin.FullNote
				err  string
			}

			results := make([]result, len(args.NoteIDs))
			var mu sync.Mutex

			g, gCtx := errgroup.WithContext(batchCtx)
			g.SetLimit(5)

			for i, id := range args.NoteIDs {
				i, id := i, id
				g.Go(func() error {
					note, err := c.GetNote(gCtx, id)
					mu.Lock()
					defer mu.Unlock()
					if err != nil {
						results[i] = result{idx: i, err: err.Error()}
						return nil
					}
					tags, _ := c.GetNoteTags(gCtx, id)
					tagNames := make([]string, 0, len(tags))
					for _, t := range tags {
						tagNames = append(tagNames, t.Title)
					}
					folderTitle := fc.GetTitle(note.ParentID)
					full := note.ToFull(folderTitle, tagNames)
					results[i] = result{idx: i, note: &full}
					return nil
				})
			}

			if err := g.Wait(); err != nil {
				return toolError(err.Error(), "")
			}

			out := make([]any, len(results))
			for _, r := range results {
				if r.err != "" {
					out[r.idx] = map[string]string{
						"id":    args.NoteIDs[r.idx],
						"error": r.err,
					}
				} else {
					out[r.idx] = r.note
				}
			}

			return toolSuccess(out)
		})

	mcp.AddTool(s, &mcp.Tool{Name: "create_note", Description: "Create a new note. Optionally auto-create folder by name and apply tags."},
		func(ctx context.Context, req *mcp.CallToolRequest, args struct {
			Title      string   `json:"title"                  jsonschema:"Note title"`
			Body       string   `json:"body,omitempty"         jsonschema:"Note body in Markdown"`
			FolderID   string   `json:"folder_id,omitempty"    jsonschema:"Destination folder ID"`
			FolderName string   `json:"folder_name,omitempty"  jsonschema:"Destination folder name (auto-creates if not found)"`
			IsTodo     bool     `json:"is_todo,omitempty"      jsonschema:"Whether the note is a to-do item"`
			TagNames   []string `json:"tag_names,omitempty"    jsonschema:"Tag names to apply (auto-creates missing tags)"`
		}) (*mcp.CallToolResult, any, error) {
			if args.Title == "" {
				return toolError("title is required", "")
			}

			folderID, folderTitle, err := resolveFolderID(ctx, c, fc, args.FolderID, args.FolderName, true)
			if err != nil {
				return handleErr(err)
			}

			params := joplin.NoteCreateParams{
				Title:    args.Title,
				Body:     args.Body,
				ParentID: folderID,
			}
			if args.IsTodo {
				params.IsTodo = 1
			}

			note, err := c.CreateNote(ctx, params)
			if err != nil {
				return handleErr(err)
			}

			appliedTags, tagWarnings := applyTags(ctx, c, note.ID, args.TagNames)

			slim := note.ToSlim(folderTitle)

			return toolSuccess(map[string]any{
				"note":         slim,
				"applied_tags": appliedTags,
				"tag_warnings": tagWarnings,
			})
		})

	mcp.AddTool(s, &mcp.Tool{Name: "update_note", Description: "Update an existing note's title, body, folder, or to-do status. Set append=true to append content instead of replacing. folder_name auto-creates if not found."},
		func(ctx context.Context, req *mcp.CallToolRequest, args struct {
			NoteID     string  `json:"note_id"               jsonschema:"The note ID to update"`
			Title      *string `json:"title,omitempty"       jsonschema:"New title"`
			Body       *string `json:"body,omitempty"        jsonschema:"New body (or content to append when append=true)"`
			Append     bool    `json:"append,omitempty"      jsonschema:"If true append body to existing content instead of replacing"`
			FolderID   *string `json:"folder_id,omitempty"   jsonschema:"New folder ID"`
			FolderName string  `json:"folder_name,omitempty" jsonschema:"New folder name (auto-creates if not found)"`
			IsTodo     *bool   `json:"is_todo,omitempty"     jsonschema:"Set to-do status"`
		}) (*mcp.CallToolResult, any, error) {
			if args.NoteID == "" {
				return toolError("note_id is required", "")
			}

			params := joplin.NoteUpdateParams{
				Title: args.Title,
			}

			// Handle append mode: read current body, concatenate
			if args.Append && args.Body != nil {
				existing, err := c.GetNote(ctx, args.NoteID)
				if err != nil {
					return handleErr(err)
				}
				newBody := existing.Body + "\n" + *args.Body
				params.Body = &newBody
			} else {
				params.Body = args.Body
			}

			// Resolve folder
			var folderID string
			if args.FolderID != nil {
				folderID = *args.FolderID
			}
			resolvedID, _, err := resolveFolderID(ctx, c, fc, folderID, args.FolderName, true)
			if err != nil {
				return handleErr(err)
			}
			if resolvedID != "" {
				params.ParentID = joplin.StringPtr(resolvedID)
			}

			if args.IsTodo != nil {
				if *args.IsTodo {
					params.IsTodo = joplin.IntPtr(1)
				} else {
					params.IsTodo = joplin.IntPtr(0)
				}
			}

			note, err := c.UpdateNote(ctx, args.NoteID, params)
			if err != nil {
				return handleErr(err)
			}

			folderTitle := fc.GetTitle(note.ParentID)
			slim := note.ToSlim(folderTitle)
			return toolSuccess(slim)
		})

	mcp.AddTool(s, &mcp.Tool{Name: "delete_note", Description: "Delete a note by ID. By default moves to trash; set permanent=true to bypass trash."},
		func(ctx context.Context, req *mcp.CallToolRequest, args struct {
			NoteID    string `json:"note_id"              jsonschema:"The note ID to delete"`
			Permanent bool   `json:"permanent,omitempty"  jsonschema:"If true bypass trash and delete permanently (default false)"`
		}) (*mcp.CallToolResult, any, error) {
			if args.NoteID == "" {
				return toolError("note_id is required", "")
			}

			if err := c.DeleteNote(ctx, args.NoteID, args.Permanent); err != nil {
				return handleErr(err)
			}

			msg := fmt.Sprintf("Note %s moved to trash.", args.NoteID)
			if args.Permanent {
				msg = fmt.Sprintf("Note %s permanently deleted.", args.NoteID)
			}
			return toolSuccess(map[string]string{"status": msg})
		})
}
