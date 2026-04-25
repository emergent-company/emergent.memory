package autoupdate

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const githubReleasesLatestURL = "https://api.github.com/repos/emergent-company/emergent.memory/releases/latest"

// Release mirrors the GitHub release payload fields that the auto-update
// logic needs.  It is kept intentionally minimal.
type Release struct {
	TagName    string  `json:"tag_name"`
	Body       string  `json:"body"`
	HTMLURL    string  `json:"html_url"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Assets     []Asset `json:"assets"`
}

// Asset is a file attached to a GitHub release.
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// CheckResult is returned by CheckForUpdate.
type CheckResult struct {
	Available      bool
	LatestVersion  string
	CurrentVersion string
	ReleaseBody    string
	ReleaseURL     string
	Error          error
}

// FetchLatestRelease calls the GitHub API and returns the latest non-draft,
// non-prerelease release.  It uses the provided httpClient so callers (and
// tests) can inject a mock transport.
func FetchLatestRelease(httpClient *http.Client) (*Release, error) {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}

	resp, err := httpClient.Get(githubReleasesLatestURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status: %d", resp.StatusCode)
	}

	var r Release
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, err
	}
	return &r, nil
}

// NormalizeVersion strips "v" and "cli-" prefixes from a version tag so
// comparisons work on bare semver strings like "0.35.50".
func NormalizeVersion(v string) string {
	v = strings.TrimPrefix(v, "cli-")
	v = strings.TrimPrefix(v, "v")
	return v
}

// semverGreater returns true if version a is strictly greater than version b.
// Both a and b should be normalized bare semver strings like "0.35.212".
// Non-numeric segments are compared lexicographically as a fallback.
func semverGreater(a, b string) bool {
	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")
	n := len(aParts)
	if len(bParts) > n {
		n = len(bParts)
	}
	for i := 0; i < n; i++ {
		var ap, bp string
		if i < len(aParts) {
			ap = aParts[i]
		}
		if i < len(bParts) {
			bp = bParts[i]
		}
		ai, aerr := strconv.Atoi(ap)
		bi, berr := strconv.Atoi(bp)
		if aerr == nil && berr == nil {
			if ai != bi {
				return ai > bi
			}
		} else {
			if ap != bp {
				return ap > bp
			}
		}
	}
	return false
}

// CheckForUpdate checks whether a newer version is available.
//
// It first consults the on-disk cache at cachePath; if the cache is still
// fresh (within checkInterval) the cached result is returned immediately with
// no network call.  Otherwise a GitHub API call is made and the cache is
// updated.
//
// currentVersion should be the bare semver string (e.g. "0.35.50").  "dev"
// and empty strings are treated as "always up to date" — the function returns
// Available=false without touching the cache or network.
func CheckForUpdate(currentVersion string, cachePath string, checkInterval time.Duration, httpClient *http.Client) *CheckResult {
	result := &CheckResult{CurrentVersion: currentVersion}

	// Dev builds never have an update available.
	norm := NormalizeVersion(currentVersion)
	if norm == "" || norm == "dev" || norm == "unknown" {
		return result
	}

	// Try the cache first.
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

	// Cache is stale or missing — fetch from GitHub.
	release, err := FetchLatestRelease(httpClient)
	if err != nil {
		result.Error = err
		return result
	}

	latest := NormalizeVersion(release.TagName)

	// Update the cache (best-effort; errors are silently swallowed so a
	// cache-write failure never breaks the command).
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
