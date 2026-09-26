# git-hooks Specification

## Purpose

Defines how the repository installs and runs git hooks: a single lefthook configuration at the git root, with fast path-scoped checks on `pre-commit` and a full lint group runnable on demand, replacing the previous split husky + per-directory-lefthook setup.

## ADDED Requirements

### Requirement: Single repo-root hook configuration

The repository SHALL define all git hooks in exactly one lefthook configuration at the git repo root (`lefthook.yml`). Because git hooks are repo-global and lefthook loads one config from the git root, there SHALL be no per-directory lefthook config and no husky hook directory.

Every hook job SHALL declare the tree it operates on using `root:` (setting the job's working directory) and/or `glob:` (path filtering), so that jobs for different apps live in one file without interfering.

#### Scenario: Hooks installed once

- **WHEN** a developer runs `task hooks:install` (or `lefthook install`) in the repo
- **THEN** lefthook installs the git hooks at the repo root
- **AND** every subsequent `git commit` runs the repo-root configuration

#### Scenario: No per-directory or husky config

- **WHEN** the tree is inspected
- **THEN** there is no `.husky/` directory and no `apps/web-ui/lefthook.yml`
- **AND** the only lefthook config is the repo-root `lefthook.yml`

### Requirement: Pre-commit is fast and path-scoped

The `pre-commit` hook SHALL run only fast, file-scoped checks and SHALL skip a job when no staged file matches that job's path scope. It SHALL NOT run the test suite.

Server (`apps/server`) staged Go files SHALL trigger `gofmt`, `go vet`, `go build`, and the architectural lint ratchet. Staged handler files SHALL each carry a Swagger `@Router` annotation. Staged migration SQL SHALL be free of leading backslash metacommands and spaced dollar quotes. Staged Go files that import a local package not tracked in git SHALL fail the hook.

Web UI (`apps/web-ui`) staged Python SHALL run `ruff`; staged gateway (Go) files SHALL run `gofmt`, `go vet`, `go build`; staged `.templ` files SHALL pass `templ generate -check`; any staged change SHALL pass the repo-wide `gitleaks` secrets scan; and staged files SHALL pass the generated-file guard.

#### Scenario: Path-scoped server checks

- **WHEN** a staged Go file under `apps/server` is not gofmt-formatted
- **THEN** the `pre-commit` hook fails and reports the file
- **AND** jobs scoped to other apps are skipped

#### Scenario: Untracked import guard

- **WHEN** a staged Go file imports a `github.com/emergent-company/emergent.memory/...` package that has no tracked Go files
- **THEN** the `pre-commit` hook fails and names the untracked package

#### Scenario: Tests are not run on commit

- **WHEN** the `pre-commit` hook runs
- **THEN** no unit or integration test suite is executed (tests are owned by CI)

### Requirement: Repo-wide secret scanning

A single repo-root `.gitleaks.toml` SHALL configure secret detection for the whole monorepo. The `pre-commit` hook SHALL scan **staged** changes strictly (no baseline). The `lint` and `lint-webui` groups SHALL scan the whole working tree. Pre-existing findings SHALL be waived through a committed `.gitleaksignore` **ratchet** whose entries may only ever be removed and never added, so that the whole-tree scan starts green while any new leak fails.

#### Scenario: Staged secret blocks the commit

- **WHEN** a staged change introduces a detected secret
- **THEN** the `pre-commit` hook fails and reports the finding

#### Scenario: Ratchet keeps the tree green

- **WHEN** the whole-tree secret scan runs in a lint group
- **THEN** findings listed in `.gitleaksignore` do not fail the scan
- **AND** any finding not listed fails it

#### Scenario: Single secrets config

- **WHEN** the tree is inspected
- **THEN** the only gitleaks config is the repo-root `.gitleaks.toml` (no app-scoped config)

### Requirement: Full lint group

The configuration SHALL expose a `lint` group that runs the full static-analysis set for every tree — server, CLI, web UI, and Linux connector: `gofmt`, `go vet`, `go build`, `go test`, `golangci-lint`, plus `ruff`, `templ generate -check`, and `gitleaks` for the web UI.

The root `task lint` SHALL run this group. The web-ui `task lint` SHALL run a `lint-webui` group scoped to the web UI and connector, preserving its previous scope.

#### Scenario: Root lint runs everything

- **WHEN** a developer runs `task lint` at the repo root
- **THEN** lefthook runs the `lint` group across all trees

#### Scenario: Web-ui lint stays scoped

- **WHEN** a developer runs `task lint` in `apps/web-ui`
- **THEN** lefthook runs the `lint-webui` group (web UI + connector only)
