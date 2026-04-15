package tools

import (
	"context"
	"fmt"

	"github.com/Wickes1/joplin-mcp/joplin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// RegisterTagTools registers the 5 tag-related MCP tools onto the server.
func RegisterTagTools(s *mcp.Server, c *joplin.Client, fc *FolderCache) {
	mcp.AddTool(s, &mcp.Tool{Name: "list_tags", Description: "List all tags in the Joplin library."},
		func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
			tags, err := c.ListTags(ctx)
			if err != nil {
				if ae, ok := err.(*joplin.AgentError); ok {
					return toolErrorFromAgent(ae)
				}
				return toolError(err.Error(), "")
			}
			return toolSuccess(tags)
		})

	mcp.AddTool(s, &mcp.Tool{Name: "tag_note", Description: "Apply a tag to a note. Creates the tag if it does not exist (case-insensitive lookup)."},
		func(ctx context.Context, req *mcp.CallToolRequest, args struct {
			TagName string `json:"tag_name" jsonschema:"description=Tag name to apply (case-insensitive; created if missing),required"`
			NoteID  string `json:"note_id"  jsonschema:"description=Note ID to tag,required"`
		}) (*mcp.CallToolResult, any, error) {
			if args.TagName == "" {
				return toolError("tag_name is required", "")
			}
			if args.NoteID == "" {
				return toolError("note_id is required", "")
			}

			allTags, err := c.ListTags(ctx)
			if err != nil {
				if ae, ok := err.(*joplin.AgentError); ok {
					return toolErrorFromAgent(ae)
				}
				return toolError(err.Error(), "")
			}

			tag := FindTagByName(allTags, args.TagName)
			if tag == nil {
				newTag, createErr := c.CreateTag(ctx, args.TagName)
				if createErr != nil {
					if ae, ok := createErr.(*joplin.AgentError); ok {
						return toolErrorFromAgent(ae)
					}
					return toolError(fmt.Sprintf("failed to create tag %q: %s", args.TagName, createErr.Error()), "")
				}
				tag = newTag
			}

			if err := c.TagNote(ctx, tag.ID, args.NoteID); err != nil {
				if ae, ok := err.(*joplin.AgentError); ok {
					return toolErrorFromAgent(ae)
				}
				return toolError(fmt.Sprintf("failed to associate tag: %s", err.Error()), "")
			}

			return toolSuccess(map[string]string{
				"status":   "tag applied",
				"tag_id":   tag.ID,
				"tag_name": tag.Title,
				"note_id":  args.NoteID,
			})
		})

	mcp.AddTool(s, &mcp.Tool{Name: "untag_note", Description: "Remove a tag from a note (case-insensitive tag lookup)."},
		func(ctx context.Context, req *mcp.CallToolRequest, args struct {
			TagName string `json:"tag_name" jsonschema:"description=Tag name to remove (case-insensitive),required"`
			NoteID  string `json:"note_id"  jsonschema:"description=Note ID to untag,required"`
		}) (*mcp.CallToolResult, any, error) {
			if args.TagName == "" {
				return toolError("tag_name is required", "")
			}
			if args.NoteID == "" {
				return toolError("note_id is required", "")
			}

			allTags, err := c.ListTags(ctx)
			if err != nil {
				if ae, ok := err.(*joplin.AgentError); ok {
					return toolErrorFromAgent(ae)
				}
				return toolError(err.Error(), "")
			}

			tag := FindTagByName(allTags, args.TagName)
			if tag == nil {
				ae := joplin.TagNotFound(args.TagName)
				return toolErrorFromAgent(ae)
			}

			if err := c.UntagNote(ctx, tag.ID, args.NoteID); err != nil {
				if ae, ok := err.(*joplin.AgentError); ok {
					return toolErrorFromAgent(ae)
				}
				return toolError(fmt.Sprintf("failed to remove tag: %s", err.Error()), "")
			}

			return toolSuccess(map[string]string{
				"status":   "tag removed",
				"tag_id":   tag.ID,
				"tag_name": tag.Title,
				"note_id":  args.NoteID,
			})
		})

	mcp.AddTool(s, &mcp.Tool{Name: "delete_tag", Description: "Delete a tag by ID. This removes the tag from all notes."},
		func(ctx context.Context, req *mcp.CallToolRequest, args struct {
			TagID string `json:"tag_id" jsonschema:"description=The tag ID to delete,required"`
		}) (*mcp.CallToolResult, any, error) {
			if args.TagID == "" {
				return toolError("tag_id is required", "")
			}

			if err := c.DeleteTag(ctx, args.TagID); err != nil {
				if ae, ok := err.(*joplin.AgentError); ok {
					return toolErrorFromAgent(ae)
				}
				return toolError(err.Error(), "")
			}

			return toolSuccess(map[string]string{
				"status": fmt.Sprintf("Tag %s deleted.", args.TagID),
			})
		})

	mcp.AddTool(s, &mcp.Tool{Name: "get_notes_by_tag", Description: "Get notes associated with a tag name (case-insensitive). Returns slim notes and a has_more flag."},
		func(ctx context.Context, req *mcp.CallToolRequest, args struct {
			TagName string `json:"tag_name"        jsonschema:"description=Tag name to look up (case-insensitive),required"`
			Limit   int    `json:"limit,omitempty"  jsonschema:"description=Max results per page (default 20)"`
			Page    int    `json:"page,omitempty"   jsonschema:"description=Page number 1-indexed (default 1)"`
		}) (*mcp.CallToolResult, any, error) {
			if args.TagName == "" {
				return toolError("tag_name is required", "")
			}

			limit := args.Limit
			if limit <= 0 {
				limit = 20
			}
			page := args.Page
			if page <= 0 {
				page = 1
			}

			allTags, err := c.ListTags(ctx)
			if err != nil {
				if ae, ok := err.(*joplin.AgentError); ok {
					return toolErrorFromAgent(ae)
				}
				return toolError(err.Error(), "")
			}

			tag := FindTagByName(allTags, args.TagName)
			if tag == nil {
				ae := joplin.TagNotFound(args.TagName)
				return toolErrorFromAgent(ae)
			}

			resp, err := c.GetNotesByTag(ctx, tag.ID, page, limit)
			if err != nil {
				if ae, ok := err.(*joplin.AgentError); ok {
					return toolErrorFromAgent(ae)
				}
				return toolError(err.Error(), "")
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
}
