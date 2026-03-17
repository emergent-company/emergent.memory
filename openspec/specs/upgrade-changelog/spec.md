# Spec: Upgrade Changelog

## Purpose

Display aggregated release changelogs to users after CLI and server upgrades, and on-demand via a dedicated `memory changelog` command. Changelog content is fetched from the GitHub Releases API and filtered to the version range being upgraded.

## Requirements

### Requirement: Fetch releases between versions from GitHub API
The system SHALL fetch the list of GitHub releases from the `emergent-company/emergent.memory` repository using the public Releases API and filter to releases whose version falls strictly between the user's current version and the target version (inclusive of target, exclusive of current).

#### Scenario: Successful fetch with multiple intermediate releases
- **WHEN** the user upgrades from v0.35.45 to v0.35.50
- **THEN** the system fetches releases and returns those tagged v0.35.46 through v0.35.50

#### Scenario: Single version upgrade
- **WHEN** the user upgrades from v0.35.49 to v0.35.50
- **THEN** the system returns only the v0.35.50 release

#### Scenario: Current version is "dev" or "unknown"
- **WHEN** the current version cannot be parsed (e.g., "dev", "unknown", empty)
- **THEN** the system returns only the target release changelog

#### Scenario: GitHub API failure
- **WHEN** the GitHub API returns an error (network failure, rate limit 403/429, non-200 status)
- **THEN** the system returns an empty changelog and a non-nil error
- **AND** the calling code continues without blocking the upgrade

#### Scenario: Paginated results
- **WHEN** the version gap spans more releases than a single API page (30 per page)
- **THEN** the system SHALL fetch up to 2 pages (60 releases) and stop

### Requirement: Parse changelog from release body
The system SHALL extract the "What's Changed" section from each GitHub release body by finding content between the `## What's Changed` header and the first `---` separator.

#### Scenario: Standard release body with What's Changed section
- **WHEN** a release body contains `## What's Changed` followed by categorized bullet points and a `---` separator
- **THEN** the system extracts only the categorized bullet points (Features, Bug Fixes, Other Changes)

#### Scenario: Release body without What's Changed section
- **WHEN** a release body does not contain a `## What's Changed` header
- **THEN** the system skips that release in the changelog output

#### Scenario: Release body is empty
- **WHEN** a release has an empty body
- **THEN** the system skips that release in the changelog output

### Requirement: Display aggregated changelog after CLI upgrade
The system SHALL display the aggregated changelog to stdout after a successful `memory upgrade` command, between the success message and the "To upgrade the server" hint.

#### Scenario: Changelog available with changes
- **WHEN** the CLI upgrade succeeds and changelog fetch returns entries
- **THEN** the system prints a "What's New" header followed by version-grouped changes with categorized bullet points

#### Scenario: Changelog fetch fails
- **WHEN** the CLI upgrade succeeds but changelog fetch returns an error
- **THEN** the system prints only the existing success message without any changelog or error message

#### Scenario: No changes found between versions
- **WHEN** the CLI upgrade succeeds but no parseable changelog entries exist between the versions
- **THEN** the system prints only the existing success message without a changelog section

### Requirement: Display aggregated changelog after server upgrade
The system SHALL display the aggregated changelog to stdout after a successful `memory server upgrade` command, between the success banner and the end of output.

#### Scenario: Changelog available with changes
- **WHEN** the server upgrade succeeds and changelog fetch returns entries
- **THEN** the system prints a "What's New" header followed by version-grouped changes

#### Scenario: Changelog fetch fails gracefully
- **WHEN** the server upgrade succeeds but changelog fetch returns an error
- **THEN** the upgrade completes without displaying changelog or any error

### Requirement: Truncate changelog for large version gaps
The system SHALL limit the displayed changelog to at most 10 releases. If more releases exist in the range, it SHALL show the 10 most recent and print a summary line indicating how many older releases were omitted with a link to the GitHub releases page.

#### Scenario: More than 10 releases in range
- **WHEN** upgrading across 15 versions with changelog entries
- **THEN** the system displays the 10 most recent releases' changes
- **AND** prints "... and 5 more releases. See https://github.com/emergent-company/emergent.memory/releases"

#### Scenario: Exactly 10 or fewer releases
- **WHEN** upgrading across 8 versions with changelog entries
- **THEN** the system displays all 8 releases' changes without a truncation message

### Requirement: Filter out draft and prerelease entries
The system SHALL exclude draft releases and prereleases from the changelog output.

#### Scenario: Mix of stable and prerelease versions in range
- **WHEN** the version range includes both stable releases and prereleases
- **THEN** only stable, non-draft releases appear in the changelog output

### Requirement: Changelog teaser for auto-update notification
The system SHALL provide a function that summarizes the changes between two versions into a short teaser string suitable for a single-line notification. The teaser SHALL count categorized items (features, fixes, other) from the aggregated release bodies.

#### Scenario: Multiple categories present
- **WHEN** the releases between current and latest contain 3 features, 2 bug fixes, and 1 other change
- **THEN** the teaser returns a string like "3 new features, 2 fixes"

#### Scenario: Only one category present
- **WHEN** the releases contain only bug fixes
- **THEN** the teaser returns a string like "4 fixes"

#### Scenario: No parseable changes
- **WHEN** no release bodies contain a "What's Changed" section
- **THEN** the teaser returns an empty string

### Requirement: On-demand changelog command
The system SHALL provide a `memory changelog` command that displays the full changelog between the user's current version and the latest available release without performing an upgrade.

#### Scenario: Newer version available
- **WHEN** user runs `memory changelog` and a newer version exists
- **THEN** the system fetches and displays the full aggregated changelog between current and latest, formatted with version headers and categorized bullet points

#### Scenario: Already up to date
- **WHEN** user runs `memory changelog` and current version matches the latest
- **THEN** the system prints "You are running the latest version (<version>)."

#### Scenario: Dev build
- **WHEN** user runs `memory changelog` with a dev build
- **THEN** the system fetches the latest release and displays its changelog only, with a note that version comparison is unavailable for dev builds
