package linux

import (
	"context"
	"fmt"
	"os"
	"sort"

	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/toolreg"
)

// newFSListTool builds the linux-fs-list tool. Listing requires a root that
// grants Read. Entries are sorted by name.
func newFSListTool(roots []Root) toolreg.Tool {
	return toolreg.Tool{
		Name:        ToolFSList,
		Description: "List the entries of a directory inside an allowed root, sorted by name, with each entry's type and size.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "absolute path to a directory inside an allowed root",
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
				return nil, fmt.Errorf("%s: read denied for root %q", ToolFSList, root.Path)
			}

			entries, err := os.ReadDir(resolved)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", ToolFSList, err)
			}
			sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

			out := make([]map[string]any, 0, len(entries))
			for _, e := range entries {
				var size int64
				if info, infoErr := e.Info(); infoErr == nil {
					size = info.Size()
				}
				out = append(out, map[string]any{
					"name": e.Name(),
					"type": dirEntryType(e),
					"size": size,
				})
			}
			return map[string]any{
				"path":    resolved,
				"entries": out,
				"count":   len(out),
			}, nil
		},
	}
}

// dirEntryType classifies a directory entry without following symlinks.
func dirEntryType(e os.DirEntry) string {
	switch {
	case e.Type()&os.ModeSymlink != 0:
		return "symlink"
	case e.IsDir():
		return "dir"
	case e.Type().IsRegular():
		return "file"
	default:
		return "other"
	}
}
