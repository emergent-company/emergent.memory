## MODIFIED Requirements

### Requirement: Updates come from a signed public feed

The app SHALL discover available versions by fetching a published update feed over HTTPS. The feed SHALL be hosted on a **public static host** and SHALL be reachable **without credentials** — it SHALL NOT be a GitHub release asset of a private repository, because those are not anonymously fetchable. The app SHALL support two feeds — a **stable** feed and a **development** feed — and SHALL use the feed for the currently selected update channel. Each feed SHALL list, for each release, the release version, the minimum supported macOS version, and a URL plus cryptographic signature for the update archive, and those archive URLs SHALL also be publicly reachable without credentials. The app SHALL NOT install an update whose archive does not match the signature published for it.

#### Scenario: A newer signed release is discovered

- **WHEN** the selected feed lists a release newer than the installed app and its archive signature matches the app's embedded public key
- **THEN** the update is offered to the user

#### Scenario: Feed and archive are publicly reachable

- **WHEN** the app fetches the selected feed and the referenced archive over HTTPS with no credentials
- **THEN** both requests succeed (HTTP 200), not a private-resource 404

#### Scenario: Archive signature does not match

- **WHEN** the downloaded archive's bytes do not match the signature published in the feed, or no signature is published
- **THEN** the app refuses the update and reports that verification failed, leaving the installed app untouched

#### Scenario: Feed is unreachable

- **WHEN** the selected feed cannot be fetched or is malformed
- **THEN** the check is reported as failed while the installed app continues to run

#### Scenario: Selected channel has no valid feed

- **WHEN** the selected channel's feed URL is missing or not HTTPS
- **THEN** the updater does not start and the app reports that update checks are unavailable, rather than checking a different channel's feed

### Requirement: Releases publish a feed entry with a stable URL

Every published Mac app release SHALL publish a DMG archive, a signature for that archive, and an updated feed entry referencing stable, permanent, publicly reachable URLs. The app SHALL support a stable feed and a development feed, each at a permanent URL on the public static host that SHALL NOT depend on a mutable "latest release" alias, so updating from any older installed version remains possible.

#### Scenario: A new app release is published

- **WHEN** the Mac release workflow runs for a tag
- **THEN** the archive, its signature, and the regenerated feed entry are published to the permanent public stable feed location

#### Scenario: Updating from an old version

- **WHEN** an app several releases behind checks the feed
- **THEN** the feed still resolves the newest archive correctly, and the update succeeds without intermediate releases being reachable
