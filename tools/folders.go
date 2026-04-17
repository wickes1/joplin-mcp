package tools

import (
	"context"
	"fmt"

	"github.com/Wickes1/joplin-mcp/joplin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// RegisterFolderTools registers the 4 folder-related MCP tools onto the server.
func RegisterFolderTools(s *mcp.Server, c joplin.API, fc *FolderCache) {
	mcp.AddTool(s, &mcp.Tool{Name: "list_folders", Description: "List all folders as a nested tree with computed paths (e.g. Work/Projects/Q1)."},
		func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
			rawFolders, err := fc.AllFolders()
			if err != nil {
				return handleErr(err)
			}

			tree := convertFolderTree(rawFolders, "", fc)
			return toolSuccess(tree)
		})

	mcp.AddTool(s, &mcp.Tool{Name: "create_folder", Description: "Create a new folder, optionally nested under a parent folder."},
		func(ctx context.Context, req *mcp.CallToolRequest, args struct {
			Title    string `json:"title"              jsonschema:"Folder name"`
			ParentID string `json:"parent_id,omitempty" jsonschema:"Parent folder ID (optional — omit for top-level)"`
		}) (*mcp.CallToolResult, any, error) {
			if args.Title == "" {
				return toolError("title is required", "")
			}

			folder, err := c.CreateFolder(ctx, args.Title, args.ParentID)
			if err != nil {
				return handleErr(err)
			}

			// Invalidate cache so subsequent operations see the new folder
			fc.Invalidate()

			path := fc.ComputePath(folder.ID)
			if path == "" {
				// Cache just invalidated — compute path manually
				if args.ParentID != "" {
					parentPath := fc.ComputePath(args.ParentID)
					if parentPath != "" {
						path = parentPath + "/" + folder.Title
					} else {
						path = folder.Title
					}
				} else {
					path = folder.Title
				}
			}

			return toolSuccess(map[string]any{
				"id":        folder.ID,
				"title":     folder.Title,
				"parent_id": folder.ParentID,
				"path":      path,
			})
		})

	mcp.AddTool(s, &mcp.Tool{Name: "delete_folder", Description: "Delete a folder by ID. By default moves to trash; set permanent=true to bypass trash."},
		func(ctx context.Context, req *mcp.CallToolRequest, args struct {
			FolderID  string `json:"folder_id"            jsonschema:"The folder ID to delete"`
			Permanent bool   `json:"permanent,omitempty"  jsonschema:"If true bypass trash and delete permanently (default false)"`
		}) (*mcp.CallToolResult, any, error) {
			if args.FolderID == "" {
				return toolError("folder_id is required", "")
			}

			if err := c.DeleteFolder(ctx, args.FolderID, args.Permanent); err != nil {
				return handleErr(err)
			}

			fc.Invalidate()

			msg := fmt.Sprintf("Folder %s moved to trash.", args.FolderID)
			if args.Permanent {
				msg = fmt.Sprintf("Folder %s permanently deleted.", args.FolderID)
			}
			return toolSuccess(map[string]string{"status": msg})
		})

	mcp.AddTool(s, &mcp.Tool{Name: "update_folder", Description: "Rename or move a folder."},
		func(ctx context.Context, req *mcp.CallToolRequest, args struct {
			FolderID string  `json:"folder_id"             jsonschema:"The folder ID to update"`
			Title    *string `json:"title,omitempty"       jsonschema:"New title"`
			ParentID *string `json:"parent_id,omitempty"   jsonschema:"New parent folder ID (move)"`
		}) (*mcp.CallToolResult, any, error) {
			if args.FolderID == "" {
				return toolError("folder_id is required", "")
			}
			if args.Title == nil && args.ParentID == nil {
				return toolError("at least one of title or parent_id must be provided", "")
			}

			params := joplin.FolderUpdateParams{
				Title:    args.Title,
				ParentID: args.ParentID,
			}

			folder, err := c.UpdateFolder(ctx, args.FolderID, params)
			if err != nil {
				return handleErr(err)
			}

			fc.Invalidate()

			path := fc.ComputePath(folder.ID)
			if path == "" {
				path = folder.Title
			}

			return toolSuccess(map[string]any{
				"id":        folder.ID,
				"title":     folder.Title,
				"parent_id": folder.ParentID,
				"path":      path,
			})
		})
}

// convertFolderTree recursively converts a []*joplin.Folder into []*joplin.FolderTree,
// computing the full path for each folder.
func convertFolderTree(folders []*joplin.Folder, parentPath string, fc *FolderCache) []*joplin.FolderTree {
	result := make([]*joplin.FolderTree, 0, len(folders))
	for _, f := range folders {
		if f == nil {
			continue
		}

		var path string
		if parentPath == "" {
			path = f.Title
		} else {
			path = parentPath + "/" + f.Title
		}

		node := &joplin.FolderTree{
			ID:       f.ID,
			Title:    f.Title,
			ParentID: f.ParentID,
			Path:     path,
			Children: convertFolderTree(f.Children, path, fc),
		}
		result = append(result, node)
	}
	return result
}
