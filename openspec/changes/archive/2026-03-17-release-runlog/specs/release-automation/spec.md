## ADDED Requirements

### Requirement: goreleaser configuration produces cross-platform binaries
The repository SHALL contain a `.goreleaser.yaml` that builds the `cmd/runlog` binary for `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, and `windows/amd64`.

#### Scenario: goreleaser builds all targets
- **WHEN** `goreleaser build --snapshot --clean` is run
- **THEN** binary artifacts are produced for all five OS/arch combinations

#### Scenario: Binaries are statically linked (no CGO)
- **WHEN** the goreleaser config is inspected
- **THEN** `CGO_ENABLED=0` is set in the build environment

### Requirement: GitHub Actions release workflow on tag push
The repository SHALL contain a GitHub Actions workflow (`.github/workflows/release.yml`) that triggers on version tag pushes (`v*`) and runs goreleaser to publish a GitHub Release with binaries and checksums.

#### Scenario: Tag push triggers release
- **WHEN** a tag matching `v*` is pushed to the repository
- **THEN** the release workflow runs, creates a GitHub Release, and attaches binary archives and a checksums file

#### Scenario: Release includes changelog
- **WHEN** a release is published
- **THEN** the release body includes auto-generated changelog entries since the last tag

### Requirement: GitHub Actions CI workflow on push and PR
The repository SHALL contain a GitHub Actions workflow (`.github/workflows/ci.yml`) that runs on pushes to `main` and pull requests. It SHALL run `go vet`, `go build ./...`, and `go test ./...`.

#### Scenario: CI runs on pull request
- **WHEN** a pull request is opened against `main`
- **THEN** the CI workflow runs and reports pass/fail status

### Requirement: Install script for binary download
The repository SHALL contain an `install.sh` script that downloads the appropriate binary for the user's OS/architecture from the latest GitHub Release.

#### Scenario: Install script detects OS and architecture
- **WHEN** `curl -sSfL https://raw.githubusercontent.com/emergent-company/runlog/main/install.sh | sh` is run on a Linux amd64 system
- **THEN** the script downloads the `linux_amd64` binary and places it in a standard location (`/usr/local/bin` or `$HOME/.local/bin`)

### Requirement: Semantic versioning starting at v0.1.0
The first release SHALL be tagged `v0.1.0`. All subsequent releases SHALL follow semantic versioning (semver 2.0.0).

#### Scenario: First release tag
- **WHEN** the first release is created
- **THEN** the tag is `v0.1.0` and the Go module proxy resolves it correctly
