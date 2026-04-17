# Contributing to joplin-mcp

## Development Setup

```bash
git clone https://github.com/Wickes1/joplin-mcp
cd joplin-mcp
go build ./...
```

Set your Joplin API token (required for integration tests):

```bash
export JOPLIN_TOKEN=your-token-here
```

Make sure Joplin desktop is running with Web Clipper enabled before running integration tests.

## Code Style

- Format with `gofmt` before committing (`gofmt -w .`)
- Follow existing patterns:
  - New tools go in the appropriate `tools/*.go` file and are registered in that file's `Register*Tools` function
  - Use `toolError()` from `tools/helpers.go` for all error returns
  - Name-based parameters (`folder_name`, `tag_names`) should resolve via the folder/tag cache, not direct API calls
  - Keep tool descriptions concise and LLM-oriented

## Testing

Run unit tests:

```bash
go test ./...
```

Run integration tests (requires a running Joplin instance with `JOPLIN_TOKEN` set):

```bash
JOPLIN_TOKEN=your-token-here go test -tags integration ./...
```

Integration tests create and clean up their own test data in Joplin. Do not run them against a Joplin instance with data you cannot afford to lose.

## Pull Request Process

1. Fork the repository and create a branch from `main`
2. Make your changes, following the code style above
3. Add or update tests for any new behavior
4. Ensure `go build ./...` and `go test ./...` both pass
5. Open a pull request with a clear description of the change and why it is needed
