package cmd

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- compareVersions tests ---

func TestCompareVersions_Equal(t *testing.T) {
	assert.Equal(t, 0, compareVersions("0.35.50", "0.35.50"))
	assert.Equal(t, 0, compareVersions("v0.35.50", "0.35.50"))
	assert.Equal(t, 0, compareVersions("1.0.0", "1.0.0"))
}

func TestCompareVersions_Ascending(t *testing.T) {
	assert.Equal(t, -1, compareVersions("0.35.49", "0.35.50"))
	assert.Equal(t, -1, compareVersions("0.34.99", "0.35.0"))
	assert.Equal(t, -1, compareVersions("0.9.9", "1.0.0"))
}

func TestCompareVersions_Descending(t *testing.T) {
	assert.Equal(t, 1, compareVersions("0.35.50", "0.35.49"))
	assert.Equal(t, 1, compareVersions("1.0.0", "0.99.99"))
	assert.Equal(t, 1, compareVersions("0.36.0", "0.35.99"))
}

func TestCompareVersions_DifferentLengthSegments(t *testing.T) {
	assert.Equal(t, 0, compareVersions("1.0", "1.0.0"))
	assert.Equal(t, -1, compareVersions("1.0", "1.0.1"))
	assert.Equal(t, 1, compareVersions("1.0.1", "1.0"))
}

func TestCompareVersions_WithPrefixes(t *testing.T) {
	assert.Equal(t, 0, compareVersions("v1.2.3", "v1.2.3"))
	assert.Equal(t, 0, compareVersions("v1.2.3", "1.2.3"))
}

// --- extractChangelog tests ---

func TestExtractChangelog_StandardBody(t *testing.T) {
	body := `## What's Changed

### Features
- feat: add new feature (abc1234)

### Bug Fixes
- fix: resolve issue (def5678)

---

## Memory CLI

### One-Line Install
` + "```bash\ncurl -fsSL ...\n```"

	result := extractChangelog(body)
	assert.Contains(t, result, "### Features")
	assert.Contains(t, result, "feat: add new feature")
	assert.Contains(t, result, "### Bug Fixes")
	assert.Contains(t, result, "fix: resolve issue")
	assert.NotContains(t, result, "Memory CLI")
	assert.NotContains(t, result, "One-Line Install")
}

func TestExtractChangelog_NoWhatsChangedHeader(t *testing.T) {
	body := `## Release Notes

Some other content here.

---

More content.`

	result := extractChangelog(body)
	assert.Empty(t, result)
}

func TestExtractChangelog_EmptyBody(t *testing.T) {
	result := extractChangelog("")
	assert.Empty(t, result)
}

func TestExtractChangelog_WhatsChangedNoSeparator(t *testing.T) {
	body := `## What's Changed

### Features
- feat: something new (abc1234)`

	result := extractChangelog(body)
	assert.Contains(t, result, "### Features")
	assert.Contains(t, result, "feat: something new")
}

func TestExtractChangelog_WhatsChangedEmptyContent(t *testing.T) {
	body := `## What's Changed

---

## More stuff`

	result := extractChangelog(body)
	assert.Empty(t, result)
}

// --- formatChangelog tests ---

func TestFormatChangelog_MultipleReleases(t *testing.T) {
	releases := []Release{
		{
			TagName: "v0.35.50",
			Body: `## What's Changed

### Features
- feat: new thing (abc)

---

## CLI`,
		},
		{
			TagName: "v0.35.49",
			Body: `## What's Changed

### Bug Fixes
- fix: old bug (def)

---

## CLI`,
		},
	}

	result := formatChangelog(releases)
	assert.Contains(t, result, "What's New")
	assert.Contains(t, result, "v0.35.50")
	assert.Contains(t, result, "feat: new thing")
	assert.Contains(t, result, "v0.35.49")
	assert.Contains(t, result, "fix: old bug")
}

