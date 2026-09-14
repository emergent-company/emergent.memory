package linux

import (
	"context"
	"fmt"

	"github.com/emergent-company/memory.web-ui/connector/internal/toolreg"
)

// newFSDeleteTool builds the linux-fs-delete tool. Deletes require a root that
// grants Delete and move the target to the XDG trash. When trashing fails and
// allowPermanent is set, the target is permanently removed instead; otherwise
// the target is left intact and an error is returned.
func newFSDeleteTool(roots []Root, allowPermanent bool, audit *auditLogger) toolreg.Tool {
	return toolreg.Tool{
		Name:        ToolFSDelete,
		Description: "Move a file or directory inside an allowed root to the XDG trash. Only permanently deletes when the root config allows it and trashing is unavailable.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "absolute path to a file or directory inside an allowed root",
				},
			},
			"required": []string{"path"},
		},
		Handler: func(_ context.Context, args map[string]any) (map[string]any, error) {
			target, err := requireString(args, "path")
			if err != nil {
				audit.record(ToolFSDelete, outcomeError, "", "", err)
				return nil, err
			}
			root, resolved, err := resolveTarget(roots, target)
			if err != nil {
				audit.record(ToolFSDelete, outcomeDenied, "", target, err)
				return nil, err
			}
			if !root.Delete {
				denied := fmt.Errorf("%s: delete denied for root %q", ToolFSDelete, root.Path)
				audit.record(ToolFSDelete, outcomeDenied, root.Path, resolved, denied)
				return nil, denied
			}

			if trashErr := trashPath(resolved); trashErr != nil {
				if !allowPermanent {
					werr := fmt.Errorf("%s: trash %s: %w", ToolFSDelete, resolved, trashErr)
					audit.record(ToolFSDelete, outcomeError, root.Path, resolved, werr)
					return nil, werr
				}
				if rmErr := removeAllFn(resolved); rmErr != nil {
					werr := fmt.Errorf("%s: delete %s: %w", ToolFSDelete, resolved, rmErr)
					audit.record(ToolFSDelete, outcomeError, root.Path, resolved, werr)
					return nil, werr
				}
				audit.record(ToolFSDelete, outcomeSuccess, root.Path, resolved, nil)
				return map[string]any{
					"path":      resolved,
					"trashed":   false,
					"permanent": true,
				}, nil
			}

			audit.record(ToolFSDelete, outcomeSuccess, root.Path, resolved, nil)
			return map[string]any{
				"path":      resolved,
				"trashed":   true,
				"permanent": false,
			}, nil
		},
	}
}
