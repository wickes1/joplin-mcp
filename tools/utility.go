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
func RegisterUtilityTools(s *mcp.Server, c *joplin.Client, fc *FolderCache) {
	mcp.AddTool(s, &mcp.Tool{Name: "import_markdown", Description: "Import a Markdown (.md) file into Joplin as a note. Title is extracted from the first '# ' heading or the filename."},
		func(ctx context.Context, req *mcp.CallToolRequest, args struct {
			FilePath   string   `json:"file_path"              jsonschema:"Absolute path to the .md file to import"`
			FolderID   string   `json:"folder_id,omitempty"    jsonschema:"Destination folder ID"`
			FolderName string   `json:"folder_name,omitempty"  jsonschema:"Destination folder name (auto-creates if not found)"`
			TagNames   []string `json:"tag_names,omitempty"    jsonschema:"Tag names to apply (auto-creates missing tags)"`
		}) (*mcp.CallToolResult, any, error) {
			if args.FilePath == "" {
				return toolError("file_path is required", "")
			}

			// Validate extension
			ext := strings.ToLower(filepath.Ext(args.FilePath))
			if ext != ".md" {
				ae := joplin.InvalidFileExtension(ext)
				return toolErrorFromAgent(ae)
			}

			// Read file
			data, err := os.ReadFile(args.FilePath)
			if err != nil {
				return toolError(fmt.Sprintf("failed to read file %q: %s", args.FilePath, err.Error()), "Check that the file exists and is readable.")
			}
			body := string(data)

			// Extract title from first "# " heading or use filename
			title := extractMarkdownTitle(body)
			if title == "" {
				base := filepath.Base(args.FilePath)
				title = strings.TrimSuffix(base, filepath.Ext(base))
			}

			// Resolve or auto-create folder
			folderID := args.FolderID
			if args.FolderName != "" && folderID == "" {
				existing := fc.FindByName(args.FolderName)
				if existing != nil {
					folderID = existing.ID
				} else {
					newFolder, createErr := c.CreateFolder(ctx, args.FolderName, "")
					if createErr != nil {
						if ae, ok := createErr.(*joplin.AgentError); ok {
							return toolErrorFromAgent(ae)
						}
						return toolError(fmt.Sprintf("failed to create folder %q: %s", args.FolderName, createErr.Error()), "")
					}
					fc.Invalidate()
					folderID = newFolder.ID
				}
			}

			// Create note
			params := joplin.NoteCreateParams{
				Title:    title,
				Body:     body,
				ParentID: folderID,
			}
			note, err := c.CreateNote(ctx, params)
			if err != nil {
				if ae, ok := err.(*joplin.AgentError); ok {
					return toolErrorFromAgent(ae)
				}
				return toolError(err.Error(), "")
			}

			// Apply tags
			var tagWarnings []string
			appliedTags := make([]string, 0, len(args.TagNames))

			if len(args.TagNames) > 0 {
				allTags, listErr := c.ListTags(ctx)
				if listErr != nil {
					tagWarnings = append(tagWarnings, fmt.Sprintf("failed to list tags: %s", listErr.Error()))
				} else {
					for _, tagName := range args.TagNames {
						tag := FindTagByName(allTags, tagName)
						if tag == nil {
							newTag, createErr := c.CreateTag(ctx, tagName)
							if createErr != nil {
								tagWarnings = append(tagWarnings, fmt.Sprintf("failed to create tag %q: %s", tagName, createErr.Error()))
								continue
							}
							tag = newTag
							allTags = append(allTags, *newTag)
						}
						if assocErr := c.TagNote(ctx, tag.ID, note.ID); assocErr != nil {
							tagWarnings = append(tagWarnings, fmt.Sprintf("failed to apply tag %q: %s", tagName, assocErr.Error()))
							continue
						}
						appliedTags = append(appliedTags, tag.Title)
					}
				}
			}

			folderTitle := fc.GetTitle(note.ParentID)
			full := note.ToFull(folderTitle, appliedTags)

			return toolSuccess(map[string]any{
				"note":         full,
				"tag_warnings": tagWarnings,
			})
		})

	mcp.AddTool(s, &mcp.Tool{Name: "get_status", Description: "Check Joplin connectivity and return library statistics (folder count, tag count, port)."},
		func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
			if err := c.Ping(ctx); err != nil {
				if ae, ok := err.(*joplin.AgentError); ok {
					return toolErrorFromAgent(ae)
				}
				return toolError(err.Error(), "Ensure Joplin desktop is running and Web Clipper is enabled in Preferences.")
			}

			// Count folders (recursive walk of tree)
			folders, err := fc.AllFolders()
			if err != nil {
				if ae, ok := err.(*joplin.AgentError); ok {
					return toolErrorFromAgent(ae)
				}
				return toolError(err.Error(), "")
			}
			folderCount := countFolders(folders)

			// Count tags
			tags, err := c.ListTags(ctx)
			if err != nil {
				if ae, ok := err.(*joplin.AgentError); ok {
					return toolErrorFromAgent(ae)
				}
				return toolError(err.Error(), "")
			}

			return toolSuccess(map[string]any{
				"status":       "ok",
				"port":         c.Port(),
				"folder_count": folderCount,
				"tag_count":    len(tags),
			})
		})
}

// extractMarkdownTitle scans the body for the first "# " heading and returns its text.
// Returns "" if no h1 heading is found.
func extractMarkdownTitle(body string) string {
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	return ""
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
