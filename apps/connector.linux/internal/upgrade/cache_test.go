package upgrade

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCacheRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "latest-version.json")
	cache := &VersionCache{Version: "0.4.0", CheckedAt: time.Now().Truncate(time.Second)}
	if err := SaveCache(path, cache); err != nil {
		t.Fatalf("SaveCache: %v", err)
	}
	got, err := LoadCache(path)
	if err != nil {
		t.Fatalf("LoadCache: %v", err)
	}
	if got == nil {
		t.Fatal("LoadCache = nil, want cache")
	}
	if got.Version != "0.4.0" {
		t.Errorf("Version = %q, want 0.4.0", got.Version)
	}
	if !got.CheckedAt.Equal(cache.CheckedAt) {
		t.Errorf("CheckedAt = %v, want %v", got.CheckedAt, cache.CheckedAt)
	}
}

func TestLoadCacheMissing(t *testing.T) {
	got, err := LoadCache(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err != nil {
		t.Fatalf("LoadCache missing = %v, want nil", err)
	}
	if got != nil {
		t.Errorf("LoadCache missing = %+v, want nil", got)
	}
}

func TestLoadCacheCorrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "latest-version.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write corrupt cache: %v", err)
	}
	got, err := LoadCache(path)
	if err != nil {
		t.Fatalf("LoadCache corrupt = %v, want nil", err)
	}
	if got != nil {
		t.Errorf("LoadCache corrupt = %+v, want nil", got)
	}
}

func TestIsFresh(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name     string
		cache    *VersionCache
		interval time.Duration
		want     bool
	}{
		{name: "nil cache", cache: nil, interval: time.Hour, want: false},
		{name: "fresh", cache: &VersionCache{CheckedAt: now}, interval: time.Hour, want: true},
		{name: "stale", cache: &VersionCache{CheckedAt: now.Add(-2 * time.Hour)}, interval: time.Hour, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsFresh(tc.cache, tc.interval); got != tc.want {
				t.Errorf("IsFresh = %v, want %v", got, tc.want)
			}
		})
	}
}
