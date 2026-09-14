package linux

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// trashNameMax is the usual NAME_MAX for a single path component.
const trashNameMax = 255

// trashInfoSuffix is the suffix of XDG trash info files.
const trashInfoSuffix = ".trashinfo"

// removeAllFn permanently removes a path. Injectable for tests.
var removeAllFn = os.RemoveAll

// trashPath moves an absolute, already-resolved path into an XDG trash. It is a
// variable so tests can force the "trash unavailable" path (and so the platform
// fallback can be swapped).
var trashPath = trashPathPlatform

// trashInfoBody renders the XDG .trashinfo body. encodedPath must already be
// percent-encoded; when is formatted as local time with no timezone offset.
func trashInfoBody(encodedPath string, when time.Time) string {
	return "[Trash Info]\nPath=" + encodedPath + "\nDeletionDate=" + when.Format("2006-01-02T15:04:05") + "\n"
}

// percentEncodePath percent-encodes a path per RFC 2396, leaving unreserved
// characters and the path separator intact. Unlike url.PathEscape it leaves
// '!' and '~' alone and always uses uppercase hex.
func percentEncodePath(p string) string {
	var b strings.Builder
	for i := 0; i < len(p); i++ {
		c := p[i]
		if c == '/' || isRFC2396Unreserved(c) {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", c)
	}
	return b.String()
}

func isRFC2396Unreserved(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	}
	return strings.IndexByte("-_.!~*'()", c) >= 0
}

// uniqueTrashName returns a name not used by files/<name> or
// info/<name>.trashinfo, appending .1, .2, ... on collision. The base is
// truncated so "<name>.trashinfo" fits NAME_MAX.
func uniqueTrashName(filesDir, infoDir, name string) (string, error) {
	for i := 0; i < 100000; i++ {
		suffix := ""
		if i > 0 {
			suffix = fmt.Sprintf(".%d", i)
		}
		candidate := fitTrashName(name, suffix)
		if !pathExists(filepath.Join(filesDir, candidate)) && !pathExists(filepath.Join(infoDir, candidate+trashInfoSuffix)) {
			return candidate, nil
		}
	}
	return "", errors.New("trash: could not find a unique name")
}

// fitTrashName truncates name so name+suffix+".trashinfo" fits NAME_MAX.
func fitTrashName(name, suffix string) string {
	maxBase := trashNameMax - len(trashInfoSuffix) - len(suffix)
	if maxBase < 1 {
		maxBase = 1
	}
	if len(name) > maxBase {
		name = name[:maxBase]
	}
	return name + suffix
}

func pathExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// ensureDir creates dir (and parents) with mode.
func ensureDir(dir string, mode os.FileMode) error {
	return os.MkdirAll(dir, mode)
}
