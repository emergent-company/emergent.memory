package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

const (
	releasesURL        = "https://api.github.com/repos/emergent-company/emergent.memory/releases?per_page=30"
	releasesPageURL    = "https://github.com/emergent-company/emergent.memory/releases"
	maxDisplayReleases = 10
	maxPages           = 2
)

// compareVersions compares two semver-style version strings (e.g. "0.35.50").
// Returns -1 if a < b, 0 if a == b, 1 if a > b.
// Strips leading "v" prefix if present.
func compareVersions(a, b string) int {
	a = strings.TrimPrefix(a, "v")
	b = strings.TrimPrefix(b, "v")

	partsA := strings.Split(a, ".")
	partsB := strings.Split(b, ".")

	maxLen := len(partsA)
	if len(partsB) > maxLen {
		maxLen = len(partsB)
	}

	for i := 0; i < maxLen; i++ {
		var numA, numB int
		if i < len(partsA) {
			numA, _ = strconv.Atoi(partsA[i])
		}
		if i < len(partsB) {
			numB, _ = strconv.Atoi(partsB[i])
		}
		if numA < numB {
			return -1
		}
		if numA > numB {
			return 1
		}
	}
	return 0
}

// fetchReleasesBetween fetches GitHub releases whose version falls in the
// range (currentVersion, targetVersion]. Drafts and prereleases are excluded.
// Returns releases sorted newest-first (as returned by the API).
// On any error, returns nil and the error — callers should treat this as best-effort.
func fetchReleasesBetween(currentVersion, targetVersion string) ([]Release, error) {
	currentVersion = normalizeVersion(currentVersion)
	targetVersion = normalizeVersion(targetVersion)

	// If current version is unparseable, just return the target release info
	if currentVersion == "" || currentVersion == "dev" || currentVersion == "unknown" {
		currentVersion = "0.0.0"
	}

	var allReleases []Release

	for page := 1; page <= maxPages; page++ {
		url := fmt.Sprintf("%s&page=%d", releasesURL, page)
		resp, err := http.Get(url)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch releases: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
		}

		var releases []Release
		if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
			return nil, fmt.Errorf("failed to decode releases: %w", err)
		}

		if len(releases) == 0 {
			break
		}

		allReleases = append(allReleases, releases...)

		// If the oldest release on this page is already older than currentVersion,
		// no need to fetch more pages
		oldest := releases[len(releases)-1]
		oldestVersion := normalizeVersion(oldest.TagName)
		if compareVersions(oldestVersion, currentVersion) <= 0 {
			break
		}
	}

	return filterReleasesBetween(allReleases, currentVersion, targetVersion), nil
}

// filterReleasesBetween filters releases to those in range (currentVersion, targetVersion].
// Excludes drafts and prereleases.
func filterReleasesBetween(releases []Release, currentVersion, targetVersion string) []Release {
	var filtered []Release
	for _, r := range releases {
		if r.Draft || r.Prerelease {
			continue
		}
		v := normalizeVersion(r.TagName)
		if v == "" {
			continue
		}
		// Include if: currentVersion < v <= targetVersion
		if compareVersions(v, currentVersion) > 0 && compareVersions(v, targetVersion) <= 0 {
			filtered = append(filtered, r)
		}
	}
	return filtered
}

// normalizeVersion strips "v" and "cli-" prefixes from a version string.
func normalizeVersion(v string) string {
	v = strings.TrimPrefix(v, "cli-")
	v = strings.TrimPrefix(v, "v")
	return v
}

// extractChangelog extracts the "What's Changed" section from a GitHub release body.
// Returns the content between "## What's Changed" and the first "---" separator.
// Returns empty string if the section is not found or body is empty.
func extractChangelog(body string) string {
	if body == "" {
		return ""
	}

	// Find the "What's Changed" header
	idx := strings.Index(body, "## What's Changed")
	if idx == -1 {
		return ""
	}

	// Get content after the header line
	content := body[idx:]
	// Skip past the header line itself
	if nlIdx := strings.Index(content, "\n"); nlIdx != -1 {
		content = content[nlIdx+1:]
	} else {
		return ""
	}

	// Find the first "---" separator
	if sepIdx := strings.Index(content, "---"); sepIdx != -1 {
		content = content[:sepIdx]
	}

	return strings.TrimSpace(content)
}

// formatChangelog formats an aggregated changelog from multiple releases for terminal display.
// Releases should be sorted newest-first. Output is truncated to maxDisplayReleases.
func formatChangelog(releases []Release) string {
	if len(releases) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	sb.WriteString("  What's New\n")
	sb.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")

	displayed := 0
	skippedWithContent := 0

	for _, r := range releases {
		changelog := extractChangelog(r.Body)
		if changelog == "" {
			continue
		}

		if displayed >= maxDisplayReleases {
			skippedWithContent++
			continue
		}

		version := normalizeVersion(r.TagName)
		sb.WriteString(fmt.Sprintf("\n  v%s\n", version))
		// Indent the changelog content
		for _, line := range strings.Split(changelog, "\n") {
			if line == "" {
				continue
			}
			sb.WriteString(fmt.Sprintf("  %s\n", line))
		}
		displayed++
	}

	if skippedWithContent > 0 {
		sb.WriteString(fmt.Sprintf("\n  ... and %d more release(s). See %s\n", skippedWithContent, releasesPageURL))
	}

	if displayed == 0 {
		return ""
	}

	sb.WriteString("\n")
	return sb.String()
}

// getUpgradeChangelog fetches and formats the changelog between two versions.
// Returns the formatted string, or empty string on any error. Never fails loudly.
func getUpgradeChangelog(currentVersion, targetVersion string) string {
	releases, err := fetchReleasesBetween(currentVersion, targetVersion)
	if err != nil {
		return ""
	}
	return formatChangelog(releases)
}

// FetchReleasesBetween is the exported version of fetchReleasesBetween.
// It fetches GitHub releases in the range (currentVersion, targetVersion].
func FetchReleasesBetween(currentVersion, targetVersion string) ([]Release, error) {
	return fetchReleasesBetween(currentVersion, targetVersion)
}

// SummarizeChanges counts features, fixes, and other changes across all release
// bodies and returns a short teaser string (e.g., "3 new features, 2 bug fixes").
// Returns empty string if no parseable changes are found.
func SummarizeChanges(releases []Release) string {
	var features, fixes, other int

	for _, r := range releases {
		changelog := extractChangelog(r.Body)
		if changelog == "" {
			continue
		}

		inSection := ""
		for _, line := range strings.Split(changelog, "\n") {
			trimmed := strings.TrimSpace(line)
			lower := strings.ToLower(trimmed)

			if strings.HasPrefix(trimmed, "### ") {
				if strings.Contains(lower, "feature") {
					inSection = "features"
				} else if strings.Contains(lower, "fix") {
					inSection = "fixes"
				} else {
					inSection = "other"
				}
				continue
			}

			if strings.HasPrefix(trimmed, "- ") {
				switch inSection {
				case "features":
					features++
				case "fixes":
					fixes++
				default:
					other++
				}
			}
		}
	}

	if features+fixes+other == 0 {
		return ""
	}

	var parts []string
	if features > 0 {
		parts = append(parts, fmt.Sprintf("%d new feature(s)", features))
	}
	if fixes > 0 {
		parts = append(parts, fmt.Sprintf("%d bug fix(es)", fixes))
	}
	if other > 0 {
		parts = append(parts, fmt.Sprintf("%d other change(s)", other))
	}
	return strings.Join(parts, ", ")
}
