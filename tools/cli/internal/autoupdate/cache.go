// Package autoupdate provides background version checking and self-update logic.
package autoupdate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// VersionCache stores the result of the last GitHub release check so that
// repeated CLI invocations do not hammer the API.
type VersionCache struct {
	Version     string    `json:"version"`
	CheckedAt   time.Time `json:"checked_at"`
	ReleaseBody string    `json:"release_body"`
	ReleaseURL  string    `json:"release_url"`
}

// DefaultCachePath returns the default path for the version cache file.
func DefaultCachePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".memory", "cache", "latest-version.json"), nil
}

// LoadCache reads the version cache from disk.
// Returns nil (no error) when the file does not exist or is corrupt — callers
// should treat nil as "cache miss".
func LoadCache(path string) (*VersionCache, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var c VersionCache
	if err := json.Unmarshal(data, &c); err != nil {
		// Corrupt cache — treat as a miss, not a hard error.
		return nil, nil
	}
	return &c, nil
}

// SaveCache writes the cache to disk atomically (write to tmp, rename).
func SaveCache(path string, cache *VersionCache) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	data, err := json.Marshal(cache)
	if err != nil {
		return err
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}

	return os.Rename(tmp, path)
}

// IsFresh returns true when the cache was populated within the given interval,
// meaning a fresh network check is not yet needed.
func IsFresh(cache *VersionCache, interval time.Duration) bool {
	if cache == nil {
		return false
	}
	return time.Since(cache.CheckedAt) < interval
}
