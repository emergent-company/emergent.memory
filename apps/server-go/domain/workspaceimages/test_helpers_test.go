package workspaceimages

import (
	"log/slog"
	"os"
)

// testLogger returns a logger suitable for testing.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}
