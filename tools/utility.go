package tools

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Wickes1/joplin-mcp/joplin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// RegisterUtilityTools registers the 2 utility MCP tools onto the server.
func RegisterUtilityTools(s *mcp.Server, c joplin.API, fc *FolderCache) {
	mcp.AddTool(s, &mcp.Tool{Name: "import_markdown", Description: "Import a Markdown (.md) file into Joplin as a note. Title is extracted from the first '# ' heading (first 10 lines) or the filename."},
		func(ctx context.Context, req *mcp.CallToolRequest, args struct {
			FilePath   string   `json:"file_path"              jsonschema:"Absolute path to the .md file to import"`
			FolderID   string   `json:"folder_id,omitempty"    jsonschema:"Destination folder ID"`
			FolderName string   `json:"folder_name,omitempty"  jsonschema:"Destination folder name (auto-creates if not found)"`
			TagNames   []string `json:"tag_names,omitempty"    jsonschema:"Tag names to apply (auto-creates missing tags)"`
		}) (*mcp.CallToolResult, any, error) {
			if args.FilePath == "" {
				return toolError("file_path is required", "")
			}

			if err := validateAbsPath(args.FilePath); err != nil {
				return toolError(err.Error(), "Provide an absolute path to the .md file.")
			}

			// Validate extension
			ext := strings.ToLower(filepath.Ext(args.FilePath))
			if ext != ".md" {
				return handleErr(joplin.InvalidFileExtension(ext))
			}

			// Stat file and reject if > 50MB
			fileInfo, err := os.Stat(args.FilePath)
			if err != nil {
				return toolError(fmt.Sprintf("failed to stat file %q: %s", args.FilePath, err.Error()), "Check that the file exists and is readable.")
			}
			if fileInfo.Size() > 50*1024*1024 {
				return toolError("file too large (max 50MB)", "")
			}

			// Read file
			data, err := os.ReadFile(args.FilePath)
			if err != nil {
				return toolError(fmt.Sprintf("failed to read file %q: %s", args.FilePath, err.Error()), "Check that the file exists and is readable.")
			}
			body := string(data)

			// Extract title from first "# " heading (first 10 lines) or use filename
			title := extractMarkdownTitle(body, args.FilePath)

			// Resolve or auto-create folder
			folderID, folderTitle, err := resolveFolderID(ctx, c, fc, args.FolderID, args.FolderName, true)
			if err != nil {
				return handleErr(err)
			}

			// Create note
			params := joplin.NoteCreateParams{
				Title:    title,
				Body:     body,
				ParentID: folderID,
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

	mcp.AddTool(s, &mcp.Tool{Name: "get_status", Description: "Check Joplin connectivity and return library statistics (folder count, tag count, port)."},
		func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
			if err := c.Ping(ctx); err != nil {
				return handleErr(err)
			}

			// Count folders (recursive walk of tree)
			folders, err := fc.AllFolders()
			if err != nil {
				return handleErr(err)
			}
			folderCount := countFolders(folders)

			// Count tags
			tags, err := c.ListTags(ctx)
			if err != nil {
				return handleErr(err)
			}

			return toolSuccess(map[string]any{
				"status":       "ok",
				"port":         c.Port(),
				"folder_count": folderCount,
				"tag_count":    len(tags),
			})
		})
}

// extractMarkdownTitle scans the first 10 lines of body for a "# " heading.
// Falls back to the filename (without extension) if no heading is found.
func extractMarkdownTitle(body, filePath string) string {
	scanner := bufio.NewScanner(strings.NewReader(body))
	lineCount := 0
	for scanner.Scan() && lineCount < 10 {
		lineCount++
		line := scanner.Text()
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	// Fallback to filename
	base := filepath.Base(filePath)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// countFolders recursively counts all folders in the tree.
func countFolders(folders []*joplin.Folder) int {
	count := 0
	for _, f := range folders {
		if f == nil {
			continue
		}
		count++
		if len(f.Children) > 0 {
			count += countFolders(f.Children)
		}
	}
	return count
}