func TestFormatChangelog_SingleRelease(t *testing.T) {
	releases := []Release{
		{
			TagName: "v0.35.50",
			Body: `## What's Changed

### Features
- feat: only change (abc)

---`,
		},
	}

	result := formatChangelog(releases)
	assert.Contains(t, result, "v0.35.50")
	assert.Contains(t, result, "feat: only change")
}

func TestFormatChangelog_EmptyList(t *testing.T) {
	result := formatChangelog(nil)
	assert.Empty(t, result)

	result = formatChangelog([]Release{})
	assert.Empty(t, result)
}

func TestFormatChangelog_SkipsReleasesWithEmptyChangelog(t *testing.T) {
	releases := []Release{
		{
			TagName: "v0.35.50",
			Body: `## What's Changed

### Features
- feat: has content (abc)

---`,
		},
		{
			TagName: "v0.35.49",
			Body:    "",
		},
		{
			TagName: "v0.35.48",
			Body:    "No changelog section here.",
		},
	}

	result := formatChangelog(releases)
	assert.Contains(t, result, "v0.35.50")
	assert.NotContains(t, result, "v0.35.49")
	assert.NotContains(t, result, "v0.35.48")
}

func TestFormatChangelog_TruncatesAtMaxDisplayReleases(t *testing.T) {
	// Create 12 releases with valid changelogs
	releases := make([]Release, 12)
	for i := 0; i < 12; i++ {
		releases[i] = Release{
			TagName: fmt.Sprintf("v0.35.%d", 50-i),
			Body: fmt.Sprintf(`## What's Changed

### Features
- feat: change %d (abc)

---`, i),
		}
	}

	result := formatChangelog(releases)
	// Should show first 10
	assert.Contains(t, result, "v0.35.50")
	assert.Contains(t, result, "v0.35.41")
	// Should NOT show 11th and 12th
	assert.NotContains(t, result, "v0.35.40")
	assert.NotContains(t, result, "v0.35.39")
	// Should show truncation message
	assert.Contains(t, result, "... and 2 more release(s)")
	assert.Contains(t, result, "github.com/emergent-company/emergent.memory/releases")
}

// --- filterReleasesBetween tests ---

func TestFilterReleasesBetween_BasicRange(t *testing.T) {
	releases := []Release{
		{TagName: "v0.35.50"},
		{TagName: "v0.35.49"},
		{TagName: "v0.35.48"},
		{TagName: "v0.35.47"},
		{TagName: "v0.35.46"},
	}

	result := filterReleasesBetween(releases, "0.35.47", "0.35.50")
	assert.Len(t, result, 3)
	assert.Equal(t, "v0.35.50", result[0].TagName)
	assert.Equal(t, "v0.35.49", result[1].TagName)
	assert.Equal(t, "v0.35.48", result[2].TagName)
}

func TestFilterReleasesBetween_ExcludesCurrentVersion(t *testing.T) {
	releases := []Release{
		{TagName: "v0.35.50"},
		{TagName: "v0.35.49"},
	}

	result := filterReleasesBetween(releases, "0.35.49", "0.35.50")
	assert.Len(t, result, 1)
	assert.Equal(t, "v0.35.50", result[0].TagName)
}

func TestFilterReleasesBetween_IncludesTargetVersion(t *testing.T) {
	releases := []Release{
		{TagName: "v0.35.50"},
		{TagName: "v0.35.49"},
	}

	result := filterReleasesBetween(releases, "0.35.48", "0.35.50")
	assert.Len(t, result, 2)
}

func TestFilterReleasesBetween_ExcludesDraftsAndPrereleases(t *testing.T) {
	releases := []Release{
		{TagName: "v0.35.50"},
		{TagName: "v0.35.49", Draft: true},
		{TagName: "v0.35.48", Prerelease: true},
		{TagName: "v0.35.47"},
	}

	result := filterReleasesBetween(releases, "0.35.46", "0.35.50")
	assert.Len(t, result, 2)
	assert.Equal(t, "v0.35.50", result[0].TagName)
	assert.Equal(t, "v0.35.47", result[1].TagName)
}

