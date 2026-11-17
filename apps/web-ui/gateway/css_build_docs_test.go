package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCSSTaskCommentDescribesSelfContainedNodeModules pins #913: the header
// comment in webui/css/app.css claimed node_modules was symlinked from
// /root/go-daisy/node_modules. That stopped being true when the agent rig made
// its go-daisy checkout opt-in (infra#1312) — the css task installs this
// module's own pinned deps. A reader on a fresh host followed the stale comment
// to a directory the rig does not create and could conclude their environment
// was broken, so the comment must describe the task that exists.
func TestCSSTaskCommentDescribesSelfContainedNodeModules(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("webui", "css", "app.css"))
	if err != nil {
		t.Fatalf("read webui/css/app.css: %v", err)
	}
	header := leadingCSSComment(string(b))
	if header == "" {
		t.Fatal("webui/css/app.css does not start with a /* … */ header comment")
	}

	// The removed bootstrap instruction must not come back...
	for _, stale := range []string{"/root/go-daisy/node_modules", "symlinked from"} {
		if strings.Contains(header, stale) {
			t.Errorf("app.css header still names the removed node_modules bootstrap %q — the css task installs this module's own deps via npm ci; nothing symlinks /root/go-daisy", stale)
		}
	}
	// ...and the mechanism that actually provides node_modules must be named.
	for _, want := range []string{"npm ci", "package-lock.json"} {
		if !strings.Contains(header, want) {
			t.Errorf("app.css header does not mention %q, the mechanism the css task uses to populate node_modules", want)
		}
	}

	// Tie the comment to its source of truth: the css task must still install
	// the deps itself, or the rewritten comment would drift the other way.
	task, err := os.ReadFile("Taskfile.yml")
	if err != nil {
		t.Fatalf("read Taskfile.yml: %v", err)
	}
	if !strings.Contains(string(task), "test -e node_modules || npm ci") {
		t.Error("the css task no longer runs `npm ci` when node_modules is absent — app.css's header comment describes that step, so both must change together")
	}
}

// leadingCSSComment returns the leading /* … */ block of src, or "" when the
// file does not start with one.
func leadingCSSComment(src string) string {
	src = strings.TrimLeft(src, " \t\r\n")
	if !strings.HasPrefix(src, "/*") {
		return ""
	}
	end := strings.Index(src, "*/")
	if end < 0 {
		return ""
	}
	return src[:end+2]
}
