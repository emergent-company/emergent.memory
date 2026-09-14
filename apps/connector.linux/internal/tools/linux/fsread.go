package linux

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/emergent-company/memory.web-ui/connector/internal/toolreg"
)

// newFSReadTool builds the linux-fs-read tool. Reads are capped at maxBytes and
// only permitted inside a root that grants Read.
func newFSReadTool(roots []Root, maxBytes int64) toolreg.Tool {
	if maxBytes <= 0 {
		maxBytes = defaultMaxBytes
	}
	return toolreg.Tool{
		Name:        ToolFSRead,
		Description: fmt.Sprintf("Read a file inside an allowed root, up to %d bytes. Returns the content, its byte count, and whether it was truncated.", maxBytes),
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "absolute path to a file inside an allowed root",
				},
			},
			"required": []string{"path"},
		},
		Handler: func(_ context.Context, args map[string]any) (map[string]any, error) {
			target, err := requireString(args, "path")
			if err != nil {
				return nil, err
			}
			root, resolved, err := resolveTarget(roots, target)
			if err != nil {
				return nil, err
			}
			if !root.Read {
				return nil, fmt.Errorf("%s: read denied for root %q", ToolFSRead, root.Path)
			}

			f, err := os.Open(resolved)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", ToolFSRead, err)
			}
			defer func() { _ = f.Close() }()

			buf, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
			if err != nil {
				return nil, fmt.Errorf("%s: %w", ToolFSRead, err)
			}
			truncated := int64(len(buf)) > maxBytes
			if truncated {
				buf = buf[:maxBytes]
			}
			return map[string]any{
				"path":      resolved,
				"content":   string(buf),
				"bytes":     len(buf),
				"truncated": truncated,
			}, nil
		},
	}
}
