package upgrade

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// releaseTagsJSON builds a GitHub releases-list payload from tag names.
func releaseTagsJSON(tags []string) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, tag := range tags {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"tag_name":%q}`, tag)
	}
	b.WriteByte(']')
	return b.String()
}

// newReleasesServer serves the releases endpoint for the given tags and counts
// requests so tests can assert a cache hit made no network call.
func newReleasesServer(t *testing.T, tags ...string) (*httptest.Server, *int32) {
	t.Helper()
	var hits int32
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/"+Repo+"/releases", func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write([]byte(releaseTagsJSON(tags)))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &hits
}

func checkClient(srv *httptest.Server) *Client {
	return &Client{HTTP: srv.Client(), APIBase: srv.URL}
}

func TestCheckForUpdateFreshCacheSkipsNetwork(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "latest-version.json")
	if err := SaveCache(cachePath, &VersionCache{Version: "0.36.0", CheckedAt: time.Now()}); err != nil {
		t.Fatalf("SaveCache: %v", err)
	}

	srv, hits := newReleasesServer(t, "v0.36.0")
	res, err := CheckForUpdate(context.Background(), checkClient(srv), "0.35.0", cachePath, 24*time.Hour)
	if err != nil {
		t.Fatalf("CheckForUpdate: %v", err)
	}
	if !res.Available {
		t.Error("Available = false, want true")
	}
	if res.Latest != "0.36.0" {
		t.Errorf("Latest = %q, want 0.36.0", res.Latest)
	}
	if got := atomic.LoadInt32(hits); got != 0 {
		t.Errorf("API hits = %d, want 0 (fresh cache must not fetch)", got)
	}
}

func TestCheckForUpdateStaleCacheFetches(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "latest-version.json")
	if err := SaveCache(cachePath, &VersionCache{Version: "0.35.0", CheckedAt: time.Now().Add(-48 * time.Hour)}); err != nil {
		t.Fatalf("SaveCache: %v", err)
	}

	srv, hits := newReleasesServer(t, "v0.36.0")
	res, err := CheckForUpdate(context.Background(), checkClient(srv), "0.35.0", cachePath, 24*time.Hour)
	if err != nil {
		t.Fatalf("CheckForUpdate: %v", err)
	}
	if !res.Available {
		t.Error("Available = false, want true")
	}
	if res.Latest != "0.36.0" {
		t.Errorf("Latest = %q, want 0.36.0", res.Latest)
	}
	if got := atomic.LoadInt32(hits); got != 1 {
		t.Errorf("API hits = %d, want 1 (stale cache must fetch)", got)
	}

	// The cache must have been rewritten with the freshly fetched version.
	got, err := LoadCache(cachePath)
	if err != nil {
		t.Fatalf("LoadCache: %v", err)
	}
	if got == nil || got.Version != "0.36.0" {
		t.Errorf("cache version = %+v, want 0.36.0", got)
	}
}

func TestCheckForUpdateUpToDate(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "latest-version.json")

	srv, _ := newReleasesServer(t, "v0.34.0")
	res, err := CheckForUpdate(context.Background(), checkClient(srv), "0.35.0", cachePath, 24*time.Hour)
	if err != nil {
		t.Fatalf("CheckForUpdate: %v", err)
	}
	if res.Available {
		t.Error("Available = true, want false (current is newer)")
	}
}

func TestCheckForUpdateDevVersionSkips(t *testing.T) {
	for _, current := range []string{"dev", "unknown", ""} {
		t.Run(current, func(t *testing.T) {
			dir := t.TempDir()
			cachePath := filepath.Join(dir, "latest-version.json")
			srv, hits := newReleasesServer(t, "v0.36.0")

			res, err := CheckForUpdate(context.Background(), checkClient(srv), current, cachePath, 24*time.Hour)
			if err != nil {
				t.Fatalf("CheckForUpdate(%q): %v", current, err)
			}
			if res.Available {
				t.Errorf("Available = true for %q, want false", current)
			}
			if got := atomic.LoadInt32(hits); got != 0 {
				t.Errorf("API hits = %d for %q, want 0", got, current)
			}
			if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
				t.Errorf("cache file exists for %q, want none", current)
			}
		})
	}
}

func TestCheckForUpdateNetworkError(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "latest-version.json")

	client := &Client{HTTP: &http.Client{Timeout: 100 * time.Millisecond}, APIBase: "http://127.0.0.1:0"}
	if _, err := CheckForUpdate(context.Background(), client, "0.35.0", cachePath, 24*time.Hour); err == nil {
		t.Fatal("CheckForUpdate = nil error, want network error")
	}
}
