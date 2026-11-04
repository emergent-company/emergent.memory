package linux

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/toolreg"
)

// newFSWriteTool builds the linux-fs-write tool. Writes require a root that
// grants Write, are bounded by maxBytes, and are atomic (temp file in the
// target directory, chmod 0600, rename over the target).
func newFSWriteTool(roots []Root, maxBytes int64, audit *auditLogger) toolreg.Tool {
	if maxBytes <= 0 {
		maxBytes = defaultMaxBytes
	}
	return toolreg.Tool{
		Name:        ToolFSWrite,
		Description: fmt.Sprintf("Atomically write content to a file inside an allowed root (up to %d bytes). Rejects out-of-root paths and leaves any existing file unchanged on failure.", maxBytes),
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "absolute path to a file inside an allowed root",
				},
				"content": map[string]any{
					"type":        "string",
					"description": "file content to write (may be empty)",
				},
			},
			"required": []string{"path", "content"},
		},
		Handler: func(_ context.Context, args map[string]any) (map[string]any, error) {
			target, err := requireString(args, "path")
			if err != nil {
				audit.record(ToolFSWrite, outcomeError, "", "", err)
				return nil, err
			}
			content, err := requireStringAllowEmpty(args, "content")
			if err != nil {
				audit.record(ToolFSWrite, outcomeError, "", target, err)
				return nil, err
			}
			root, resolved, err := resolveTarget(roots, target)
			if err != nil {
				audit.record(ToolFSWrite, outcomeDenied, "", target, err)
				return nil, err
			}
			if !root.Write {
				denied := fmt.Errorf("%s: write denied for root %q", ToolFSWrite, root.Path)
				audit.record(ToolFSWrite, outcomeDenied, root.Path, resolved, denied)
				return nil, denied
			}
			if err := checkParentContained(root.Path, resolved); err != nil {
				audit.record(ToolFSWrite, outcomeDenied, root.Path, resolved, err)
				return nil, err
			}
			if int64(len(content)) > maxBytes {
				oversized := fmt.Errorf("%s: content is %d bytes, exceeds limit %d", ToolFSWrite, len(content), maxBytes)
				audit.record(ToolFSWrite, outcomeError, root.Path, resolved, oversized)
				return nil, oversized
			}
			if err := atomicWriteFile(resolved, []byte(content)); err != nil {
				werr := fmt.Errorf("%s: write %s: %w", ToolFSWrite, resolved, err)
				audit.record(ToolFSWrite, outcomeError, root.Path, resolved, werr)
				return nil, werr
			}
			audit.record(ToolFSWrite, outcomeSuccess, root.Path, resolved, nil)
			return map[string]any{
				"path":    resolved,
				"bytes":   len(content),
				"written": true,
			}, nil
		},
	}
}

// checkParentContained re-checks that the target's parent is inside rootPath.
// resolveTarget already resolves through the deepest existing ancestor, but an
// explicit check keeps the guarantee local to mutating calls.
func checkParentContained(rootPath, resolved string) error {
	resolvedRoot, err := resolvePath(rootPath)
	if err != nil {
		return err
	}
	parent, err := resolvePath(filepath.Dir(resolved))
	if err != nil {
		return err
	}
	if !contains(resolvedRoot, parent) {
		return fmt.Errorf("%w: parent of %s", ErrOutsideRoots, resolved)
	}
	return nil
}

// atomicWriteFile writes data to path via a temp file in the same directory,
// then renames it over path. On any failure the temp file is removed and the
// existing target is left unchanged.
func atomicWriteFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}
	if err := tmp.Chmod(0o600); err != nil {
		cleanup()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

// requireStringAllowEmpty is requireString but permits an empty value.
func requireStringAllowEmpty(args map[string]any, key string) (string, error) {
	v, ok := args[key]
	if !ok || v == nil {
		return "", fmt.Errorf("missing required argument %q", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("argument %q must be a string, got %T", key, v)
	}
	return s, nil
}
