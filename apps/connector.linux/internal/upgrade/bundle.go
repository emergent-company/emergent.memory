package upgrade

import (
	"path/filepath"
	"strings"
)

// IsInsideAppBundle reports whether the executable path resolves inside a
// macOS application bundle. Symlinks are resolved first (EvalSymlinks) so a
// link that points into a bundle is detected. A path is treated as inside a
// bundle when any directory component ends in ".app" (the bundle root) or when
// it descends through a Contents/MacOS pair (the standard bundle executable
// location).
func IsInsideAppBundle(path string) bool {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	parts := strings.Split(filepath.Clean(path), string(filepath.Separator))
	for i, part := range parts {
		if strings.HasSuffix(part, ".app") {
			return true
		}
		if part == "Contents" && i+1 < len(parts) && parts[i+1] == "MacOS" {
			return true
		}
	}
	return false
}
