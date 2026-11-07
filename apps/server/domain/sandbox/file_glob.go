package sandbox

import (
	"path"
	"strings"
)

// buildFindPathPattern converts a workspace_glob pattern into the argument for
// `find -path`. Relative patterns are interpreted under searchPath; absolute
// patterns are passed through as given.
//
// `find -path` matches the whole path of each entry, so a relative pattern must
// be joined with the search root or it can never match when the root is
// /workspace (#394). "**" is converted to "*" because "*" already crosses
// directory separators in `-path`, preserving the historical "**/*.go" form.
//
// Examples with searchPath = "/workspace":
//
//	"*.go"                          -> "/workspace/*.go"
//	"apps/server/pkg/httputil/*.go" -> "/workspace/apps/server/pkg/httputil/*.go"
//	"**/*.go"                       -> "/workspace/*/*.go"
//	"/workspace/apps/**/*.go"       -> "/workspace/apps/*/*.go" (absolute, unchanged)
func buildFindPathPattern(searchPath, pattern string) string {
	findPattern := strings.ReplaceAll(pattern, "**", "*")
	if path.IsAbs(findPattern) {
		return findPattern
	}
	if searchPath == "" {
		searchPath = workspaceDir
	}
	return path.Join(searchPath, findPattern)
}
