# Joplin MCP Server — Design Document

**Date**: 2026-04-14
**Status**: Approved (post-review v3)
**Language**: Go 1.26+
**SDK**: `modelcontextprotocol/go-sdk` v1.5+
**License**: MIT

---

## 1. Overview

A Go-based MCP server that connects to a local Joplin desktop application via its Web Clipper REST API (localhost:41184). Provides 18 tools for full note, folder, tag, and search operations.

**Goals:**
- Single binary deployment, zero runtime dependencies
- Complete coverage of Joplin's note management capabilities
- LLM-optimized tool naming and response design
- Slim responses to minimize token usage

**Non-goals:**
- Replacing Joplin desktop (sync, encryption, storage are Joplin's responsibility)
- MCP Resources or Prompts (not well-supported in stdio mode)
- Resource/attachment management (Joplin `:/resource-id` references are opaque to this server)
- GUI or web interface

## 2. Architecture

```
┌─────────────────┐     stdio (JSON-RPC)     ┌──────────────┐
│   Claude Code   │ ◄─────────────────────►   │  joplin-mcp  │
│   (MCP Client)  │                           │  (Go binary) │
└─────────────────┘                           └──────┬───────┘
                                                     │ HTTP REST
                                                     ▼
                                              ┌──────────────┐
                                              │ Joplin Desktop│
                                              │ :41184       │
                                              └──────────────┘
```

### Transport
- **MCP transport**: stdio (JSON-RPC 2.0)
- **Joplin connection**: HTTP GET/POST/PUT/DELETE to `localhost:41184`
- **Logging**: All logs written to stderr (stdout reserved for MCP JSON-RPC)

### Authentication
- Joplin API token passed via `JOPLIN_TOKEN` environment variable
- Token appended as `?token=XXX` query parameter on all Joplin API requests

> **Security warning**: Do NOT commit `JOPLIN_TOKEN` to version control or dotfile repos.
> On macOS, store in Keychain and reference via wrapper script:
> ```bash
> #!/bin/sh
> export JOPLIN_TOKEN=$(security find-generic-password -a joplin -s joplin-mcp -w)
> exec /path/to/joplin-mcp
> ```
> Ensure config file permissions are `600` if storing the token directly.

### Configuration
| Source | Variable | Default | Description |
|--------|----------|---------|-------------|
| Env | `JOPLIN_TOKEN` | (required) | Joplin Web Clipper API token |
| Env | `JOPLIN_PORT` | `41184` | Joplin API port |
| Env | `JOPLIN_HOST` | `localhost` | Joplin API host |
| Env | `JOPLIN_LOG_LEVEL` | `info` | Log level: debug, info, warn, error |

## 3. Project Structure

```
joplin-mcp/
├── main.go              # Entry point, MCP server init, calls tools.Register*()
├── joplin/
│   ├── client.go        # HTTP client for Joplin REST API
│   ├── types.go         # Note, Folder, Tag structs + Joplin int-bool conversion
│   └── errors.go        # Error types with agent-friendly messages
├── tools/
│   ├── notes.go         # RegisterNoteTools(server, client)
│   ├── search.go        # RegisterSearchTools(server, client)
│   ├── folders.go       # RegisterFolderTools(server, client)
│   ├── tags.go          # RegisterTagTools(server, client)
│   ├── utility.go       # RegisterUtilityTools(server, client)
│   └── helpers.go       # Shared: toolError(), folder cache, name resolution
├── go.mod
├── go.sum
├── docs/
│   └── design.md        # This file
└── README.md
```

### Dependency Injection Pattern
Each `tools/*.go` file exports a `Register*Tools(s *mcp.Server, c *joplin.Client)` function. `main.go` creates the server and client, then calls each Register function. Tools never import `main`; `main` imports `tools` and `joplin`. No circular dependencies.

```go
// main.go
server := mcp.NewServer(...)
client := joplin.NewClient(token, host, port)
tools.RegisterNoteTools(server, client)
tools.RegisterFolderTools(server, client)
tools.RegisterTagTools(server, client)
tools.RegisterSearchTools(server, client)
tools.RegisterUtilityTools(server, client)
server.Run(ctx, &mcp.StdioTransport{})
```

### Internal DRY Principles
- `joplin/client.go`: Single HTTP client handles all API calls, pagination, error wrapping
- `joplin/types.go`: Shared structs with Joplin int↔bool conversion (see Section 6)
- `joplin/errors.go`: All errors include agent-guidance messages
- `tools/helpers.go`: Shared folder cache (session-level, 30s TTL), `toolError()` helper

## 4. MCP Tools (18 total)

### 4.1 Notes (8 tools)

#### `list_notes`
> *List notes by folder. Use search_notes for content-based queries; this tool is for browsing by folder structure.*

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `folder_id` | string | no | — | Filter by folder ID |
| `limit` | int | no | 20 | Max results per page (max 100) |
| `page` | int | no | 1 | Page number (1-indexed) |

**Pagination**: Pass-through to Joplin API. LLM controls pagination via `page` param. Response includes `has_more` to signal more pages.

**Response** (slim):
```json
{
  "notes": [
    {
      "id": "abc123",
      "title": "My Note",
      "parent_id": "folder1",
      "folder_title": "Work",
      "is_todo": false,
      "updated_time": "2026-04-14T10:30:00Z"
    }
  ],
  "has_more": true
}
```

#### `get_note`
> *Get a single note with full body content, tags, and metadata.*

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `note_id` | string | yes | Note ID |

**Response** (full):
```json
{
  "id": "abc123",
  "title": "My Note",
  "body": "Full markdown content...",
  "parent_id": "folder1",
  "folder_title": "Work",
  "is_todo": false,
  "todo_completed": null,
  "created_time": "2026-04-10T08:00:00Z",
  "updated_time": "2026-04-14T10:30:00Z",
  "tags": ["project", "urgent"]
}
```

Note: `todo_completed` is `null` when not completed, or an ISO 8601 timestamp when completed (Joplin stores this as Unix ms, 0 = not completed).

#### `get_notes`
> *Batch read multiple notes by IDs. Use when you need full content of several notes at once.*

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `note_ids` | string[] | yes | List of note IDs (max 50) |

**Response**: Array of full note objects (same as `get_note`). Failed lookups included as `{ "id": "xxx", "error": "not found" }`.

**Implementation**: Concurrent HTTP calls with `errgroup`, capped at 5 parallel requests (Joplin's SQLite backend is single-writer). Total operation timeout: 30 seconds via `context.Context` deadline, independent of per-request HTTP timeout.

#### `create_note`
> *Create a new note. Supports folder by name (auto-created if not found) and tags by name (auto-created if not exist).*

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `title` | string | yes | — | Note title |
| `body` | string | no | "" | Markdown content |
| `folder_id` | string | no | — | Target folder ID (mutually exclusive with `folder_name`) |
| `folder_name` | string | no | — | Target folder name (looked up, created if not found) |
| `is_todo` | bool | no | false | Create as todo item |
| `tag_names` | string[] | no | — | Tags to apply (created if not exist) |

**Response**: Full created note object. If some tags failed to apply, response includes `"tag_warnings": ["Failed to apply tag 'x': reason"]`.

**Implementation**:
1. If `folder_name` provided: search folders by title, use first match or create new
2. Create note via `POST /notes`
3. If `tag_names` provided: for each tag, find or create, then `POST /tags/:id/notes`
4. Tag application is best-effort. Verify applied tags and surface warnings for any failures.

#### `update_note`
> *Update an existing note. Only provided fields are changed. Use folder_name to move by name instead of ID. Unlike create_note, folder_name does NOT auto-create — use create_folder first if needed.*

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `note_id` | string | yes | Note ID |
| `title` | string | no | New title |
| `body` | string | no | New body (full replacement) |
| `folder_id` | string | no | Move to folder by ID |
| `folder_name` | string | no | Move to folder by name (looked up, not auto-created) |
| `is_todo` | bool | no | Change todo status |

**Response**: Updated note object.

#### `append_to_note`
> *Append content to the end of a note. Useful for adding entries without replacing existing content.*

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `note_id` | string | yes | Note ID |
| `content` | string | yes | Content to append (prepended with newline) |

**Response**: Updated note object.

**Implementation**: `GET /notes/:id?fields=body` → append content → `PUT /notes/:id`.

> **Known limitation**: This is a read-modify-write operation. Joplin's API has no optimistic locking. If the note is edited concurrently (e.g., in Joplin desktop), one edit may be lost. For critical appends, verify the result with `get_note` afterward.

#### `delete_note`
> *Delete a note. Moves to trash by default; use permanent=true to delete permanently.*

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `note_id` | string | yes | — | Note ID |
| `permanent` | bool | no | false | Permanently delete instead of trash |

**Response**: `{ "status": "deleted", "note_id": "abc123" }`

**Implementation**: `DELETE /notes/:id` (trash) or `DELETE /notes/:id?permanent=1` (permanent). Note: Joplin API uses integer `1` for the permanent query param, not boolean `true`.

#### `search_notes`
> *Full-text search across all notes using Joplin's built-in search. Returns titles with body previews to help determine relevance.*

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `query` | string | yes | — | Search query (supports Joplin search syntax) |
| `limit` | int | no | 20 | Max results (max 50) |

**Pagination**: Single-page results capped by `limit`. For large result sets, refine the query.

**Response** (slim + preview):
```json
{
  "notes": [
    {
      "id": "abc123",
      "title": "My Note",
      "parent_id": "folder1",
      "folder_title": "Work",
      "is_todo": false,
      "updated_time": "2026-04-14T10:30:00Z",
      "preview": "First 200 characters of body content..."
    }
  ],
  "has_more": false
}
```

**Implementation**:
1. `GET /search?query=XXX&limit=N&fields=id,title,parent_id,is_todo,updated_time,body` — attempt to request body in fields to avoid N+1 queries
2. **Fallback**: If Joplin's `/search` endpoint does not return `body` in the response (field filtering may not be supported on search in all Joplin versions), fall back to N concurrent `GET /notes/:id?fields=body` requests (capped at 5 concurrent, same as `get_notes`)
3. Truncate body to 200 chars in Go for preview
4. Resolve `folder_title` from cached folder map

### 4.2 Folders (3 tools)

#### `list_folders`
> *List all notebooks as a hierarchical tree with full paths for easy identification.*

No parameters.

**Response**:
```json
{
  "folders": [
    {
      "id": "folder1",
      "title": "Work",
      "parent_id": "",
      "path": "Work",
      "children": [
        {
          "id": "folder2",
          "title": "Projects",
          "parent_id": "folder1",
          "path": "Work/Projects",
          "children": []
        }
      ]
    }
  ]
}
```

**Implementation**: `GET /folders` returns a pre-nested tree with `children` key. Compute `path` field by traversing parent chain. For large folder collections (500+), this requires full pagination of the API response; performance may degrade linearly.

#### `create_folder`
> *Create a new notebook. Optionally nest under a parent folder.*

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `title` | string | yes | Folder name |
| `parent_id` | string | no | Parent folder ID for nesting |

**Response**: Created folder object with `path`.

#### `delete_folder`
> *Delete a notebook. Moves to trash by default; use permanent=true to delete permanently. Notes inside may be moved or trashed with the folder — verify after deletion.*

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `folder_id` | string | yes | — | Folder ID |
| `permanent` | bool | no | false | Permanently delete instead of trash |

**Response**: `{ "status": "deleted", "folder_id": "folder1" }`

**Implementation**: `DELETE /folders/:id` (trash) or `DELETE /folders/:id?permanent=1` (permanent).

> **Caution**: Joplin's behavior for notes inside a deleted folder is not explicitly documented. The notes may be moved to trash along with the folder. Verify note safety after folder deletion.

### 4.3 Tags (5 tools)

#### `list_tags`
> *List all tags.*

No parameters.

**Response**:
```json
{
  "tags": [
    { "id": "tag1", "title": "project" },
    { "id": "tag2", "title": "urgent" }
  ]
}
```

#### `tag_note`
> *Add a tag to a note by tag name. Creates the tag automatically if it doesn't exist.*

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `tag_name` | string | yes | Tag name (case-insensitive lookup) |
| `note_id` | string | yes | Note ID |

**Response**: `{ "status": "tagged", "tag_id": "tag1", "tag_name": "project", "note_id": "abc123" }`

**Implementation**:
1. Search existing tags for name match (case-insensitive)
2. If not found, `POST /tags` to create
3. `POST /tags/:id/notes` to associate

#### `untag_note`
> *Remove a tag from a note by tag name.*

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `tag_name` | string | yes | Tag name (case-insensitive lookup) |
| `note_id` | string | yes | Note ID |

**Response**: `{ "status": "untagged", "tag_name": "project", "note_id": "abc123" }`

**Implementation**:
1. Search tags by name → get tag ID
2. `DELETE /tags/:id/notes/:note_id`

#### `delete_tag`
> *Delete a tag entirely. Removes it from all notes.*

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `tag_id` | string | yes | Tag ID |

**Response**: `{ "status": "deleted", "tag_id": "tag1" }`

#### `get_notes_by_tag`
> *List all notes that have a specific tag.*

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `tag_name` | string | yes | — | Tag name (case-insensitive) |
| `limit` | int | no | 20 | Max results per page |
| `page` | int | no | 1 | Page number (1-indexed) |

**Response**: Same slim format as `list_notes` (includes `has_more`).

**Implementation**:
1. Search tags by name → get tag ID
2. `GET /tags/:id/notes` with `page` and `limit` pass-through

### 4.4 Utility (2 tools)

#### `import_markdown`
> *Import a local markdown file as a new note. Only .md files are accepted. Folder name auto-creates if not found (same as create_note).*

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `file_path` | string | yes | Absolute path to .md file |
| `folder_id` | string | no | Target folder ID |
| `folder_name` | string | no | Target folder name (auto-created if not found) |
| `tag_names` | string[] | no | Tags to apply (created if not exist) |

**Response**: Created note object.

**Security**: Only files with `.md` extension are accepted. Path traversal is not a concern since this runs locally, but the extension check prevents accidental import of non-markdown files.

**Implementation**:
1. Validate file extension is `.md`
2. Read file from disk
3. Extract title from first `# ` heading, fallback to filename without extension
4. Create note via `create_note` logic (inherits folder and tag handling)

#### `get_status`
> *Check connection to Joplin and get a quick overview of your library.*

No parameters.

**Response**:
```json
{
  "status": "connected",
  "joplin_port": 41184,
  "folder_count": 15,
  "tag_count": 28
}
```

**Implementation**: `GET /ping` to verify connection. Folder and tag counts from `GET /folders` and `GET /tags` (typically small collections). Note count is omitted as it requires full pagination for accuracy. For libraries with 500+ folders or tags, this call may be slow.

## 5. Error Handling

All errors return structured JSON with `error` and `hint` fields via `CallToolResult` with `IsError: true`. The `hint` guides the LLM on what to do next.

### Error Response Format
```json
{
  "error": "Note def456 not found.",
  "hint": "Use search_notes or list_notes to find valid note IDs."
}
```

### Error Categories

| Scenario | Error Message | Hint |
|----------|---------------|------|
| Note not found | `Note {id} not found.` | `Use search_notes or list_notes to find valid note IDs.` |
| Folder not found (by ID) | `Folder {id} not found.` | `Use list_folders to see available folders.` |
| Folder not found (by name, in update_note) | `Folder "{name}" not found.` | `Use create_folder to create it first, then retry update_note.` |
| Tag not found | `Tag "{name}" not found.` | `Use list_tags to see available tags.` |
| Joplin not running | `Cannot connect to Joplin at {host}:{port}.` | `Ensure Joplin desktop is running and Web Clipper is enabled in Preferences.` |
| Invalid token | `Joplin API returned 403 Forbidden.` | `Check that JOPLIN_TOKEN is set correctly. Find it in Joplin > Preferences > Web Clipper.` |
| Missing required param | `Parameter "{name}" is required.` | `{description of what the parameter should be}` |
| Invalid file extension | `Only .md files can be imported, got "{ext}".` | `Provide a path to a Markdown (.md) file.` |

## 6. Response Design Principles

### Slim vs Full Responses
- **Slim** (list/search operations): `id`, `title`, `parent_id`, `folder_title`, `is_todo`, `updated_time`
  - Minimizes token usage for browsing
  - Includes `folder_title` so LLM understands context without extra API calls
  - `folder_title` resolved from a session-level folder cache (30s TTL, shared across tool invocations)
- **Full** (get/create/update operations): All slim fields + `body`, `created_time`, `todo_completed`, `tags[]`
  - Used when the LLM needs the actual content
- **Preview** (search only): Slim + first 200 chars of body
  - Often sufficient to determine relevance without `get_note`

### Joplin API Type Mapping
Joplin's REST API has non-standard types that must be converted:

| Joplin API | Go type | MCP response | Notes |
|------------|---------|-------------|-------|
| `is_todo`: `0` or `1` | `int` internally | `bool` (`true`/`false`) | Convert on serialize |
| `todo_completed`: `0` or Unix ms | `int64` internally | `null` or ISO 8601 string | 0 → `null`, >0 → timestamp |
| `created_time`: Unix ms | `int64` internally | ISO 8601 string | 0 → omit field |
| `updated_time`: Unix ms | `int64` internally | ISO 8601 string | 0 → omit field |
| `permanent`: query param | — | `?permanent=1` | Only append when `true`; never send `?permanent=0` |

### Timestamps
- All timestamps converted from Joplin's Unix milliseconds to ISO 8601 strings
- Zero-value timestamps (0 ms) are omitted from the response rather than returning `1970-01-01`

### Name-Based Parameters
- `folder_name` and `tag_names`/`tag_name` accepted alongside ID-based parameters
- Reduces LLM round-trips (no need to list → find ID → use ID)
- ID-based params take precedence when both provided
- Applied consistently: `create_note`, `update_note`, `tag_note`, `untag_note`, `get_notes_by_tag`, `import_markdown`

### Pagination Strategy
- **LLM-controlled pagination**: `list_notes`, `get_notes_by_tag` expose `page`/`limit` params, pass through to Joplin API, return `has_more`
- **Internal pagination**: `list_folders`, `list_tags` fetch all pages internally (typically small collections)
- **Capped results**: `search_notes` returns up to `limit` results in one call (no multi-page)
- No auto-pagination exposed to LLM — the LLM decides whether to fetch more pages

### Folder Cache
- **Scope**: Session-level (persists across tool invocations within the same MCP server process)
- **TTL**: 30 seconds. After expiry, next access re-fetches from Joplin API.
- **Purpose**: Resolve `folder_title` in slim responses and `folder_name` in name-based parameters without re-fetching the full folder list on every tool call
- **Invalidation**: Cache is also invalidated when `create_folder` or `delete_folder` is called

## 7. Implementation Notes

### HTTP Client (`joplin/client.go`)
- Single shared `http.Client` with 10-second per-request timeout
- All methods accept `context.Context` for cancellation
- Automatic pagination helper for internal use (folders, tags)
- Token injected into every request as query parameter
- Base URL constructed from `JOPLIN_HOST` and `JOPLIN_PORT`
- Simple retry: 1 retry with 500ms delay for 5xx responses only (Joplin local API, low risk of retry amplification)

### Concurrency
- `get_notes` (batch): Uses `errgroup` with max 5 concurrent goroutines (Joplin's SQLite is single-writer; higher concurrency causes lock contention)
- `search_notes` preview fallback: If field-based body fetch fails, falls back to concurrent note fetches (same 5-goroutine cap)
- Total operation deadline for `get_notes`: 30 seconds via `context.WithTimeout`
- All other tools: Sequential (single Joplin API call or small chain)

### Logging
- Output: stderr only (stdout reserved for MCP)
- Library: stdlib `log/slog`
- Level: controlled by `JOPLIN_LOG_LEVEL` env var
- Format: structured text `[LEVEL] timestamp message key=value`
- Logged events: startup config (token redacted), Joplin API errors, tool invocations at debug level
- Token value never logged; only first 4 chars shown as `"abcd..."`

### Graceful Shutdown
- Server exits when stdin is closed (MCP client disconnection)
- `go-sdk`'s `StdioTransport` detects stdin EOF and causes `server.Run()` to return
- No persistent state to clean up (stateless HTTP client)
- In-flight HTTP requests cancelled via context propagation from `server.Run(ctx, ...)`

### Build & Distribution
```bash
# Build
go build -o joplin-mcp .

# Cross-compile
GOOS=linux GOARCH=amd64 go build -o joplin-mcp-linux .
GOOS=darwin GOARCH=arm64 go build -o joplin-mcp-darwin .
```

### Claude Code Configuration
```json
{
  "mcpServers": {
    "joplin": {
      "command": "/path/to/joplin-mcp",
      "env": {
        "JOPLIN_TOKEN": "your-token-here"
      }
    }
  }
}
```

## 8. Dependencies

| Package | Purpose |
|---------|---------|
| `github.com/modelcontextprotocol/go-sdk/mcp` | MCP protocol types, server framework, stdio transport (all in one package) |
| `golang.org/x/sync/errgroup` | Bounded concurrent goroutines for batch operations |
| stdlib `net/http` | Joplin HTTP client |
| stdlib `encoding/json` | JSON marshaling |
| stdlib `os` | Environment variables, file reading |
| stdlib `log/slog` | Structured logging |

Two external dependencies: official MCP SDK + errgroup.

## 9. Known Limitations

| Limitation | Cause | Mitigation |
|------------|-------|------------|
| `append_to_note` race condition | Joplin API has no optimistic locking (no ETags) | Documented in tool description; verify with `get_note` for critical appends |
| `create_note` tag application is best-effort | Multi-step operation (create note + apply tags) can partially fail | Warnings surfaced in response `tag_warnings` field |
| No resource/attachment tools | Out of scope for v1 | Notes with `![](:/resource-id)` references are returned as-is |
| `get_status` omits note count | Counting all notes requires full pagination, expensive for large libraries | Use `list_notes` with pagination to browse |
| `search_notes` returns one page only | Joplin search API has limited pagination support | Refine query or reduce limit for better results |
| `search_notes` preview may require fallback | Joplin `/search` field filtering for `body` not guaranteed in all versions | Falls back to concurrent per-note fetches if needed |
| `delete_folder` note safety unverified | Joplin docs don't specify what happens to notes in deleted folders | Documented as caution; user should verify notes after deletion |
| Large folder/tag libraries may be slow | `list_folders`, `list_tags`, `get_status` fetch all pages internally | Acceptable for typical Joplin usage; degrades linearly for 500+ items |
