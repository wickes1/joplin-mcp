# joplin-mcp

A Go-based MCP server that connects AI assistants to Joplin desktop for complete note management.

## Features

27 tools covering notes, folders, tags, search, batch operations, export/import, and resource management — all accessible to any MCP-compatible AI assistant (Claude, etc.).

- **Notes**: create, read, update, delete, batch-read
- **Search**: full-text search with body previews
- **Folders**: list, create, rename, delete (nested tree with computed paths)
- **Tags**: list, apply, remove, delete, query notes by tag
- **Batch**: move notes, tag/untag in bulk, merge notes
- **Export/Import**: export to Markdown files, bulk import from directory
- **Resources**: list, inspect, download, and upload attachments
- **Utility**: connectivity check, import single Markdown file

## Prerequisites

- [Joplin desktop](https://joplinapp.org/) installed and running
- Web Clipper enabled: **Settings → Web Clipper → Enable Web Clipper Service**
- Copy your API token from the Web Clipper settings page

## Installation

```bash
go install github.com/Wickes1/joplin-mcp@latest
```

Or build from source:

```bash
git clone https://github.com/Wickes1/joplin-mcp
cd joplin-mcp
go build -o joplin-mcp .
```

## Configuration

Add to your `.mcp.json` (or Claude Code's MCP config):

```json
{
  "mcpServers": {
    "joplin": {
      "command": "joplin-mcp",
      "env": {
        "JOPLIN_TOKEN": "your-token-here",
        "JOPLIN_HOST": "localhost",
        "JOPLIN_PORT": "41184"
      }
    }
  }
}
```

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `JOPLIN_TOKEN` | (required) | Joplin Web Clipper API token |
| `JOPLIN_HOST` | `localhost` | Joplin API host |
| `JOPLIN_PORT` | `41184` | Joplin API port |
| `JOPLIN_LOG_LEVEL` | `info` | Log level: `debug`, `info`, `warn`, `error` |

> **Security**: Do not commit `JOPLIN_TOKEN` to version control. On macOS, store it in Keychain and inject it via a wrapper script.

## Tools Reference

### Notes (6 tools)

| Tool | Description |
|------|-------------|
| `list_notes` | List notes, optionally filtered by folder. Returns slim notes and a `has_more` flag. |
| `get_note` | Get a single note by ID with full body and tags. |
| `get_notes` | Batch-read up to 50 notes by ID. Failed lookups are included as error entries. |
| `create_note` | Create a new note. Optionally auto-create folder by name and apply tags. |
| `update_note` | Update an existing note's title, body, folder, or to-do status. `folder_name` auto-creates if not found. |
| `delete_note` | Delete a note by ID. By default moves to trash; set `permanent=true` to bypass trash. |

### Search (1 tool)

| Tool | Description |
|------|-------------|
| `search_notes` | Full-text search across all notes. Returns preview notes (200-char body preview) and a `has_more` flag. |

### Folders (4 tools)

| Tool | Description |
|------|-------------|
| `list_folders` | List all folders as a nested tree with computed paths (e.g. `Work/Projects/Q1`). |
| `create_folder` | Create a new folder, optionally nested under a parent folder. |
| `update_folder` | Rename or move a folder. |
| `delete_folder` | Delete a folder by ID. By default moves to trash; set `permanent=true` to bypass trash. |

### Tags (5 tools)

| Tool | Description |
|------|-------------|
| `list_tags` | List all tags in the Joplin library. |
| `tag_note` | Apply a tag to a note. Creates the tag if it does not exist (case-insensitive). |
| `untag_note` | Remove a tag from a note (case-insensitive tag lookup). |
| `delete_tag` | Delete a tag by ID. Removes it from all notes. |
| `get_notes_by_tag` | Get notes associated with a tag name (case-insensitive). Returns slim notes and a `has_more` flag. |

### Batch (3 tools)

| Tool | Description |
|------|-------------|
| `batch_move_notes` | Move up to 100 notes to a destination folder concurrently. |
| `batch_tag_notes` | Add or remove a tag from up to 100 notes concurrently. Creates the tag automatically when adding. |
| `merge_notes` | Fetch up to 50 notes, concatenate their bodies with headings and separators, create a new merged note. |

### Export / Import (2 tools)

| Tool | Description |
|------|-------------|
| `export_notes` | Export Joplin notes to the filesystem as Markdown files, preserving folder hierarchy with optional YAML frontmatter. |
| `batch_import_markdown` | Walk a directory for `.md` files and import each as a Joplin note, optionally preserving directory structure as folders. |

### Resources (4 tools)

| Tool | Description |
|------|-------------|
| `list_resources` | List Joplin resources (attachments). Returns a page of resources and a `has_more` flag. |
| `get_resource` | Get metadata for a single Joplin resource (attachment) by ID. |
| `download_resource` | Download a Joplin resource (attachment) file to a local path on disk. |
| `upload_resource` | Upload a local file as a Joplin resource. Returns an ID for embedding: `![alt](:/id)` or `[name](:/id)`. |

### Utility (2 tools)

| Tool | Description |
|------|-------------|
| `import_markdown` | Import a single Markdown (`.md`) file into Joplin as a note. Title extracted from the first `#` heading or filename. |
| `get_status` | Check Joplin connectivity and return library statistics (folder count, tag count, port). |

## Response Design

Tools follow a **slim / full / preview** pattern to keep token usage low:

- **Slim** (list operations): `id`, `title`, `parent_id`, `folder_title`, `is_todo`, `updated_time`. Used for browsing without fetching note bodies.
- **Full** (get / create / update): All slim fields plus `body`, `created_time`, `todo_completed`, and `tags[]`. Used when content is needed.
- **Preview** (search): Slim fields plus the first 200 characters of the body. Usually enough to judge relevance without a follow-up `get_note` call.

Write operations (`create_note`, `update_note`, etc.) return slim responses; read operations (`get_note`, `get_notes`) return full responses.

## License

MIT
