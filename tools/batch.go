package tools

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Wickes1/joplin-mcp/joplin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/sync/errgroup"
)

// RegisterBatchTools registers the 3 batch MCP tools onto the server.
func RegisterBatchTools(s *mcp.Server, c joplin.API, fc *FolderCache) {

	// --- batch_move_notes ---
	mcp.AddTool(s, &mcp.Tool{Name: "batch_move_notes", Description: "Move up to 100 notes to a destination folder concurrently."},
		func(ctx context.Context, req *mcp.CallToolRequest, args struct {
			NoteIDs    []string `json:"note_ids"               jsonschema:"List of note IDs to move (max 100)"`
			FolderID   string   `json:"folder_id,omitempty"    jsonschema:"Destination folder ID"`
			FolderName string   `json:"folder_name,omitempty"  jsonschema:"Destination folder name (must exist)"`
		}) (*mcp.CallToolResult, any, error) {
			if len(args.NoteIDs) == 0 {
				return toolError("note_ids is required and must not be empty", "")
			}
			if len(args.NoteIDs) > 100 {
				return toolError("note_ids must contain at most 100 IDs", "Split into smaller batches.")
			}
			if args.FolderID == "" && args.FolderName == "" {
				return toolError("folder_id or folder_name is required", "")
			}

			folderID, _, err := resolveFolderID(ctx, c, fc, args.FolderID, args.FolderName, false)
			if err != nil {
				return handleErr(err)
			}

			batchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()

			var mu sync.Mutex
			result := joplin.BatchResult{}

			g, gCtx := errgroup.WithContext(batchCtx)
			g.SetLimit(5)

			for _, id := range args.NoteIDs {
				id := id
				g.Go(func() error {
					_, err := c.UpdateNote(gCtx, id, joplin.NoteUpdateParams{
						ParentID: joplin.StringPtr(folderID),
					})
					mu.Lock()
					defer mu.Unlock()
					if err != nil {
						result.Failed++
						result.Errors = append(result.Errors, joplin.BatchItemError{ID: id, Error: err.Error()})
					} else {
						result.Succeeded++
					}
					return nil
				})
			}

			if err := g.Wait(); err != nil {
				return toolError(err.Error(), "")
			}

			return toolSuccess(result)
		})

	// --- merge_notes ---
	mcp.AddTool(s, &mcp.Tool{Name: "merge_notes", Description: "Fetch up to 50 notes, concatenate their bodies with headings and separators, create a new merged note, and optionally apply tags."},
		func(ctx context.Context, req *mcp.CallToolRequest, args struct {
			NoteIDs    []string `json:"note_ids"               jsonschema:"List of source note IDs to merge (max 50)"`
			Title      string   `json:"title"                  jsonschema:"Title of the merged note"`
			FolderID   string   `json:"folder_id,omitempty"    jsonschema:"Destination folder ID (defaults to first note's folder)"`
			FolderName string   `json:"folder_name,omitempty"  jsonschema:"Destination folder name (auto-creates if not found)"`
			TagNames   []string `json:"tag_names,omitempty"    jsonschema:"Tag names to apply to the merged note"`
		}) (*mcp.CallToolResult, any, error) {
			if len(args.NoteIDs) == 0 {
				return toolError("note_ids is required and must not be empty", "")
			}
			if len(args.NoteIDs) > 50 {
				return toolError("note_ids must contain at most 50 IDs", "Split into smaller batches.")
			}
			if args.Title == "" {
				return toolError("title is required", "")
			}

			batchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()

			type noteResult struct {
				idx  int
				note *joplin.Note
				err  string
			}

			noteResults := make([]noteResult, len(args.NoteIDs))
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
						noteResults[i] = noteResult{idx: i, err: err.Error()}
					} else {
						noteResults[i] = noteResult{idx: i, note: note}
					}
					return nil
				})
			}

			if err := g.Wait(); err != nil {
				return toolError(err.Error(), "")
			}

			// Determine folder: use specified folder, or fall back to first successful note's folder
			folderID := args.FolderID
			folderName := args.FolderName

			if folderID == "" && folderName == "" {
				for _, r := range noteResults {
					if r.note != nil {
						folderID = r.note.ParentID
						break
					}
				}
			}

			resolvedFolderID, folderTitle, err := resolveFolderID(ctx, c, fc, folderID, folderName, true)
			if err != nil {
				return handleErr(err)
			}

			// Build merged body
			var parts []string
			for _, r := range noteResults {
				if r.note != nil {
					parts = append(parts, fmt.Sprintf("## %s\n\n%s", r.note.Title, r.note.Body))
				}
			}
			mergedBody := strings.Join(parts, "\n\n---\n\n")

			// Create merged note
			newNote, err := c.CreateNote(ctx, joplin.NoteCreateParams{
				Title:    args.Title,
				Body:     mergedBody,
				ParentID: resolvedFolderID,
			})
			if err != nil {
				return handleErr(err)
			}

			applyTags(ctx, c, newNote.ID, args.TagNames)

			return toolSuccess(map[string]any{
				"merged_note_id": newNote.ID,
				"title":          newNote.Title,
				"folder_title":   folderTitle,
				"source_count":   len(parts),
				"body_length":    len(mergedBody),
			})
		})

	// --- batch_tag_notes ---
	mcp.AddTool(s, &mcp.Tool{Name: "batch_tag_notes", Description: "Add or remove a tag from up to 100 notes concurrently. The tag is created automatically when adding."},
		func(ctx context.Context, req *mcp.CallToolRequest, args struct {
			NoteIDs []string `json:"note_ids"          jsonschema:"List of note IDs (max 100)"`
			TagName string   `json:"tag_name"          jsonschema:"Tag name to add or remove"`
			Action  string   `json:"action,omitempty"  jsonschema:"\"add\" or \"remove\" (default \"add\")"`
		}) (*mcp.CallToolResult, any, error) {
			if len(args.NoteIDs) == 0 {
				return toolError("note_ids is required and must not be empty", "")
			}
			if len(args.NoteIDs) > 100 {
				return toolError("note_ids must contain at most 100 IDs", "Split into smaller batches.")
			}
			if args.TagName == "" {
				return toolError("tag_name is required", "")
			}

			action := args.Action
			if action == "" {
				action = "add"
			}
			if action != "add" && action != "remove" {
				return toolError("action must be \"add\" or \"remove\"", "")
			}

			// Resolve or create the tag
			allTags, err := c.ListTags(ctx)
			if err != nil {
				return handleErr(err)
			}

			tag := FindTagByName(allTags, args.TagName)
			if tag == nil {
				if action == "add" {
					newTag, err := c.CreateTag(ctx, args.TagName)
					if err != nil {
						return handleErr(err)
					}
					tag = newTag
				} else {
					// remove: tag doesn't exist, nothing to do
					return toolSuccess(map[string]any{
						"succeeded": 0,
						"failed":    0,
						"errors":    []joplin.BatchItemError{},
						"tag_name":  args.TagName,
						"action":    action,
					})
				}
			}

			batchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()

			var mu sync.Mutex
			result := joplin.BatchResult{}
			tagID := tag.ID

			g, gCtx := errgroup.WithContext(batchCtx)
			g.SetLimit(5)

			for _, id := range args.NoteIDs {
				id := id
				g.Go(func() error {
					var opErr error
					if action == "add" {
						opErr = c.TagNote(gCtx, tagID, id)
					} else {
						opErr = c.UntagNote(gCtx, tagID, id)
					}
					mu.Lock()
					defer mu.Unlock()
					if opErr != nil {
						result.Failed++
						result.Errors = append(result.Errors, joplin.BatchItemError{ID: id, Error: opErr.Error()})
					} else {
						result.Succeeded++
					}
					return nil
				})
			}

			if err := g.Wait(); err != nil {
				return toolError(err.Error(), "")
			}

			return toolSuccess(map[string]any{
				"succeeded": result.Succeeded,
				"failed":    result.Failed,
				"errors":    result.Errors,
				"tag_name":  args.TagName,
				"action":    action,
			})
		})
}
