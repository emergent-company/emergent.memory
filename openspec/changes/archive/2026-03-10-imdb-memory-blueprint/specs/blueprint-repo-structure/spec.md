## ADDED Requirements

### Requirement: Blueprint-compatible directory layout

The repo root SHALL contain the directories and files required for `memory blueprints <source>` to apply it successfully.

#### Scenario: Applying the blueprint from GitHub URL

- **WHEN** a developer runs `memory blueprints https://github.com/emergent-company/imdb-memory-blueprint`
- **THEN** the CLI SHALL find and apply `packs/imdb.yaml` without error
- **AND** no `agents/` or `seed/` directory is required for the command to succeed

#### Scenario: Repo cloned and applied locally

- **WHEN** a developer clones the repo and runs `memory blueprints ./imdb-memory-blueprint`
- **THEN** the CLI SHALL apply the pack identically to the GitHub URL path

### Requirement: Seeder is a runnable Go program

The repo SHALL contain a standalone Go program at `cmd/seeder/main.go` that can be run with `go run ./cmd/seeder` after cloning.

#### Scenario: Running the seeder with go run

- **WHEN** a developer runs `go run ./cmd/seeder --server <url> --token <tok> --project <id>`
- **THEN** the seeder SHALL start streaming IMDb data and ingesting into the specified project

#### Scenario: Go module is self-contained

- **WHEN** a developer runs `go mod download` in the repo root
- **THEN** all dependencies SHALL resolve without requiring access to a private registry

### Requirement: README quickstart

The repo SHALL contain a `README.md` with a quickstart section covering the two-step workflow.

#### Scenario: Quickstart is complete and accurate

- **WHEN** a developer follows only the README quickstart
- **THEN** they SHALL be able to install the template pack and run the seeder against a local Memory instance without consulting any other documentation

#### Scenario: README documents all seeder flags

- **WHEN** a developer reads the README
- **THEN** all flags and their corresponding environment variables SHALL be listed with descriptions and defaults

### Requirement: No monorepo files committed

The repo SHALL NOT contain any files from the Memory monorepo, internal test fixtures, hardcoded project IDs, or credentials.

#### Scenario: Repo contains only blueprint and seeder files

- **WHEN** the repo is inspected
- **THEN** it SHALL contain only: `packs/`, `cmd/`, `go.mod`, `go.sum`, `README.md`, and optionally `.github/` for CI
- **AND** no `.env` files, log files, or test fixtures SHALL be present
