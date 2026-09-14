package main

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestAcquireLockSecondAcquireFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml.lock")
	release, err := acquireLock(path)
	if err != nil {
		t.Fatalf("first acquireLock: %v", err)
	}
	defer release()

	_, err = acquireLock(path)
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second acquireLock error = %v, want ErrAlreadyRunning", err)
	}
}

func TestAcquireLockReleasedThenAcquirable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.yml.lock")
	release, err := acquireLock(path)
	if err != nil {
		t.Fatalf("acquireLock: %v", err)
	}
	release()

	release2, err := acquireLock(path)
	if err != nil {
		t.Fatalf("acquireLock after release: %v", err)
	}
	release2()
}
