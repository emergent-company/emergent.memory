package upgrade

import (
	"context"
	"fmt"
	"time"
)

// Result reports the outcome of an automatic update check.
type Result struct {
	// Current is the normalized current version.
	Current string
	// Latest is the newest release version (empty when unknown).
	Latest string
	// Available is true when Latest is newer than Current.
	Available bool
}

// CheckForUpdate resolves the newest release and compares it with current,
// caching the verdict on disk. A fresh cache short-circuits the network call;
// otherwise the release list is fetched, the cache is saved, and the versions
// are compared with Compare.
//
// A current version that normalizes to "dev", "unknown", or empty is skipped
// entirely (no cache read and no network call): dev builds never have an
// update available. A network error is returned as error and never aborts the
// caller; a cache-write failure is silently ignored.
func CheckForUpdate(ctx context.Context, client *Client, current, cachePath string, interval time.Duration) (*Result, error) {
	res := &Result{Current: Normalize(current)}
	if res.Current == "" || res.Current == "dev" || res.Current == "unknown" {
		return res, nil
	}

	cache, _ := LoadCache(cachePath)
	if IsFresh(cache, interval) {
		res.Latest = Normalize(cache.Version)
		res.Available = Compare(res.Latest, res.Current) > 0
		return res, nil
	}

	rel, err := client.Latest(ctx)
	if err != nil {
		return nil, fmt.Errorf("check for update: %w", err)
	}
	latest := Normalize(rel.Version)

	// Best-effort cache write; a cache-write failure must never break the check.
	_ = SaveCache(cachePath, &VersionCache{Version: latest, CheckedAt: time.Now()})

	res.Latest = latest
	res.Available = Compare(latest, res.Current) > 0
	return res, nil
}
