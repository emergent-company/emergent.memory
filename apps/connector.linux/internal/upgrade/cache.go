package upgrade

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// VersionCache stores the result of the last version check so the daemon does
// not hammer the GitHub API on every tick.
type VersionCache struct {
	// Version is the newest release version, as a bare semver string.
	Version string `json:"version"`
	// CheckedAt is when the check last fetched the release list.
	CheckedAt time.Time `json:"checked_at"`
}

// DefaultCachePath returns the default path for the version cache file, under
// the OS user cache dir (os.UserCacheDir()). When that is unavailable it falls
// back to ~/.cache, and finally to a bare file in the working directory.
func DefaultCachePath() string {
	if dir, err := os.UserCacheDir(); err == nil && dir != "" {
		return filepath.Join(dir, "memory-connector", "latest-version.json")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".cache", "memory-connector", "latest-version.json")
	}
	return "latest-version.json"
}

// LoadCache reads the version cache from disk. It returns nil (no error) when
// the file does not exist or is corrupt — callers treat nil as a cache miss.
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

// SaveCache writes the cache to disk atomically (write to a temp file in the
// same directory, then rename over the target).
func SaveCache(path string, cache *VersionCache) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(cache)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// IsFresh reports whether the cache was populated within interval, meaning a
// fresh network check is not yet needed.
func IsFresh(cache *VersionCache, interval time.Duration) bool {
	if cache == nil {
		return false
	}
	return time.Since(cache.CheckedAt) < interval
}
