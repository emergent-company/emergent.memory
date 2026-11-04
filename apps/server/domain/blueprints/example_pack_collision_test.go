package blueprints

import (
	"io/fs"
	"os"
	"path"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExamplePackNamesDoNotCollideWithBuiltins guards the example packs against
// shadowing a built-in blueprint by name.
//
// Both are installed by name, so an example whose blueprint name matches a
// built-in is a second copy of the same pack — and the copy nobody re-checks
// when the built-in changes. That is how blueprints/agent-notes and
// blueprints/task-board drifted to an older schema (issue #2324): installing
// from the example path silently replaced the built-in with a stale version.
//
// Names are resolved through loadBlueprintDir, the same loader an install uses,
// so this compares what the blueprint is actually called rather than the
// directory it happens to live in (an example directory may carry the name of
// its first schema pack).
func TestExamplePackNamesDoNotCollideWithBuiltins(t *testing.T) {
	builtinNames := blueprintNames(t, builtinPacksFS, "builtin_packs", "embedded builtin packs")
	require.NotEmpty(t, builtinNames, "expected the embedded built-in packs to load")

	// The example packs are installed from the checkout rather than embedded in
	// the binary; guard them when the checkout is present.
	const exampleRoot = "../../../../blueprints"
	if _, err := os.Stat(exampleRoot); err != nil {
		t.Logf("example packs not found at %s; nothing can shadow the built-ins", exampleRoot)
		return
	}
	exampleNames := blueprintNames(t, os.DirFS(exampleRoot), ".", "example packs")
	require.NotEmpty(t, exampleNames, "expected the example packs to load")

	for name, exampleDir := range exampleNames {
		if builtinDir, collides := builtinNames[name]; collides {
			assert.Failf(t, "example pack shadows a built-in blueprint",
				"example %q and built-in %q are both blueprints named %q; keep one copy so they cannot drift",
				exampleDir, builtinDir, name)
		}
	}
}

// blueprintNames maps each top-level blueprint directory under root to the
// blueprint name loadBlueprintDir resolves for it.
func blueprintNames(t *testing.T, fsys fs.FS, root, source string) map[string]string {
	t.Helper()

	entries, err := fs.ReadDir(fsys, root)
	require.NoErrorf(t, err, "%s: read %s", source, root)

	names := make(map[string]string, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := path.Join(root, e.Name())
		desc, err := loadBlueprintDir(fsys, dir, e.Name())
		require.NoErrorf(t, err, "%s: %s must load as a blueprint", source, dir)
		if other, dup := names[desc.Name]; dup {
			assert.Failf(t, "duplicate blueprint name", "%s: %s and %s are both named %q", source, other, dir, desc.Name)
		}
		names[desc.Name] = dir
	}
	return names
}
