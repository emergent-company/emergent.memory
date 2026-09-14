package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// ErrAlreadyRunning is returned by acquireLock when another process already
// holds the lock file.
var ErrAlreadyRunning = errors.New("already running")

// acquireLock takes a non-blocking exclusive advisory flock on path, creating
// the file (0600) and its parent directory as needed. The returned release
// closes the descriptor, dropping the lock; the lock is also released
// automatically when the process exits.
//
// The lock is advisory and per-open-file-description: it guards against two
// connector processes sharing a config, not against unrelated writers.
func acquireLock(path string) (release func(), err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create lock dir for %s: %w", path, err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock %s: %w", path, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if err == syscall.EWOULDBLOCK || err == syscall.EAGAIN {
			return nil, fmt.Errorf("%w (lock held: %s)", ErrAlreadyRunning, path)
		}
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
