//go:build linux

package linux

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Injectable seams for deterministic tests.
var (
	renameFn = os.Rename
	topdirFn = mountTopdir
	getuid   = os.Getuid
)

// trashPathPlatform trashes path, preferring the home trash and falling back to
// a mount-topdir trash on EXDEV. On any failure the target is left intact.
func trashPathPlatform(path string) error {
	if path == string(os.PathSeparator) {
		return errors.New("trash: refusing to trash the filesystem root")
	}
	if home, err := homeTrashDir(); err == nil {
		renameErr := moveToTrashDir(path, home, path)
		if renameErr == nil {
			return nil
		}
		if !errors.Is(renameErr, syscall.EXDEV) {
			return renameErr
		}
	}
	return moveToTopdirTrash(path)
}

// homeTrashDir returns $XDG_DATA_HOME/Trash (or ~/.local/share/Trash).
func homeTrashDir() (string, error) {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		if !filepath.IsAbs(xdg) {
			return "", errors.New("trash: XDG_DATA_HOME must be absolute")
		}
		return filepath.Join(xdg, "Trash"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", errors.New("trash: cannot determine home directory")
	}
	return filepath.Join(home, ".local", "share", "Trash"), nil
}

// moveToTopdirTrash uses the mount topdir's shared .Trash/<uid> when it is a
// non-symlink directory with the sticky bit, otherwise a private .Trash-<uid>
// directory. The recorded Path= is relative to the topdir.
func moveToTopdirTrash(path string) error {
	topdir, err := topdirFn(path)
	if err != nil {
		return fmt.Errorf("trash: %w", err)
	}
	uid := strconv.Itoa(getuid())

	shared := filepath.Join(topdir, ".Trash")
	if st, statErr := os.Lstat(shared); statErr == nil && st.IsDir() && st.Mode()&os.ModeSymlink == 0 && st.Mode()&os.ModeSticky != 0 {
		if userTrash := filepath.Join(shared, uid); ensureDir(userTrash, 0o700) == nil {
			if rel, relErr := filepath.Rel(topdir, path); relErr == nil {
				return moveToTrashDir(path, userTrash, rel)
			}
		}
	}

	fallback := filepath.Join(topdir, ".Trash-"+uid)
	if err := ensureDir(fallback, 0o700); err != nil {
		return fmt.Errorf("trash: no usable topdir trash under %s: %w", topdir, err)
	}
	rel, err := filepath.Rel(topdir, path)
	if err != nil {
		return fmt.Errorf("trash: %s is not under topdir %s: %w", path, topdir, err)
	}
	return moveToTrashDir(path, fallback, rel)
}

// moveToTrashDir creates files/ and info/ under trashRoot and moves path into
// files/<unique>. The .trashinfo is written first (O_CREATE|O_EXCL); if the
// move fails the info file is rolled back so no dangling record remains.
func moveToTrashDir(path, trashRoot, displayPath string) error {
	if isSameOrAncestor(path, trashRoot) {
		return fmt.Errorf("trash: refusing to trash %s: it contains the trash directory %s", path, trashRoot)
	}
	filesDir := filepath.Join(trashRoot, "files")
	infoDir := filepath.Join(trashRoot, "info")
	if err := ensureDir(filesDir, 0o700); err != nil {
		return err
	}
	if err := ensureDir(infoDir, 0o700); err != nil {
		return err
	}

	name, err := uniqueTrashName(filesDir, infoDir, filepath.Base(path))
	if err != nil {
		return err
	}
	infoPath := filepath.Join(infoDir, name+trashInfoSuffix)
	f, err := os.OpenFile(infoPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(trashInfoBody(percentEncodePath(displayPath), time.Now())); err != nil {
		_ = f.Close()
		_ = os.Remove(infoPath)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(infoPath)
		return err
	}
	if err := renameFn(path, filepath.Join(filesDir, name)); err != nil {
		_ = os.Remove(infoPath)
		return err
	}
	return nil
}

// mountTopdir returns the most specific mount point containing target.
func mountTopdir(target string) (string, error) {
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return "", fmt.Errorf("read mountinfo: %w", err)
	}
	best := ""
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		mountPoint := unescapeMountField(fields[4])
		if !isSameOrAncestor(mountPoint, target) {
			continue
		}
		if len(mountPoint) > len(best) {
			best = mountPoint
		}
	}
	if best == "" {
		return "", fmt.Errorf("no mount point found for %s", target)
	}
	return best, nil
}

// unescapeMountField decodes the octal escapes (e.g. \040 for space) used in
// /proc/self/mountinfo.
func unescapeMountField(s string) string {
	if !strings.Contains(s, "\\") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+4 <= len(s) {
			if v, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(v))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// isSameOrAncestor reports whether a == b or b is below a.
func isSameOrAncestor(a, b string) bool {
	rel, err := filepath.Rel(a, b)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel)
}
