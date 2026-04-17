package tools

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Wickes1/joplin-mcp/joplin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// RegisterExportTools registers the export_notes and batch_import_markdown MCP tools.
func RegisterExportTools(s *mcp.Server, c joplin.API, fc *FolderCache) {

	// --- export_notes ---
	mcp.AddTool(s, &mcp.Tool{Name: "export_notes", Description: "Export Joplin notes to the filesystem as Markdown files, preserving folder hierarchy (or flattening) with optional YAML frontmatter."},
		func(ctx context.Context, req *mcp.CallToolRequest, args struct {
			OutputDir       string `json:"output_dir"                  jsonschema:"Absolute path to the output directory (created if absent)"`
			FolderID        string `json:"folder_id,omitempty"         jsonschema:"Export only this folder (and its notes)"`
			IncludeMetadata bool   `json:"include_metadata,omitempty"  jsonschema:"Prepend YAML frontmatter with joplin_id, title, folder, tags, created, updated"`
			Flatten         bool   `json:"flatten,omitempty"           jsonschema:"Write all notes into output_dir without sub-directories"`
		}) (*mcp.CallToolResult, any, error) {
			if args.OutputDir == "" {
				return toolError("output_dir is required", "")
			}
			if err := validateAbsPath(args.OutputDir); err != nil {
				return toolError(err.Error(), "Provide an absolute path for output_dir.")
			}

			// Ensure output directory exists
			if err := os.MkdirAll(args.OutputDir, 0755); err != nil {
				return toolError(fmt.Sprintf("failed to create output directory: %s", err.Error()), "")
			}

			// Determine which folders to export
			var targetFolders []*joplin.Folder
			if args.FolderID != "" {
				// Single folder: create a synthetic slice with just that folder ID
				targetFolders = []*joplin.Folder{{ID: args.FolderID, Title: fc.GetTitle(args.FolderID)}}
			} else {
				// All folders
				all, err := fc.AllFolders()
				if err != nil {
					return handleErr(err)
				}
				// Flatten the tree into a list for iteration
				targetFolders = flattenFolderList(all)
			}

			// Also export notes from the "root" (no parent) when no specific folder is requested
			exportRoot := args.FolderID == ""

			exported := 0
			skipped := 0
			var exportedFolderTitles []string

			// usedPaths tracks filenames used within a directory to handle collisions
			usedPaths := make(map[string]int)

			writeNote := func(note *joplin.Note, folderPath, folderTitle string) error {
				// Fetch full note body
				full, err := c.GetNote(ctx, note.ID)
				if err != nil {
					return err
				}

				// Determine write directory
				var writeDir string
				if args.Flatten || folderTitle == "" {
					writeDir = args.OutputDir
				} else {
					writeDir = filepath.Join(args.OutputDir, filepath.FromSlash(folderPath))
					if err := os.MkdirAll(writeDir, 0755); err != nil {
						return fmt.Errorf("failed to create dir %q: %w", writeDir, err)
					}
				}

				// Resolve filename, handle collisions
				baseName := sanitizeFilename(full.Title)
				if baseName == "" {
					baseName = sanitizeFilename(full.ID)
				}
				collisionKey := strings.ToLower(filepath.Join(writeDir, baseName))
				count := usedPaths[collisionKey]
				usedPaths[collisionKey] = count + 1

				var fileName string
				if count == 0 {
					fileName = baseName + ".md"
				} else {
					fileName = fmt.Sprintf("%s_%d.md", baseName, count+1)
				}

				filePath := filepath.Join(writeDir, fileName)

				// Build content
				var content string
				if args.IncludeMetadata {
					// Fetch tags for frontmatter
					tagObjs, _ := c.GetNoteTags(ctx, full.ID)
					tagTitles := make([]string, 0, len(tagObjs))
					for _, t := range tagObjs {
						tagTitles = append(tagTitles, t.Title)
					}
					content = buildFrontmatter(full, folderTitle, tagTitles) + full.Body
				} else {
					content = full.Body
				}

				if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
					return fmt.Errorf("failed to write %q: %w", filePath, err)
				}
				return nil
			}

			// Export notes from root (no parent folder)
			if exportRoot {
				page := 1
				for {
					resp, err := c.ListNotes(ctx, "", page, 100)
					if err != nil {
						break
					}
					for i := range resp.Items {
						n := &resp.Items[i]
						if n.ParentID == "" {
							if err := writeNote(n, "", ""); err == nil {
								exported++
							} else {
								skipped++
							}
						}
					}
					if !resp.HasMore {
						break
					}
					page++
				}
			}

			// Export notes folder by folder
			for _, folder := range targetFolders {
				folderPath := fc.ComputePath(folder.ID)
				if folderPath == "" {
					folderPath = folder.Title
				}

				page := 1
				folderHadNotes := false
				for {
					resp, err := c.ListNotes(ctx, folder.ID, page, 100)
					if err != nil {
						break
					}
					for i := range resp.Items {
						n := &resp.Items[i]
						if err := writeNote(n, folderPath, folder.Title); err == nil {
							exported++
							folderHadNotes = true
						} else {
							skipped++
						}
					}
					if !resp.HasMore {
						break
					}
					page++
				}
				if folderHadNotes {
					exportedFolderTitles = append(exportedFolderTitles, folderPath)
				}
			}

			return toolSuccess(map[string]any{
				"exported":   exported,
				"skipped":    skipped,
				"output_dir": args.OutputDir,
				"folders":    exportedFolderTitles,
			})
		})

	// --- batch_import_markdown ---
	mcp.AddTool(s, &mcp.Tool{Name: "batch_import_markdown", Description: "Walk a directory for .md files and import each as a Joplin note, optionally preserving directory structure as Joplin folders."},
		func(ctx context.Context, req *mcp.CallToolRequest, args struct {
			InputDir          string   `json:"input_dir"                     jsonschema:"Absolute path to the directory containing .md files"`
			FolderID          string   `json:"folder_id,omitempty"           jsonschema:"Destination folder ID (root of import)"`
			FolderName        string   `json:"folder_name,omitempty"         jsonschema:"Destination folder name (auto-creates if not found)"`
			Recursive         *bool    `json:"recursive,omitempty"           jsonschema:"Walk sub-directories (default true)"`
			PreserveStructure *bool    `json:"preserve_structure,omitempty"  jsonschema:"Create Joplin folders matching directory hierarchy (default true)"`
			TagNames          []string `json:"tag_names,omitempty"           jsonschema:"Tag names to apply to every imported note"`
		}) (*mcp.CallToolResult, any, error) {
			if args.InputDir == "" {
				return toolError("input_dir is required", "")
			}
			if err := validateAbsPath(args.InputDir); err != nil {
				return toolError(err.Error(), "Provide an absolute path for input_dir.")
			}

			// Stat the directory
			info, err := os.Stat(args.InputDir)
			if err != nil {
				return toolError(fmt.Sprintf("cannot access input_dir: %s", err.Error()), "Check that the path exists and is readable.")
			}
			if !info.IsDir() {
				return toolError("input_dir must be a directory", "")
			}

			// Default flags
			recursive := true
			if args.Recursive != nil {
				recursive = *args.Recursive
			}
			preserveStructure := true
			if args.PreserveStructure != nil {
				preserveStructure = *args.PreserveStructure
			}

			// Resolve root folder
			rootFolderID, _, err := resolveFolderID(ctx, c, fc, args.FolderID, args.FolderName, true)
			if err != nil {
				return handleErr(err)
			}

			imported := 0
			skipped := 0
			foldersCreated := 0
			var importErrors []string

			// folderIDCache maps local relative dir → Joplin folder ID, to avoid repeated lookups/creates
			folderIDCache := map[string]string{
				".": rootFolderID,
				"":  rootFolderID,
			}

			// resolveImportFolder returns the Joplin folder ID for a relative directory path
			// (relative to args.InputDir), creating intermediate folders as needed.
			resolveImportFolder := func(relDir string) string {
				if !preserveStructure || relDir == "" || relDir == "." {
					return rootFolderID
				}
				if id, ok := folderIDCache[relDir]; ok {
					return id
				}

				// Build path components
				parts := strings.Split(filepath.ToSlash(relDir), "/")
				currentParentID := rootFolderID
				cumulative := ""
				for _, part := range parts {
					if part == "" || part == "." {
						continue
					}
					if cumulative == "" {
						cumulative = part
					} else {
						cumulative = cumulative + "/" + part
					}
					if id, ok := folderIDCache[cumulative]; ok {
						currentParentID = id
						continue
					}
					// Create or find this folder under currentParentID
					newFolder, err := c.CreateFolder(ctx, part, currentParentID)
					if err != nil {
						// Best effort: use parent
						folderIDCache[cumulative] = currentParentID
					} else {
						foldersCreated++
						fc.Invalidate()
						currentParentID = newFolder.ID
						folderIDCache[cumulative] = newFolder.ID
					}
				}
				return currentParentID
			}

			walkErr := filepath.WalkDir(args.InputDir, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					importErrors = append(importErrors, fmt.Sprintf("walk error at %q: %s", path, err.Error()))
					return nil // continue
				}
				if d.IsDir() {
					// Skip sub-directories if not recursive (but still process the root)
					if !recursive && path != args.InputDir {
						return fs.SkipDir
					}
					return nil
				}
				if !strings.EqualFold(filepath.Ext(path), ".md") {
					skipped++
					return nil
				}

				// Determine relative directory
				rel, _ := filepath.Rel(args.InputDir, path)
				relDir := filepath.Dir(rel)
				if relDir == "." {
					relDir = ""
				}

				// Skip if non-recursive and file is in a sub-directory
				if !recursive && relDir != "" {
					return nil
				}

				// Determine parent folder ID
				parentFolderID := resolveImportFolder(relDir)

				// Read file
				data, err := os.ReadFile(path)
				if err != nil {
					importErrors = append(importErrors, fmt.Sprintf("failed to read %q: %s", path, err.Error()))
					skipped++
					return nil
				}
				body := string(data)
				title := extractMarkdownTitle(body, path)

				note, err := c.CreateNote(ctx, joplin.NoteCreateParams{
					Title:    title,
					Body:     body,
					ParentID: parentFolderID,
				})
				if err != nil {
					importErrors = append(importErrors, fmt.Sprintf("failed to import %q: %s", path, err.Error()))
					return nil
				}

				if len(args.TagNames) > 0 {
					_, tagWarnings := applyTags(ctx, c, note.ID, args.TagNames)
					for _, w := range tagWarnings {
						importErrors = append(importErrors, fmt.Sprintf("tag warning for %q: %s", path, w))
					}
				}

				imported++
				return nil
			})
			if walkErr != nil {
				return toolError(fmt.Sprintf("directory walk failed: %s", walkErr.Error()), "")
			}

			return toolSuccess(map[string]any{
				"imported":        imported,
				"skipped":         skipped,
				"folders_created": foldersCreated,
				"errors":          importErrors,
			})
		})
}

