# Batch & Export Features — Design Document

**Date**: 2026-04-15
**Status**: Draft
**Depends on**: design.md (v3)

---

## Motivation

The current 18 tools cover single-note CRUD well, but real-world AI-assisted workflows often need to operate on many notes at once:

- "Help me reorganize my entire Joplin library" → needs batch move + merge
- "Export all my notes as a backup" → needs export
- "Import my markdown folder into Joplin" → needs batch import
- "Tag all notes in this folder as 'archived'" → needs batch tag

These 5 new tools upgrade the MCP from "basic CRUD" to "AI note management assistant."

---

## New Tools

### 1. `export_notes`

Export notes to local markdown files, preserving folder structure.

**Parameters:**

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `output_dir` | string | yes | Base directory for exported files |
| `folder_id` | string | no | Export specific folder only (default: all) |
| `include_metadata` | bool | no | Include YAML frontmatter (default: false) |
| `flatten` | bool | no | Flat structure, no sub-dirs (default: false) |

**Behavior:**
1. List all folders (or target folder) via `fc.AllFolders()`
2. Paginate through notes in each folder
3. Batch fetch note bodies via `c.GetNote()` (errgroup, limit 5)
4. Create local directory structure matching Joplin folder paths
5. Write each note as `{folder_path}/{sanitized_title}.md`
6. If `include_metadata`: prepend YAML frontmatter with id, tags, created_time, updated_time

**Frontmatter format:**
```yaml
---
joplin_id: abc123def456
title: My Note Title
folder: Work/Folotech
tags: [project, game]
created: 2025-01-15T10:30:00Z
updated: 2026-04-14T16:40:27Z
---
```

**Response:**
```json
{
  "exported": 42,
  "skipped": 0,
  "output_dir": "/path/to/export",
  "folders": ["Work", "Work/Folotech", "Blogs"]
}
```

**File naming:**
- Sanitize title: replace `/\:*?"<>|` with `_`, trim whitespace
- Collision handling: append `_2`, `_3` if duplicate titles in same folder

**Estimated effort:** 1-2 hours

---

### 2. `batch_move_notes`

Move multiple notes to a target folder in one operation.

**Parameters:**

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `note_ids` | string[] | yes | Note IDs to move (max 100) |
| `folder_id` | string | one of | Target folder ID |
| `folder_name` | string | one of | Target folder name (resolved via cache) |

**Behavior:**
1. Resolve folder_name → folder_id if needed
2. Validate all note_ids exist (batch fetch headers)
3. Update each note's parent_id concurrently (errgroup, limit 5)
4. Return per-note success/error

**Response:**
```json
{
  "moved": 15,
  "failed": 1,
  "errors": [
    {"note_id": "xxx", "error": "not found"}
  ]
}
```

**Estimated effort:** 1 hour

---

### 3. `merge_notes`

Merge multiple notes into a single new note, preserving content.

**Parameters:**

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `note_ids` | string[] | yes | Notes to merge, in order (max 50) |
| `title` | string | yes | Title for merged note |
| `folder_id` | string | no | Destination folder (default: first note's folder) |
| `folder_name` | string | no | Destination folder by name |
| `separator` | string | no | Content between notes (default: `\n\n---\n\n`) |
| `include_titles` | bool | no | Prepend each note's title as heading (default: true) |
| `delete_originals` | bool | no | Move originals to trash after merge (default: false) |

**Behavior:**
1. Batch fetch all notes via `get_notes` pattern
2. Concatenate bodies in order, with separator
3. If `include_titles`: prepend `## {note_title}\n\n` before each body
4. Create new note with merged body
5. If `delete_originals`: delete source notes (trash, not permanent)

**Response:**
```json
{
  "merged_note_id": "new123",
  "source_count": 5,
  "body_length": 12450,
  "originals_deleted": false
}
```

**Estimated effort:** 1-2 hours

---

### 4. `batch_tag_notes`

Apply or remove a tag across multiple notes.

**Parameters:**

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `note_ids` | string[] | yes | Notes to tag (max 100) |
| `tag_name` | string | yes | Tag to apply/remove |
| `action` | string | no | `add` (default) or `remove` |

**Behavior:**
1. Resolve tag_name → tag_id (create if `add` and not exists)
2. For each note, POST or DELETE tag association concurrently (errgroup, limit 5)
3. Return per-note results

**Response:**
```json
{
  "tagged": 20,
  "failed": 0,
  "action": "add",
  "tag_name": "archived"
}
```

**Estimated effort:** 1 hour

---

### 5. `batch_import_markdown`

Import all markdown files from a directory into Joplin.

**Parameters:**

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `input_dir` | string | yes | Directory containing .md files |
| `folder_id` | string | no | Target folder (default: create matching structure) |
| `folder_name` | string | no | Target folder by name |
| `recursive` | bool | no | Recurse into sub-directories (default: true) |
| `tag_names` | string[] | no | Tags to apply to all imported notes |
| `preserve_structure` | bool | no | Create Joplin folders matching dir structure (default: true) |

**Behavior:**
1. Walk `input_dir` for `*.md` files
2. If `preserve_structure`: create Joplin folders matching directory hierarchy
3. For each file: read content, extract title from `# heading` or filename
4. Create notes concurrently (errgroup, limit 5)
5. Apply tags if specified

**Response:**
```json
{
  "imported": 15,
  "skipped": 2,
  "folders_created": 3,
  "errors": []
}
```

**Estimated effort:** 1-2 hours

---

## Implementation Notes

### Concurrency
All batch operations reuse the existing `errgroup` pattern from `get_notes`:
- Max 5 concurrent goroutines (Joplin SQLite is single-writer)
- 30-second total deadline via context
- Per-note error collection, partial success is OK

### File Registration
```go
// tools/batch.go — new file
func RegisterBatchTools(s *mcp.Server, c *joplin.Client, fc *FolderCache) {
    // batch_move_notes
    // batch_tag_notes
    // merge_notes
}

// tools/export.go — new file
func RegisterExportTools(s *mcp.Server, c *joplin.Client, fc *FolderCache) {
    // export_notes
    // batch_import_markdown
}
```

Update `main.go`:
```go
tools.RegisterBatchTools(server, client, folderCache)
tools.RegisterExportTools(server, client, folderCache)
```

### Error Handling
Follow existing pattern: return `AgentError` with hint for each failed note.
Batch results always include per-item status so the LLM knows what succeeded.

### Limits
| Tool | Max items | Reason |
|------|-----------|--------|
| `batch_move_notes` | 100 | Prevent accidental mass moves |
| `merge_notes` | 50 | Merged body could be very large |
| `batch_tag_notes` | 100 | Same as move |
| `batch_import_markdown` | 200 files | Filesystem traversal safety |
| `export_notes` | unlimited | Export is read-only, safe |

---

## Priority

| # | Tool | Value | Effort |
|---|------|-------|--------|
| 1 | `export_notes` | High — backup, migration, Claude Code integration | 1-2h |
| 2 | `merge_notes` | High — reorganization, archiving | 1-2h |
| 3 | `batch_move_notes` | Medium — bulk reorganization | 1h |
| 4 | `batch_tag_notes` | Medium — bulk categorization | 1h |
| 5 | `batch_import_markdown` | Low — reverse of export, less common | 1-2h |

Total estimated effort: 5-8 hours