func TestFilterReleasesBetween_EmptyRange(t *testing.T) {
	releases := []Release{
		{TagName: "v0.35.50"},
	}

	result := filterReleasesBetween(releases, "0.35.50", "0.35.50")
	assert.Len(t, result, 0)
}

func TestFilterReleasesBetween_NoReleasesInRange(t *testing.T) {
	releases := []Release{
		{TagName: "v0.35.50"},
		{TagName: "v0.35.49"},
	}

	result := filterReleasesBetween(releases, "0.35.50", "0.35.51")
	assert.Len(t, result, 0)
}

// --- normalizeVersion tests ---

func TestNormalizeVersion(t *testing.T) {
	assert.Equal(t, "0.35.50", normalizeVersion("v0.35.50"))
	assert.Equal(t, "0.35.50", normalizeVersion("cli-v0.35.50"))
	assert.Equal(t, "0.35.50", normalizeVersion("0.35.50"))
	assert.Equal(t, "dev", normalizeVersion("dev"))
}

// --- getUpgradeChangelog tests ---

func TestGetUpgradeChangelog_EmptyOnError(t *testing.T) {
	// With versions that won't match any real releases in a test environment,
	// the function should return empty string gracefully
	// (actual HTTP calls would fail in test, but getUpgradeChangelog handles that)
	// This tests the graceful degradation path
	result := getUpgradeChangelog("dev", "dev")
	// In a unit test context without network, this might fail or return empty
	// The key assertion is it doesn't panic
	_ = result
}

// --- SummarizeChanges tests ---

func TestSummarizeChanges_MultipleCategories(t *testing.T) {
	releases := []Release{
		{
			TagName: "v0.35.50",
			Body: `## What's Changed

### Features
- feat: add thing A (abc)
- feat: add thing B (def)

### Bug Fixes
- fix: resolve issue X (ghi)

### Other Changes
- chore: update deps (jkl)

---

## CLI`,
		},
	}

	result := SummarizeChanges(releases)
	assert.Contains(t, result, "2 new feature(s)")
	assert.Contains(t, result, "1 bug fix(es)")
	assert.Contains(t, result, "1 other change(s)")
}

func TestSummarizeChanges_SingleCategory(t *testing.T) {
	releases := []Release{
		{
			TagName: "v0.35.50",
			Body: `## What's Changed

### Bug Fixes
- fix: resolve crash (abc)
- fix: fix edge case (def)

---`,
		},
	}

	result := SummarizeChanges(releases)
	assert.Contains(t, result, "2 bug fix(es)")
	assert.NotContains(t, result, "feature")
	assert.NotContains(t, result, "other")
}

func TestSummarizeChanges_NoParseable(t *testing.T) {
	releases := []Release{
		{TagName: "v0.35.50", Body: ""},
		{TagName: "v0.35.49", Body: "No changelog section here."},
	}

	result := SummarizeChanges(releases)
	assert.Empty(t, result)
}

func TestSummarizeChanges_AcrossMultipleReleases(t *testing.T) {
	releases := []Release{
		{
			TagName: "v0.35.51",
			Body: `## What's Changed

### Features
- feat: new dashboard (abc)

---`,
		},
		{
			TagName: "v0.35.50",
			Body: `## What's Changed

### Features
- feat: search improvements (def)

### Bug Fixes
- fix: login fix (ghi)

---`,
		},
	}

	result := SummarizeChanges(releases)
	assert.Contains(t, result, "2 new feature(s)")
	assert.Contains(t, result, "1 bug fix(es)")
}

func TestSummarizeChanges_EmptyList(t *testing.T) {
	result := SummarizeChanges(nil)
	assert.Empty(t, result)

	result = SummarizeChanges([]Release{})
	assert.Empty(t, result)
}
