## Why

After running `memory upgrade` or `memory server upgrade`, users see a success banner but have no idea what changed. They have to manually visit GitHub to find release notes. Since we already fetch release metadata from the GitHub API, we should also fetch and display an aggregated changelog covering all versions between the user's current version and the version they just upgraded to.

## What Changes

- Extend the GitHub `Release` struct to include `Body` (release notes markdown) and `TagName` for each release
- Add a new function to fetch multiple releases from the GitHub API (`/repos/.../releases`) and filter to the range between the old and new version
- Parse and aggregate the "What's Changed" sections from each release's body into a concise summary
- Display the aggregated changelog after both `memory upgrade` (CLI) and `memory server upgrade` (server) complete successfully
- Keep output concise: show categorized bullet points (features, fixes, other) with version headers, truncate if too many entries

## Capabilities

### New Capabilities
- `upgrade-changelog`: Fetch, aggregate, and display changelog from GitHub releases between the user's current version and the target version after a successful upgrade

### Modified Capabilities

## Impact

- **Code**: `tools/cli/internal/cmd/upgrade.go` — new release-fetching logic, changelog aggregation, display in both `runUpgrade` and `runUpgradeServer`
- **APIs**: Additional GitHub API calls to `/repos/emergent-company/emergent.memory/releases` (paginated list endpoint, unauthenticated)
- **Dependencies**: No new dependencies — uses existing `net/http` and `encoding/json`
- **Risk**: GitHub API rate limits for unauthenticated requests (60/hour) — changelog fetch is best-effort, failure should not block the upgrade
- **Integration**: `cli-auto-update` — the auto-update notification reuses the changelog teaser formatter from this change to show a summary in the update notice; the `memory changelog` on-demand command also uses the same fetch/format functions
