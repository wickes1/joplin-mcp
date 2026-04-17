package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Wickes1/joplin-mcp/joplin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// RegisterResourceTools registers the 4 resource-related MCP tools onto the server.
func RegisterResourceTools(s *mcp.Server, c joplin.API, fc *FolderCache) {

	// --- list_resources ---
	mcp.AddTool(s, &mcp.Tool{Name: "list_resources", Description: "List Joplin resources (attachments). Returns a page of resources and a has_more flag."},
		func(ctx context.Context, req *mcp.CallToolRequest, args struct {
			Limit int `json:"limit,omitempty" jsonschema:"Max results per page (default 20, max 100)"`
			Page  int `json:"page,omitempty"  jsonschema:"Page number 1-indexed (default 1)"`
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

			resp, err := c.ListResources(ctx, page, limit)
			if err != nil {
				return handleErr(err)
			}

			resources := make([]joplin.ResourceResponse, 0, len(resp.Items))
			for i := range resp.Items {
				resources = append(resources, resp.Items[i].ToSlim())
			}

			return toolSuccess(map[string]any{
				"resources": resources,
				"has_more":  resp.HasMore,
			})
		})

	// --- get_resource ---
	mcp.AddTool(s, &mcp.Tool{Name: "get_resource", Description: "Get metadata for a single Joplin resource (attachment) by ID."},
		func(ctx context.Context, req *mcp.CallToolRequest, args struct {
			ResourceID string `json:"resource_id" jsonschema:"The resource ID to retrieve"`
		}) (*mcp.CallToolResult, any, error) {
			if args.ResourceID == "" {
				return toolError("resource_id is required", "")
			}

			resource, err := c.GetResource(ctx, args.ResourceID)
			if err != nil {
				return handleErr(err)
			}

			return toolSuccess(resource.ToSlim())
		})

	// --- download_resource ---
	mcp.AddTool(s, &mcp.Tool{Name: "download_resource", Description: "Download a Joplin resource (attachment) file to a local path on disk."},
		func(ctx context.Context, req *mcp.CallToolRequest, args struct {
			ResourceID string `json:"resource_id"  jsonschema:"The resource ID to download"`
			OutputPath string `json:"output_path"  jsonschema:"Absolute path where the file will be written"`
		}) (*mcp.CallToolResult, any, error) {
			if args.ResourceID == "" {
				return toolError("resource_id is required", "")
			}
			if args.OutputPath == "" {
				return toolError("output_path is required", "")
			}
			if err := validateAbsPath(args.OutputPath); err != nil {
				return toolError(err.Error(), "Provide an absolute path for output_path.")
			}

			// Fetch metadata
			resource, err := c.GetResource(ctx, args.ResourceID)
			if err != nil {
				return handleErr(err)
			}

			// Fetch binary content
			data, err := c.GetResourceFile(ctx, args.ResourceID)
			if err != nil {
				return handleErr(err)
			}

			// Ensure parent directory exists
			if err := os.MkdirAll(filepath.Dir(args.OutputPath), 0700); err != nil {
				return toolError(fmt.Sprintf("failed to create output directory: %s", err.Error()), "")
			}

			// Write to disk
			if err := os.WriteFile(args.OutputPath, data, 0600); err != nil {
				return toolError(fmt.Sprintf("failed to write file: %s", err.Error()), "Check that the path is writable.")
			}

			return toolSuccess(map[string]any{
				"resource_id": resource.ID,
				"filename":    resource.Filename,
				"size":        resource.Size,
				"output_path": args.OutputPath,
			})
		})

	// --- upload_resource ---
	mcp.AddTool(s, &mcp.Tool{Name: "upload_resource", Description: "Upload a local file as a Joplin resource (attachment). To attach to a note, use the returned ID: ![alt](:/id) for images or [name](:/id) for files."},
		func(ctx context.Context, req *mcp.CallToolRequest, args struct {
			FilePath string `json:"file_path"        jsonschema:"Absolute path to the file to upload"`
			Title    string `json:"title,omitempty"  jsonschema:"Resource title (defaults to the filename if omitted)"`
		}) (*mcp.CallToolResult, any, error) {
			if args.FilePath == "" {
				return toolError("file_path is required", "")
			}
			if err := validateAbsPath(args.FilePath); err != nil {
				return toolError(err.Error(), "Provide an absolute path for file_path.")
			}

			// Default title to filename
			title := args.Title
			if title == "" {
				title = filepath.Base(args.FilePath)
			}

			resource, err := c.CreateResource(ctx, args.FilePath, title)
			if err != nil {
				return handleErr(err)
			}

			return toolSuccess(resource.ToSlim())
		})
}
