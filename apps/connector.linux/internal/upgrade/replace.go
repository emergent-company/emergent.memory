package upgrade

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Executable returns the path of the running executable. It is a variable so
// tests can redirect the replacement at a temp file instead of the test binary.
var Executable = os.Executable

// Replace atomically swaps the file at target for data. The new bytes are
// written to a temp file in the target's directory, given the target's
// permission bits (or 0755 when the target is new), and renamed over the
// target in one step. A failure before the rename leaves the original in place.
func Replace(target string, data []byte) error {
	mode := os.FileMode(0o755)
	if info, err := os.Stat(target); err == nil {
		// Preserve the installed binary's mode; only fall back to 0755 when the
		// existing file is not executable (or has no permission bits at all).
		if perm := info.Mode().Perm(); perm&0o111 != 0 {
			mode = perm
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat %s: %w", target, err)
	}

	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(target)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	remove := true
	defer func() {
		if remove {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", tmpName, err)
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, target); err != nil {
		// The original is untouched: the rename failed before swapping.
		return fmt.Errorf("replace %s: %w", target, err)
	}
	remove = false
	return nil
}
