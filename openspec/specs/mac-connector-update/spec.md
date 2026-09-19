# mac-connector-update Specification

## Purpose
Keeps the Mac connector app current without the user re-downloading a DMG, by discovering releases from a signed public update feed, verifying their authenticity and code-signing continuity before installing, and installing them atomically while preserving the app's macOS permission grants.

## Requirements

### Requirement: Updates come from a signed public feed

The app SHALL discover available versions by fetching a published update feed over HTTPS. The feed SHALL be publicly reachable without credentials and SHALL list, for each release, the release version, the minimum supported macOS version, and a URL plus cryptographic signature for the update archive. The app SHALL NOT install an update whose archive does not match the signature published for it.

#### Scenario: A newer signed release is discovered

- **WHEN** the feed lists a release newer than the installed app and its archive signature matches the app's embedded public key
- **THEN** the update is offered to the user

#### Scenario: Archive signature does not match

- **WHEN** the downloaded archive's bytes do not match the signature published in the feed, or no signature is published
- **THEN** the app refuses the update and reports that verification failed, leaving the installed app untouched

#### Scenario: Feed is unreachable

- **WHEN** the feed cannot be fetched or is malformed
- **THEN** the app reports no update rather than failing, and the installed app continues to run

### Requirement: The installed app must be code-signed by the same identity

The app SHALL verify that an update's application bundle carries Apple code signatures satisfying the installed app's designated requirement before installing it. An update signed by a different identity SHALL be refused.

#### Scenario: Update signed by the same Developer ID

- **WHEN** the update bundle is signed by the same Developer ID as the installed app
- **THEN** installation proceeds

#### Scenario: Update signed by a different identity

- **WHEN** the update bundle is unsigned, ad-hoc signed, or signed by a different team or certificate
- **THEN** the app refuses the update and reports a signing mismatch

### Requirement: Update ordering uses a monotonic build number

The app SHALL compare releases by an integer build number that strictly increases for every published app release. A release whose build number is not greater than the installed build number SHALL NOT be offered as an update, regardless of its marketing version string.

#### Scenario: Same marketing version, higher build number

- **WHEN** a release has the same marketing version as the installed app but a higher build number
- **THEN** it is offered as an update

#### Scenario: Lower build number

- **WHEN** the feed's newest release has a build number less than or equal to the installed build number
- **THEN** no update is offered

### Requirement: The user controls update checks

The app SHALL perform automatic update checks by default and SHALL let the user disable them and re-enable them, persisting the preference across launches. The app SHALL also expose a manual "check for updates" action available at any time. Automatic checks SHALL run no more frequently than once per hour. Builds that are not release builds, and builds without a configured feed, SHALL NOT check for or install updates.

#### Scenario: Automatic checks enabled

- **WHEN** the user has not disabled automatic checks and a release build launches
- **THEN** the app checks the feed without user action and surfaces an available update

#### Scenario: Automatic checks disabled

- **WHEN** the user disables automatic checks
- **THEN** the app stops checking automatically on subsequent launches until re-enabled, while the manual action still works

#### Scenario: Development build

- **WHEN** the app is a development build or has no feed configured
- **THEN** it performs no update checks and offers no updates

### Requirement: Installing an update is atomic and preserves permissions

Installing an update SHALL replace the installed application bundle in place and relaunch the app. If any step fails, the previously installed app SHALL remain runnable and the failure SHALL be reported to the user. The update SHALL NOT require the user to re-grant macOS Automation permissions, because the installed bundle keeps the same bundle identifier and signing identity before and after the update.

#### Scenario: Successful update

- **WHEN** a verified update is installed
- **THEN** the app is replaced at its installation location, relaunches on the new version, and previously granted Automation permissions still apply

#### Scenario: Installation fails

- **WHEN** downloading, verifying, or installing the update fails
- **THEN** the app reports the failure and the previously installed version still launches

### Requirement: Releases publish a feed entry with a stable URL

Every published Mac app release SHALL publish a DMG archive, a signature for that archive, and an updated feed entry referencing stable, permanent URLs. The feed URL itself SHALL be permanent and SHALL NOT depend on a mutable "latest release" alias, so updating from any older installed version remains possible.

#### Scenario: A new app release is published

- **WHEN** the Mac release workflow runs for a tag
- **THEN** the archive, its signature, and the regenerated feed entry are published to the permanent feed location

#### Scenario: Updating from an old version

- **WHEN** an app several releases behind checks the feed
- **THEN** the feed still resolves the newest archive correctly, and the update succeeds without intermediate releases being reachable

### Requirement: Update state is visible in the app

The app SHALL expose its current version and the outcome of the most recent update check (up to date, update available, checking, installing, or failed) in the app's UI.

#### Scenario: Update available

- **WHEN** a check finds an update
- **THEN** the app shows the current and available versions and offers to install

#### Scenario: Check failed

- **WHEN** the most recent check failed
- **THEN** the app shows that the check failed rather than appearing up to date
