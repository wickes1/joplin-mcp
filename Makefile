.PHONY: help build test clean snapshot

BINARY_NAME=joplin-mcp
BUILD_FLAGS=-ldflags="-s -w" -trimpath

.DEFAULT_GOAL := help

help: ## Show available commands
	@echo "joplin-mcp"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

build: ## Build the binary
	@go build $(BUILD_FLAGS) -o $(BINARY_NAME)
	@echo "Built: ./$(BINARY_NAME)"

test: ## Run tests
	@go test ./...

clean: ## Remove build artifacts
	@rm -f $(BINARY_NAME)
	@rm -rf dist/

snapshot: ## Build a snapshot release (no publish)
	@goreleaser build --snapshot --clean
