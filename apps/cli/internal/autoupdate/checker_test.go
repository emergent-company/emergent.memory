package autoupdate

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// helper builds a minimal Release JSON response body.
func releaseJSON(tag string) string {
	r := Release{TagName: tag, HTMLURL: "https://github.com/example/releases/tag/" + tag}
	b, _ := json.Marshal(r)
	return string(b)
}

func TestCheckForUpdate_FreshCache(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "latest-version.json")

	// Write a fresh cache that says 0.36.0 is the latest.
	_ = SaveCache(cachePath, &VersionCache{
		Version:   "0.36.0",
		CheckedAt: time.Now(),
	})

	// No HTTP server should be contacted.
	result := CheckForUpdate("0.35.0", cachePath, 24*time.Hour, &http.Client{})

	if !result.Available {
		t.Fatal("expected Available=true from fresh cache")
	}
	if result.LatestVersion != "0.36.0" {
		t.Fatalf("expected LatestVersion=0.36.0, got %s", result.LatestVersion)
	}
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
}

func TestCheckForUpdate_StaleCache(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "latest-version.json")

	// Write a stale cache.
	_ = SaveCache(cachePath, &VersionCache{
		Version:   "0.35.0",
		CheckedAt: time.Now().Add(-48 * time.Hour),
	})

	// Serve a newer version from a mock HTTP server.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(releaseJSON("v0.36.0")))
	}))
	defer srv.Close()

	// Swap the endpoint for this test via a custom transport.
	client := srv.Client()
	result := checkForUpdateWithURL("0.35.0", cachePath, 24*time.Hour, client, srv.URL)

	if !result.Available {
		t.Fatal("expected Available=true after fetching newer release")
	}
	if result.LatestVersion != "0.36.0" {
		t.Fatalf("expected LatestVersion=0.36.0, got %s", result.LatestVersion)
	}
}

func TestCheckForUpdate_NoCache(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "latest-version.json")
	// File does not exist.

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(releaseJSON("v0.40.0")))
	}))
	defer srv.Close()

	result := checkForUpdateWithURL("0.35.0", cachePath, 24*time.Hour, srv.Client(), srv.URL)

	if !result.Available {
		t.Fatal("expected Available=true")
	}

	// Verify the cache was written.
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatal("cache file was not created")
	}
}

func TestCheckForUpdate_NetworkError(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "latest-version.json")

	// Use an unreachable URL via a custom URL override.
	result := checkForUpdateWithURL("0.35.0", cachePath, 24*time.Hour, &http.Client{Timeout: 100 * time.Millisecond}, "http://127.0.0.1:0")

	if result.Available {
		t.Fatal("should not be Available on network error")
	}
	if result.Error == nil {
		t.Fatal("expected non-nil Error on network failure")
	}
}

func TestCheckForUpdate_DevBuild(t *testing.T) {
	dir := t.TempDir()
	result := CheckForUpdate("dev", filepath.Join(dir, "v.json"), 24*time.Hour, nil)
	if result.Available {
		t.Fatal("dev build should never report an update available")
	}
}

func TestSemverGreater(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"0.36.1", "0.35.212", true},  // higher minor: IS greater
		{"0.35.212", "0.36.1", false}, // lower minor: NOT greater
		{"0.36.0", "0.35.0", true},
		{"0.35.212", "0.35.50", true},
		{"0.36.1", "0.36.1", false}, // equal: NOT greater
		{"1.0.0", "0.99.99", true},
		{"0.35.50", "0.36.0", false}, // downgrade should NOT be reported as upgrade
	}
	for _, c := range cases {
		got := semverGreater(c.a, c.b)
		if got != c.want {
			t.Errorf("semverGreater(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestCheckForUpdate_NoUpgradeWhenDowngrade(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "latest-version.json")

	// Cache says latest is 0.35.212, but current is 0.36.1 (newer).
	_ = SaveCache(cachePath, &VersionCache{
		Version:   "0.35.212",
		CheckedAt: time.Now(),
	})

	result := CheckForUpdate("0.36.1", cachePath, 24*time.Hour, &http.Client{})
	if result.Available {
		t.Fatal("should NOT report upgrade available when current version is newer than latest in cache")
	}
}

// checkForUpdateWithURL is a test-only variant that accepts an explicit API URL
// so we can point to a mock server without patching the package-level constant.
func checkForUpdateWithURL(currentVersion, cachePath string, checkInterval time.Duration, httpClient *http.Client, apiURL string) *CheckResult {
	result := &CheckResult{CurrentVersion: currentVersion}

	norm := NormalizeVersion(currentVersion)
	if norm == "" || norm == "dev" || norm == "unknown" {
		return result
	}

	cache, _ := LoadCache(cachePath)
	if IsFresh(cache, checkInterval) {
		latest := NormalizeVersion(cache.Version)
		if latest != "" && semverGreater(latest, norm) {
			result.Available = true
			result.LatestVersion = latest
			result.ReleaseBody = cache.ReleaseBody
			result.ReleaseURL = cache.ReleaseURL
		}
		return result
	}

	// Fetch from the provided URL (mock in tests).
	resp, err := httpClient.Get(apiURL)
	if err != nil {
		result.Error = err
		return result
	}
	defer resp.Body.Close()

	var release Release
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		result.Error = err
		return result
	}

	latest := NormalizeVersion(release.TagName)
	_ = SaveCache(cachePath, &VersionCache{
		Version:     latest,
		CheckedAt:   time.Now(),
		ReleaseBody: release.Body,
		ReleaseURL:  release.HTMLURL,
	})

	if latest != "" && semverGreater(latest, norm) {
		result.Available = true
		result.LatestVersion = latest
		result.ReleaseBody = release.Body
		result.ReleaseURL = release.HTMLURL
	}
	return result
}
