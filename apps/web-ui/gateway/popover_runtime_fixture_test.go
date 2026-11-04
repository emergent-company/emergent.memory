package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestPopoverFixtureMatchesPinnedRuntime guards the #1440 harness. The js-dom
// spec specs/js/popover-runtime.spec.ts loads a *committed snapshot* of
// go-daisy's staticfs/static/js/go-daisy-popover.js (the same file
// ui.PopoverScript serves), not the module at build time. Without this pin, a
// go-daisy upgrade would leave the snapshot stale and the spec would keep
// passing against code the app no longer ships — the exact false-confidence
// gap #1440 set out to close.
//
// It re-resolves the module directory at the go.mod version and asserts the
// committed fixture body still matches verbatim, ignoring the fixture's
// one-line provenance header.
func TestPopoverFixtureMatchesPinnedRuntime(t *testing.T) {
	const module = "github.com/emergent-company/go-daisy"
	const rel = "staticfs/static/js/go-daisy-popover.js"

	out, err := exec.CommandContext(t.Context(), "go", "list", "-m", "-f", "{{.Dir}}", module).Output()
	if err != nil {
		t.Fatalf("cannot resolve %s module dir: %v", module, err)
	}
	dir := strings.TrimSpace(string(out))
	if dir == "" {
		// `-mod=vendor` — the mode the image builds in, and the mode CI runs
		// once `task css` has vendored go-daisy — reports no module directory
		// (the module cache is not consulted). The vendored copy is then the
		// file the build actually ships.
		dir = filepath.Join("vendor", filepath.FromSlash(module))
	}
	want, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("pinned runtime %s not readable: %v", rel, err)
	}

	got, err := os.ReadFile(filepath.Join("..", "tests", "e2e", "fixtures", "go-daisy-popover.js"))
	if err != nil {
		t.Fatalf("committed popover fixture unreadable: %v", err)
	}
	// The committed copy prepends a provenance comment; strip it so the
	// assertion is on the verbatim runtime body.
	if rest, ok := strings.CutPrefix(string(got), "/* Synced verbatim from"); ok {
		if i := strings.IndexByte(rest, '\n'); i >= 0 {
			got = []byte(rest[i+1:])
		}
	}
	if string(got) != string(want) {
		t.Fatalf("popover fixture is out of sync with %s@go.mod — re-copy %s into tests/e2e/fixtures/", module, rel)
	}
}
