package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildFindPathPatterns(t *testing.T) {
	tests := []struct {
		name       string
		searchPath string
		pattern    string
		want       []string
	}{
		{
			name:       "filename only at workspace",
			searchPath: "/workspace",
			pattern:    "*.go",
			want:       []string{"/workspace/*.go"},
		},
		{
			name:       "repo-relative directory",
			searchPath: "/workspace",
			pattern:    "apps/server/pkg/httputil/*.go",
			want:       []string{"/workspace/apps/server/pkg/httputil/*.go"},
		},
		{
			name:       "double star at workspace covers zero or more dirs",
			searchPath: "/workspace",
			pattern:    "**/*.go",
			want:       []string{"/workspace/*/*.go", "/workspace/*.go"},
		},
		{
			name:       "double star mid-pattern",
			searchPath: "/workspace",
			pattern:    "apps/server/**/*.go",
			want:       []string{"/workspace/apps/server/*/*.go", "/workspace/apps/server/*.go"},
		},
		{
			name:       "absolute pattern with double star",
			searchPath: "/workspace",
			pattern:    "/workspace/apps/server/**/*.go",
			want:       []string{"/workspace/apps/server/*/*.go", "/workspace/apps/server/*.go"},
		},
		{
			name:       "absolute single star unchanged",
			searchPath: "/workspace",
			pattern:    "/workspace/pkg/*.go",
			want:       []string{"/workspace/pkg/*.go"},
		},
		{
			name:       "relative search path stays relative",
			searchPath: "apps",
			pattern:    "**/*.go",
			want:       []string{"apps/*/*.go", "apps/*.go"},
		},
		{
			name:       "empty search path defaults to workspace",
			searchPath: "",
			pattern:    "*.go",
			want:       []string{"/workspace/*.go"},
		},
		{
			name:       "bare double star does not duplicate",
			searchPath: "/workspace",
			pattern:    "**",
			want:       []string{"/workspace/*"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, buildFindPathPatterns(tt.searchPath, tt.pattern))
		})
	}
}

func TestBuildFindListCommand(t *testing.T) {
	t.Run("single pattern has no OR", func(t *testing.T) {
		cmd := buildFindListCommand("/workspace", "apps/*.go", 0)
		assert.Contains(t, cmd, `find "/workspace" \( -path "/workspace/apps/*.go" \) -printf`)
		assert.NotContains(t, cmd, "-o ")
		assert.NotContains(t, cmd, "head")
	})

	t.Run("double star ORs both forms", func(t *testing.T) {
		cmd := buildFindListCommand("/workspace", "**/*.go", 0)
		assert.Contains(t, cmd, `-path "/workspace/*/*.go" -o -path "/workspace/*.go"`)
	})

	t.Run("limit appends head", func(t *testing.T) {
		cmd := buildFindListCommand("/home/user", "**/*.go", 1000)
		assert.Contains(t, cmd, `find "/home/user" \( -path "/home/user/*/*.go" -o -path "/home/user/*.go" \)`)
		assert.True(t, strings.HasSuffix(cmd, "| head -1000"))
	})
}

// TestBuildFindListCommand_MatchesWithFind exercises the real `find -path`
// semantics, which is the bug in #394: a relative pattern matched against the
// full path never matches anything when the search root is /workspace.
func TestBuildFindListCommand_MatchesWithFind(t *testing.T) {
	// find is a base dependency of the repo (docker/workspace-base.Dockerfile
	// installs findutils); if it is missing this test must fail, not skip.
	root := t.TempDir()
	for _, rel := range []string{
		"main.go", // depth 0: must match "**/*.go"
		"apps/server/main.go",
		"apps/server/pkg/httputil/a.go",
		"apps/server/pkg/httputil/b_test.go",
		"notes.txt",
	} {
		p := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte("package x\n"), 0o644))
	}

	// find runs the exact pipeline buildFindListCommand emits, so
	// multi-pattern OR expressions are exercised end to end.
	find := func(searchRoot, pattern string) []string {
		cmd := buildFindListCommand(searchRoot, pattern, 0)
		out, ferr := exec.Command("sh", "-c", cmd).Output()
		require.NoError(t, ferr)
		var rels []string
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if line == "" {
				continue
			}
			fields := strings.SplitN(line, " ", 4)
			require.Len(t, fields, 4)
			rel, rerr := filepath.Rel(searchRoot, fields[3])
			require.NoError(t, rerr)
			rels = append(rels, filepath.ToSlash(rel))
		}
		sort.Strings(rels)
		return rels
	}

	allGo := []string{
		"apps/server/main.go",
		"apps/server/pkg/httputil/a.go",
		"apps/server/pkg/httputil/b_test.go",
		"main.go",
	}

	t.Run("repo-relative pattern matches under the search root", func(t *testing.T) {
		assert.Equal(t, []string{
			"apps/server/pkg/httputil/a.go",
			"apps/server/pkg/httputil/b_test.go",
		}, find(root, "apps/server/pkg/httputil/*.go"))
	})

	t.Run("filename-only glob matches at any depth", func(t *testing.T) {
		assert.Equal(t, allGo, find(root, "*.go"))
	})

	t.Run("double-star pattern matches depth 0 and deeper", func(t *testing.T) {
		assert.Equal(t, allGo, find(root, "**/*.go"))
	})

	t.Run("double-star pattern on a flat scoped directory", func(t *testing.T) {
		flat := filepath.Join(root, "apps/server/pkg/httputil")
		assert.Equal(t, []string{"a.go", "b_test.go"}, find(flat, "**/*.go"))
	})

	t.Run("absolute pattern matches as before", func(t *testing.T) {
		assert.Equal(t, []string{
			"apps/server/pkg/httputil/a.go",
			"apps/server/pkg/httputil/b_test.go",
		}, find(root, root+"/apps/server/pkg/httputil/*.go"))
	})

	t.Run("command pipeline ORs the double-star forms", func(t *testing.T) {
		assert.Equal(t, allGo, find(root, "**/*.go"))
	})
}
