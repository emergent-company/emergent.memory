package autoupdate

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSaveAndLoadCache(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "latest-version.json")

	want := &VersionCache{
		Version:     "1.2.3",
		CheckedAt:   time.Now().Truncate(time.Second),
		ReleaseBody: "## What's Changed\n### New Features\n- feature A",
		ReleaseURL:  "https://github.com/example/releases/tag/v1.2.3",
	}

	if err := SaveCache(path, want); err != nil {
		t.Fatalf("SaveCache: %v", err)
	}

	got, err := LoadCache(path)
	if err != nil {
		t.Fatalf("LoadCache: %v", err)
	}
	if got == nil {
		t.Fatal("LoadCache returned nil for existing file")
	}
	if got.Version != want.Version {
		t.Errorf("Version: got %q want %q", got.Version, want.Version)
	}
	if got.ReleaseURL != want.ReleaseURL {
		t.Errorf("ReleaseURL: got %q want %q", got.ReleaseURL, want.ReleaseURL)
	}
}

func TestLoadCache_Missing(t *testing.T) {
	got, err := LoadCache("/nonexistent/path/latest-version.json")
	if err != nil {
		t.Fatalf("expected nil error for missing file, got %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil cache for missing file")
	}
}

func TestLoadCache_Corrupt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := LoadCache(path)
	if err != nil {
		t.Fatalf("expected nil error for corrupt file, got %v", err)
	}
	if got != nil {
		t.Fatal("expected nil cache for corrupt file")
	}
}

func TestIsFresh(t *testing.T) {
	interval := 24 * time.Hour

	fresh := &VersionCache{CheckedAt: time.Now().Add(-1 * time.Hour)}
	if !IsFresh(fresh, interval) {
		t.Error("expected fresh cache to be fresh")
	}

	stale := &VersionCache{CheckedAt: time.Now().Add(-25 * time.Hour)}
	if IsFresh(stale, interval) {
		t.Error("expected stale cache to not be fresh")
	}

	if IsFresh(nil, interval) {
		t.Error("expected nil cache to not be fresh")
	}
}

func TestSaveCache_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "v.json")

	// Write twice — should not leave a .tmp file behind.
	c := &VersionCache{Version: "1.0.0", CheckedAt: time.Now()}
	_ = SaveCache(path, c)
	c.Version = "2.0.0"
	if err := SaveCache(path, c); err != nil {
		t.Fatalf("second SaveCache: %v", err)
	}

	got, _ := LoadCache(path)
	if got == nil || got.Version != "2.0.0" {
		t.Errorf("expected version 2.0.0, got %v", got)
	}

	// .tmp file must be gone.
	if _, err := os.Stat(path + ".tmp"); err == nil {
		t.Error(".tmp file should not exist after successful atomic save")
	}
}
