// Package secretstore is a small file-backed store for connector secrets. It
// keeps files under a single directory with mode 0600 (directory 0700) and
// writes atomically via a temp file + rename so a crash never leaves a partial
// secret behind.
package secretstore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Store persists named secrets as files inside a single directory.
type Store struct {
	dir string
}

// New returns a Store rooted at dir. The directory is created lazily on Save.
func New(dir string) Store {
	return Store{dir: dir}
}

// Path returns the on-disk path for name.
func (s Store) Path(name string) string {
	return filepath.Join(s.dir, name)
}

// Load returns the secret bytes for name. A missing secret returns (nil, nil),
// not an error, so callers can treat absence as "not configured".
func (s Store) Load(name string) ([]byte, error) {
	if err := validateName(name); err != nil {
		return nil, err
	}
	b, err := os.ReadFile(s.Path(name))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("secretstore: read %s: %w", name, err)
	}
	return b, nil
}

// Save writes data for name atomically: a temp file in the store directory is
// written, chmod 0600, and renamed over the target. The store directory is
// created with mode 0700 if needed.
func (s Store) Save(name string, data []byte) error {
	if err := validateName(name); err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("secretstore: create dir %s: %w", s.dir, err)
	}
	f, err := os.CreateTemp(s.dir, "."+name+".tmp-*")
	if err != nil {
		return fmt.Errorf("secretstore: create temp for %s: %w", name, err)
	}
	tmp := f.Name()
	cleanup := func() {
		_ = f.Close()
		_ = os.Remove(tmp)
	}
	if err := f.Chmod(0o600); err != nil {
		cleanup()
		return fmt.Errorf("secretstore: chmod temp for %s: %w", name, err)
	}
	if _, err := f.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("secretstore: write %s: %w", name, err)
	}
	if err := f.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("secretstore: sync %s: %w", name, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("secretstore: close %s: %w", name, err)
	}
	if err := os.Rename(tmp, s.Path(name)); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("secretstore: replace %s: %w", name, err)
	}
	return nil
}

// Delete removes the secret for name, ignoring an already-absent file.
func (s Store) Delete(name string) error {
	if err := validateName(name); err != nil {
		return err
	}
	if err := os.Remove(s.Path(name)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("secretstore: delete %s: %w", name, err)
	}
	return nil
}

// validateName rejects names that could escape the store directory.
func validateName(name string) error {
	if name == "" {
		return errors.New("secretstore: empty secret name")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("secretstore: invalid secret name %q", name)
	}
	if strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return fmt.Errorf("secretstore: invalid secret name %q", name)
	}
	if filepath.IsAbs(name) {
		return fmt.Errorf("secretstore: invalid secret name %q", name)
	}
	return nil
}
