## ADDED Requirements

### Requirement: Canonical core version source

The repository SHALL treat the root `VERSION` file as the single canonical
release version. The server OpenAPI annotation (`// @version` in
`apps/server/cmd/server/main.go`) and every app marketing version checked into
the repository — currently the iOS `MARKETING_VERSION` (app and broadcast
extension) — SHALL equal root `VERSION`.

#### Scenario: Versions agree

- **WHEN** root `VERSION`, the server `@version` annotation, and every iOS `MARKETING_VERSION` hold the same value
- **THEN** the drift guard passes

#### Scenario: Any component drifts

- **WHEN** root `VERSION` differs from the server annotation or any iOS `MARKETING_VERSION`
- **THEN** the drift guard exits non-zero naming the file and the offending value

### Requirement: Release versions derive from the core tag at build time

Shipped app artifacts SHALL take their marketing version from the core `vX.Y.Z`
release tag at build time: the CLI and Docker images from the tag, the mac app
and iOS app marketing versions from the tag, and the Linux connector binary
version from the tag.

#### Scenario: iOS release is core-versioned

- **WHEN** the iOS release workflow runs for tag `vX.Y.Z`
- **THEN** the archived app is built with marketing version `X.Y.Z`

#### Scenario: Linux connector release is core-versioned

- **WHEN** the connector release workflow runs for tag `vX.Y.Z`
- **THEN** it publishes `memory-connector_X.Y.Z_<os>_<arch>.tar.gz` on that release

### Requirement: Drift guard runs in CI and pre-commit

The version-drift guard SHALL run in the always-on CI workflow on pushes and
pull requests, and as a scoped pre-commit check when a versioned file is staged.
The `release` task SHALL bump root `VERSION`, the server annotation, and the iOS
`MARKETING_VERSION` values atomically in one commit.

#### Scenario: CI blocks drift

- **WHEN** a pull request leaves the canonical version files out of sync
- **THEN** CI fails

#### Scenario: Release bumps all sources atomically

- **WHEN** `task release VERSION=0.X.Y` runs
- **THEN** root `VERSION`, the server `@version`, and the iOS `MARKETING_VERSION` are updated and committed together

### Requirement: Automatic tagging follows the canonical version

The auto-tag workflow SHALL continue to create the `vX.Y.Z` tag from the root
`VERSION` file so that a version bump alone drives the release fan-out.

#### Scenario: VERSION bump tags a release

- **WHEN** a commit changing root `VERSION` lands on `main`
- **THEN** a matching `vX.Y.Z` tag is created and the release workflows run
