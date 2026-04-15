package main

import (
	"context"
	"log/slog"
	"os"
	"strconv"

	"github.com/Wickes1/joplin-mcp/joplin"
	"github.com/Wickes1/joplin-mcp/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	// --- Configuration from environment ---
	token := os.Getenv("JOPLIN_TOKEN")
	if token == "" {
		slog.Error("JOPLIN_TOKEN environment variable is required")
		os.Exit(1)
	}

	host := os.Getenv("JOPLIN_HOST")
	if host == "" {
		host = "localhost"
	}

	port := 41184
	if portStr := os.Getenv("JOPLIN_PORT"); portStr != "" {
		if p, err := strconv.Atoi(portStr); err == nil {
			port = p
		}
	}

	logLevel := os.Getenv("JOPLIN_LOG_LEVEL")
	if logLevel == "" {
		logLevel = "info"
	}

	// --- Logging setup (stderr only; stdout is reserved for MCP JSON-RPC) ---
	var level slog.Level
	switch logLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	// Redact token in logs — show only first 4 chars
	tokenHint := token
	if len(tokenHint) > 4 {
		tokenHint = tokenHint[:4] + "..."
	}
	slog.Info("starting joplin-mcp",
		"host", host,
		"port", port,
		"token_prefix", tokenHint,
		"log_level", logLevel,
	)

	// --- Joplin client and folder cache ---
	client := joplin.NewClient(token, host, port)
	folderCache := tools.NewFolderCache(client)

	// --- MCP Server ---
	server := mcp.NewServer(&mcp.Implementation{Name: "joplin-mcp", Version: "0.1.0"}, nil)

	// TODO: Register tools (Phase 2)
	// tools.RegisterNoteTools(server, client, folderCache)
	// tools.RegisterFolderTools(server, client, folderCache)
	// tools.RegisterTagTools(server, client, folderCache)
	// tools.RegisterSearchTools(server, client, folderCache)
	// tools.RegisterUtilityTools(server, client, folderCache)

	// Suppress "declared but not used" for folderCache until tools are registered.
	_ = folderCache

	slog.Info("joplin-mcp server ready")

	// --- Run on stdio transport ---
	ctx := context.Background()
	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
		slog.Error("server exited with error", "err", err)
		os.Exit(1)
	}
}
