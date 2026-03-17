## 1. Extend Release Types and API

- [x] 1.1 Extend the `Release` struct in `tools/cli/internal/cmd/upgrade.go` to include `Body string`, `Draft bool`, and `Prerelease bool` JSON fields
- [x] 1.2 Add a `fetchReleasesBetween(currentVersion, targetVersion string) ([]Release, error)` function that calls `GET /repos/emergent-company/emergent.memory/releases?per_page=30` (up to 2 pages), filters out drafts/prereleases, and returns releases whose version falls in the range `(currentVersion, targetVersion]`
- [x] 1.3 Add a `compareVersions(a, b string) int` helper that splits version strings on `.`, compares numeric components, and returns -1/0/1

## 2. Parse Changelog from Release Body

- [x] 2.1 Add a `extractChangelog(body string) string` function that finds the `## What's Changed` section and extracts content up to the first `---` separator, returning the trimmed markdown (or empty string if not found / body empty)

## 3. Format Aggregated Changelog for Terminal

- [x] 3.1 Add a `formatChangelog(releases []Release) string` function that iterates releases (newest first), calls `extractChangelog` on each body, groups output with version headers, truncates to 10 releases max, and appends a "... and N more releases" link if truncated
- [x] 3.2 Ensure output uses simple terminal-friendly formatting: version headers as bold/underline text, categorized bullet points preserved from the release body

## 4. Integrate into CLI Upgrade Flow

- [x] 4.1 In `runUpgrade`, capture `currentVersion` and `displayLatest` before the upgrade, then after the successful `installUpdate` call, invoke `fetchReleasesBetween` and `formatChangelog`, and print the result between the success message and the "To upgrade the server" hint
- [x] 4.2 Ensure changelog fetch failure is silently swallowed (no error output, no non-zero exit)

## 5. Integrate into Server Upgrade Flow

- [x] 5.1 In `installer.Upgrade`, after the success banner, call a new exported function (or accept a changelog string parameter) to print the changelog. The `runUpgradeServer` function in `upgrade.go` should fetch and pass the changelog to `Upgrade`, or print it after `Upgrade` returns successfully.
- [x] 5.2 The server upgrade knows `currentVersion` from `inst.GetInstalledVersion()` and `targetVersion` from `release.TagName` — use these to call `fetchReleasesBetween`

## 6. Changelog Teaser for Auto-Update Notification

- [x] 6.1 Add a `SummarizeChanges(releases []Release) string` function that counts features, fixes, and other changes across all release bodies and returns a short teaser string (e.g., "3 new features, 2 fixes")
- [x] 6.2 Export `FetchReleasesBetween` and `SummarizeChanges` so the `cli-auto-update` notification can call them to build the teaser line

## 7. On-Demand Changelog Command

- [x] 7.1 Add `memory changelog` command in `internal/cmd/changelog_cmd.go` that fetches latest release, compares with current version, and displays the full aggregated changelog
- [x] 7.2 Handle edge cases: already up-to-date message, dev build fallback (show latest release only)

## 8. Testing

- [x] 8.1 Add unit tests for `compareVersions` — equal versions, ascending, descending, different-length segments
- [x] 8.2 Add unit tests for `extractChangelog` — standard body with What's Changed, body without the header, empty body
- [x] 8.3 Add unit tests for `formatChangelog` — multiple releases, single release, empty list, truncation at 10+, releases with empty changelogs skipped
- [x] 8.4 Add unit test for `fetchReleasesBetween` version filtering logic (mock HTTP or test the filter function separately)
- [x] 8.5 Add unit tests for `SummarizeChanges` — multiple categories, single category, no parseable changes