// sanitizeFilename replaces filesystem-unsafe characters with underscores,
// trims leading/trailing whitespace and dots, and caps the name at 200 characters.
func sanitizeFilename(name string) string {
	replacer := strings.NewReplacer(
		"/", "_", "\\", "_", ":", "_", "*", "_",
		"?", "_", "\"", "_", "<", "_", ">", "_", "|", "_",
	)
	name = replacer.Replace(name)
	name = strings.Trim(name, " .")
	if len([]rune(name)) > 200 {
		runes := []rune(name)
		name = string(runes[:200])
	}
	return name
}

// buildFrontmatter generates a YAML frontmatter block for the given note.
func buildFrontmatter(note *joplin.Note, folderPath string, tags []string) string {
	var sb strings.Builder
	sb.WriteString("---\n")
	sb.WriteString(fmt.Sprintf("joplin_id: %s\n", note.ID))
	sb.WriteString(fmt.Sprintf("title: %q\n", note.Title))
	sb.WriteString(fmt.Sprintf("folder: %q\n", folderPath))
	if len(tags) > 0 {
		sb.WriteString("tags:\n")
		for _, t := range tags {
			sb.WriteString(fmt.Sprintf("  - %s\n", t))
		}
	} else {
		sb.WriteString("tags: []\n")
	}
	if note.CreatedTime > 0 {
		sb.WriteString(fmt.Sprintf("created: %s\n", time.UnixMilli(note.CreatedTime).UTC().Format(time.RFC3339)))
	}
	if note.UpdatedTime > 0 {
		sb.WriteString(fmt.Sprintf("updated: %s\n", time.UnixMilli(note.UpdatedTime).UTC().Format(time.RFC3339)))
	}
	sb.WriteString("---\n\n")
	return sb.String()
}

// flattenFolderList recursively flattens a folder tree into a flat slice.
func flattenFolderList(folders []*joplin.Folder) []*joplin.Folder {
	var result []*joplin.Folder
	for _, f := range folders {
		if f == nil {
			continue
		}
		result = append(result, f)
		if len(f.Children) > 0 {
			result = append(result, flattenFolderList(f.Children)...)
		}
	}
	return result
}
