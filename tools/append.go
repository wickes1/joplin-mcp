package tools

import (
	"context"
	"fmt"

	"github.com/Wickes1/joplin-mcp/joplin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// RegisterAppendTool registers the append_to_note MCP tool onto the server.
func RegisterAppendTool(s *mcp.Server, c *joplin.Client, fc *FolderCache) {
	mcp.AddTool(s, &mcp.Tool{Name: "append_to_note", Description: "Append content to an existing note (read-modify-write). A newline is inserted between existing body and new content."},
		func(ctx context.Context, req *mcp.CallToolRequest, args struct {
			NoteID  string `json:"note_id" jsonschema:"description=The note ID to append to,required"`
			Content string `json:"content" jsonschema:"description=Content to append to the note body,required"`
		}) (*mcp.CallToolResult, any, error) {
			if args.NoteID == "" {
				return toolError("note_id is required", "")
			}
			if args.Content == "" {
				return toolError("content is required", "")
			}

			// GET current body
			note, err := c.GetNote(ctx, args.NoteID)
			if err != nil {
				if ae, ok := err.(*joplin.AgentError); ok {
					return toolErrorFromAgent(ae)
				}
				return toolError(err.Error(), "")
			}

			// Append with newline separator
			newBody := note.Body + "\n" + args.Content

			// PUT updated body
			updated, err := c.UpdateNote(ctx, args.NoteID, joplin.NoteUpdateParams{
				Body: joplin.StringPtr(newBody),
			})
			if err != nil {
				if ae, ok := err.(*joplin.AgentError); ok {
					return toolErrorFromAgent(ae)
				}
				return toolError(fmt.Sprintf("failed to update note: %s", err.Error()), "")
			}

			tags, _ := c.GetNoteTags(ctx, updated.ID)
			tagNames := make([]string, 0, len(tags))
			for _, t := range tags {
				tagNames = append(tagNames, t.Title)
			}

			folderTitle := fc.GetTitle(updated.ParentID)
			full := updated.ToFull(folderTitle, tagNames)
			return toolSuccess(full)
		})
}
