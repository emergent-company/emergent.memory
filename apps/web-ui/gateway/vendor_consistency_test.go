package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// goDaisyModule is the import path whose vendored copy is a CSS build input:
// webui/css/app.css @source-s the vendored components and @import-s their
// co-located custom.css (see godaisy_css.go and Taskfile.yml `css`).
const goDaisyModule = "github.com/emergent-company/go-daisy"

// TestVendoredGoDaisyMatchesGoModPin guards the gitignored vendor/ tree: when it
// is present, its go-daisy copy must match the version pinned in go.mod. A stale
// tree builds the compiled CSS against the wrong component source with no other
// signal.
//
// vendor/ is untracked, so a fresh checkout (and CI) has no tree and this test
// skips; CI relies instead on the CSS build regenerating the tree for the pinned
// version (`task css`, which vendors on a go.mod/vendor mismatch).
func TestVendoredGoDaisyMatchesGoModPin(t *testing.T) {
	vendored := filepath.Join("vendor", "modules.txt")
	if _, err := os.Stat(vendored); err != nil {
		t.Skipf("vendor/ absent: CI relies on the CSS build regenerating it for the pinned go-daisy version")
	}

	pin := goDaisyVersionInGoMod(t)
	got := goDaisyVersionInVendor(t, vendored)
	if pin == "" {
		t.Fatal("no go-daisy requirement found in go.mod")
	}
	if got == "" {
		t.Fatalf("no go-daisy entry found in %s", vendored)
	}
	if pin != got {
		t.Errorf("vendored go-daisy %q disagrees with go.mod pin %q — regenerate with `task css` (or `GOWORK=off go mod vendor`)", got, pin)
	}
}

// goDaisyVersionInGoMod returns the version the go.mod require pins go-daisy to,
// or "" when absent. The requirement is the line
// `github.com/emergent-company/go-daisy vX.Y.Z-...` in a require block.
func goDaisyVersionInGoMod(t *testing.T) string {
	t.Helper()
	f, err := os.Open("go.mod")
	if err != nil {
		t.Fatalf("open go.mod: %v", err)
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		rest, ok := strings.CutPrefix(line, goDaisyModule)
		if !ok || rest == "" || (rest[0] != ' ' && rest[0] != '\t') {
			continue
		}
		if fields := strings.Fields(rest); len(fields) > 0 {
			return fields[0]
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan go.mod: %v", err)
	}
	return ""
}

// goDaisyVersionInVendor returns the go-daisy version recorded in
// vendor/modules.txt, or "" when absent. The entry is the comment line
// `# github.com/emergent-company/go-daisy vX.Y.Z-...`.
func goDaisyVersionInVendor(t *testing.T, path string) string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		rest, ok := strings.CutPrefix(line, "# "+goDaisyModule)
		if !ok || rest == "" || (rest[0] != ' ' && rest[0] != '\t') {
			continue
		}
		if fields := strings.Fields(rest); len(fields) > 0 {
			return fields[0]
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan %s: %v", path, err)
	}
	return ""
}
