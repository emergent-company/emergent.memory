package sandbox

import (
	"fmt"
	"path"
	"strings"
)

// buildFindPathPatterns converts a workspace_glob pattern into `find -path`
// patterns. Relative patterns are interpreted under searchPath; absolute
// patterns are passed through as given.
//
// `find -path` matches the whole path of each entry, so a relative pattern must
// be joined with the search root or it can never match when the root is
// /workspace (#394). "*" already crosses directory separators in `-path`, so a
// "**/" directory segment is redundant for one-or-more directories; a pattern
// containing "**" therefore yields two alternatives: the crossing form and the
// zero-directory form. Both are returned and OR-ed by buildFindListCommand.
//
// Examples with searchPath = "/workspace":
//
//	"*.go"                          -> ["/workspace/*.go"]
//	"apps/server/pkg/httputil/*.go" -> ["/workspace/apps/server/pkg/httputil/*.go"]
//	"**/*.go"                       -> ["/workspace/*/*.go", "/workspace/*.go"]
//	"apps/**/*.go"                  -> ["/workspace/apps/*/*.go", "/workspace/apps/*.go"]
//	"/workspace/apps/**/*.go"       -> ["/workspace/apps/*/*.go", "/workspace/apps/*.go"]
func buildFindPathPatterns(searchPath, pattern string) []string {
	if searchPath == "" {
		searchPath = workspaceDir
	}

	join := func(p string) string {
		if path.IsAbs(p) {
			return p
		}
		return path.Join(searchPath, p)
	}

	crossing := join(strings.ReplaceAll(pattern, "**", "*"))
	if !strings.Contains(pattern, "**/") {
		return []string{crossing}
	}

	// "**/" also means zero directories, which the crossing form above cannot
	// express (it needs at least one separator). Add that alternative.
	zeroDir := join(strings.ReplaceAll(pattern, "**/", ""))
	if zeroDir == crossing {
		return []string{crossing}
	}
	return []string{crossing, zeroDir}
}

// buildFindListCommand builds the `find` pipeline used by ListFiles. limit > 0
// caps the number of returned entries.
func buildFindListCommand(searchPath, pattern string, limit int) string {
	conditions := make([]string, 0, 2)
	for _, p := range buildFindPathPatterns(searchPath, pattern) {
		conditions = append(conditions, fmt.Sprintf("-path %q", p))
	}
	cmd := fmt.Sprintf("find %q \\( %s \\) -printf '%%T@ %%y %%s %%p\\n' 2>/dev/null | sort -rn",
		searchPath, strings.Join(conditions, " -o "))
	if limit > 0 {
		cmd += fmt.Sprintf(" | head -%d", limit)
	}
	return cmd
}
