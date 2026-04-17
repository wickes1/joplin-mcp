package tools

import (
	"context"

	"github.com/Wickes1/joplin-mcp/joplin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// RegisterSearchTools registers the search_notes MCP tool onto the server.
func RegisterSearchTools(s *mcp.Server, c joplin.API, fc *FolderCache) {
	mcp.AddTool(s, &mcp.Tool{Name: "search_notes", Description: "Full-text search across all notes. Returns preview notes (200 char body preview) and a has_more flag."},
		func(ctx context.Context, req *mcp.CallToolRequest, args struct {
			Query string `json:"query"          jsonschema:"Search query string"`
			Limit int    `json:"limit,omitempty" jsonschema:"Max results (default 20 max 50)"`
			Page  int    `json:"page,omitempty"  jsonschema:"Page number 1-indexed (default 1)"`
		}) (*mcp.CallToolResult, any, error) {
			if args.Query == "" {
				return toolError("query is required", "")
			}

			limit := args.Limit
			if limit <= 0 {
				limit = 20
			}
			if limit > 50 {
				limit = 50
			}

			page := args.Page
			if page <= 0 {
				page = 1
			}

			resp, err := c.SearchNotes(ctx, args.Query, page, limit)
			if err != nil {
				return handleErr(err)
			}

			previews := make([]joplin.PreviewNote, 0, len(resp.Items))
			for i := range resp.Items {
				n := &resp.Items[i]
				folderTitle := fc.GetTitle(n.ParentID)
				previews = append(previews, n.ToPreview(folderTitle))
			}

			return toolSuccess(map[string]any{
				"notes":    previews,
				"has_more": resp.HasMore,
			})
		})
}
