## MODIFIED Requirements

### Requirement: Updates come from a signed public feed

The app SHALL discover available versions by fetching a published update feed over HTTPS. The app SHALL support two feeds — a **stable** feed and a **development** feed — and SHALL use the feed for the currently selected update channel. Each feed SHALL be publicly reachable without credentials and SHALL list, for each release, the release version, the minimum supported macOS version, and a URL plus cryptographic signature for the update archive. The app SHALL NOT install an update whose archive does not match the signature published for it.

#### Scenario: A newer signed release is discovered

- **WHEN** the selected feed lists a release newer than the installed app and its archive signature matches the app's embedded public key
- **THEN** the update is offered to the user

#### Scenario: Archive signature does not match

- **WHEN** the downloaded archive's bytes do not match the signature published in the feed, or no signature is published
- **THEN** the app refuses the update and reports that verification failed, leaving the installed app untouched

#### Scenario: Feed is unreachable

- **WHEN** the selected feed cannot be fetched or is malformed
- **THEN** the check is reported as failed while the installed app continues to run

#### Scenario: Selected channel has no valid feed

- **WHEN** the selected channel's feed URL is missing or not HTTPS
- **THEN** the updater does not start and the app reports that update checks are unavailable, rather than checking a different channel's feed

### Requirement: The user controls update checks

The app SHALL perform automatic update checks by default and SHALL let the user disable them and re-enable them, persisting the preference across launches. The app SHALL also expose a manual "check for updates" action available at any time. Automatic checks SHALL run no more frequently than once per hour. Builds that are not release builds, and builds without a configured feed, SHALL NOT check for or install updates.

#### Scenario: Automatic checks enabled

- **WHEN** the user has not disabled automatic checks and a release build launches
- **THEN** the app checks the selected channel's feed without user action and surfaces an available update

#### Scenario: Automatic checks disabled

- **WHEN** the user disables automatic checks
- **THEN** the app stops checking automatically on subsequent launches until re-enabled, while the manual action still works

#### Scenario: Development build

- **WHEN** the app is a development build or has no feed configured for the selected channel
- **THEN** it performs no update checks and offers no updates

### Requirement: Update ordering uses a monotonic build number

The app SHALL compare releases by an integer build number that strictly increases for every published app release. A release whose build number is not greater than the installed build number SHALL NOT be offered as an update, regardless of its marketing version string. The build-number space SHALL be shared across channels so that a development build of the next version sorts above the current stable release and below the next stable release, allowing a machine to switch channels in either direction. The build number SHALL be encoded as `version_code * 100000 + variant`, where `version_code = major * 1000000 + minor * 1000 + patch`, `variant = 99999` for a stable release, and `variant` in `1..99998` for a development build of that version.

#### Scenario: Same marketing version, higher build number

- **WHEN** a release has the same marketing version as the installed app but a higher build number
- **THEN** it is offered as an update

#### Scenario: Development build sorts between stable releases

- **WHEN** a development build of version `V+1` is installed and the stable feed offers `V`, then later `V+1`
- **THEN** the `V` stable build is not offered (it is older) and the `V+1` stable build is offered

#### Scenario: Lower build number

- **WHEN** the feed's newest release has a build number less than or equal to the installed build number
- **THEN** no update is offered

### Requirement: Releases publish a feed entry with a stable URL

Every published Mac app release SHALL publish a DMG archive, a signature for that archive, and an updated feed entry referencing stable, permanent URLs. The app SHALL support a stable feed and a development feed, each at a permanent URL that SHALL NOT depend on a mutable "latest release" alias, so updating from any older installed version remains possible.

#### Scenario: A new app release is published

- **WHEN** the Mac release workflow runs for a tag
- **THEN** the archive, its signature, and the regenerated feed entry are published to the permanent stable feed location

#### Scenario: Updating from an old version

- **WHEN** an app several releases behind checks the feed
- **THEN** the feed still resolves the newest archive correctly, and the update succeeds without intermediate releases being reachable

## ADDED Requirements

### Requirement: The app offers a stable and a development update channel

The app SHALL offer a user-selectable update channel with at least two values, **stable** and **development**. The selected channel SHALL be persisted across launches and SHALL determine which feed the updater uses. When the user changes the channel, the app SHALL re-check the newly selected feed. The default channel for a stable build SHALL be stable; a development-channel build MAY default to development.

#### Scenario: Switch to the development channel

- **WHEN** the user selects the development channel
- **THEN** the choice is persisted and the app checks the development feed, offering a newer development build if one exists

#### Scenario: Channel persists across launches

- **WHEN** the app relaunches after a channel change
- **THEN** it checks the previously selected channel's feed without requiring the user to reselect it

#### Scenario: Switch back to stable

- **WHEN** the user selects the stable channel
- **THEN** the app checks the stable feed and offers the newest stable release newer than the installed build

### Requirement: The development channel publishes a rolling signed feed

Pushing to the default branch changes under the Mac app directory SHALL build and publish a development release without requiring a version tag. A development release SHALL be code-signed with the same Developer ID as stable releases (so Sparkle's signing-continuity check passes and update installation is atomic), SHALL use no weaker signing than an ad-hoc build, and SHALL publish a signed appcast to a rolling, permanent development feed location separate from the stable feed. Development builds MAY be published without notarization.

#### Scenario: Push to the default branch publishes the development feed

- **WHEN** a commit touching the Mac app lands on the default branch
- **THEN** a signed development build is produced and the rolling development feed is updated to reference it

#### Scenario: Development archive is signed by the same identity

- **WHEN** the app verifies a development update before installing it
- **THEN** the update's code signature satisfies the installed app's designated requirement and installation proceeds

### Requirement: Release builds run whenever the app changed

The release workflows SHALL run their build job when the corresponding app directory changed since the previous release version, and SHALL NOT be suppressed by tags that are unrelated to that app's releases (such as per-component/module tags).

#### Scenario: App directory changed since the previous version

- **WHEN** a release tag is pushed and the app directory differs from the previous release version tag
- **THEN** the build job runs

#### Scenario: Unrelated component tags present

- **WHEN** the repository contains tags for other components (for example module tags under `apps/server/pkg/sdk/`)
- **THEN** they do not cause the app's change detection to report "unchanged"

#### Scenario: App directory unchanged

- **WHEN** the app directory does not differ from the previous release version tag
- **THEN** the build job is skipped to avoid an unnecessary notarization cycle
