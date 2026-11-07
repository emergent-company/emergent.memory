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

func TestBuildFindPathPattern(t *testing.T) {
	tests := []struct {
		name       string
		searchPath string
		pattern    string
		want       string
	}{
		{
			name:       "filename only at workspace",
			searchPath: "/workspace",
			pattern:    "*.go",
			want:       "/workspace/*.go",
		},
		{
			name:       "repo-relative directory",
			searchPath: "/workspace",
			pattern:    "apps/server/pkg/httputil/*.go",
			want:       "/workspace/apps/server/pkg/httputil/*.go",
		},
		{
			name:       "double star at workspace",
			searchPath: "/workspace",
			pattern:    "**/*.go",
			want:       "/workspace/*/*.go",
		},
		{
			name:       "double star mid-pattern",
			searchPath: "/workspace",
			pattern:    "apps/**/*.go",
			want:       "/workspace/apps/*/*.go",
		},
		{
			name:       "absolute pattern unchanged",
			searchPath: "/workspace",
			pattern:    "/workspace/apps/server/**/*.go",
			want:       "/workspace/apps/server/*/*.go",
		},
		{
			name:       "absolute single star unchanged",
			searchPath: "/workspace",
			pattern:    "/workspace/pkg/*.go",
			want:       "/workspace/pkg/*.go",
		},
		{
			name:       "relative search path stays relative",
			searchPath: "apps",
			pattern:    "*.go",
			want:       "apps/*.go",
		},
		{
			name:       "empty search path defaults to workspace",
			searchPath: "",
			pattern:    "*.go",
			want:       "/workspace/*.go",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, buildFindPathPattern(tt.searchPath, tt.pattern))
		})
	}
}

// TestBuildFindPathPattern_MatchesWithFind exercises the real `find -path`
// semantics, which is the bug in #394: a relative pattern matched against the
// full path never matches anything when the search root is /workspace.
func TestBuildFindPathPattern_MatchesWithFind(t *testing.T) {
	findBin, err := exec.LookPath("find")
	if err != nil {
		t.Skip("find not available")
	}

	root := t.TempDir()
	for _, rel := range []string{
		"apps/server/pkg/httputil/a.go",
		"apps/server/pkg/httputil/b_test.go",
		"apps/server/main.go",
		"notes.txt",
	} {
		p := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte("package x\n"), 0o644))
	}

	find := func(pattern string) []string {
		out, ferr := exec.Command(findBin, root, "-type", "f", "-path", buildFindPathPattern(root, pattern)).Output()
		require.NoError(t, ferr)
		var rels []string
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if line == "" {
				continue
			}
			rel, rerr := filepath.Rel(root, line)
			require.NoError(t, rerr)
			rels = append(rels, filepath.ToSlash(rel))
		}
		sort.Strings(rels)
		return rels
	}

	t.Run("repo-relative pattern matches under the search root", func(t *testing.T) {
		assert.Equal(t, []string{
			"apps/server/pkg/httputil/a.go",
			"apps/server/pkg/httputil/b_test.go",
		}, find("apps/server/pkg/httputil/*.go"))
	})

	t.Run("filename-only glob matches at any depth", func(t *testing.T) {
		assert.Equal(t, []string{
			"apps/server/main.go",
			"apps/server/pkg/httputil/a.go",
			"apps/server/pkg/httputil/b_test.go",
		}, find("*.go"))
	})

	t.Run("double-star pattern still matches recursively", func(t *testing.T) {
		assert.Equal(t, []string{
			"apps/server/main.go",
			"apps/server/pkg/httputil/a.go",
			"apps/server/pkg/httputil/b_test.go",
		}, find("**/*.go"))
	})

	t.Run("absolute pattern matches as before", func(t *testing.T) {
		assert.Equal(t, []string{
			"apps/server/pkg/httputil/a.go",
			"apps/server/pkg/httputil/b_test.go",
		}, find(root+"/apps/server/pkg/httputil/*.go"))
	})
}
