## Why

Runlog is a terminal-native test observability tool (TUI + framework library) embedded inside the `emergent.memory.e2e` repository. It has no competitors in the "local-first, terminal-native, AI-powered Go test observability" space, yet it cannot be adopted by anyone outside this project because it is locked inside a monorepo with a project-specific module path, hardcoded test registries, and no release infrastructure. Extracting it into a standalone product lets external Go teams adopt it, builds community credibility, and decouples runlog's release cadence from the e2e test suite.

## What Changes

- **New standalone repository** `github.com/emergent-company/runlog` with its own Go module, replacing the current `cmd/runlog/` and `framework/` code that lives under `github.com/emergent-company/emergent.memory.e2e`.
- **Two importable packages**: a library package (`runlog` or `runlog/framework`) for test authors, and a CLI/TUI binary (`cmd/runlog`) for interactive use.
- **Hardcoded `knownTests` registry removed** from the TUI binary; replaced with auto-discovery from the SQLite database (tests register themselves at runtime) and optional user-supplied config.
- **Build & release automation**: goreleaser config + GitHub Actions workflow producing cross-platform binaries and a homebrew tap/install script.
- **Standalone documentation**: README with quick-start, install instructions, feature overview, and migration guide for existing users.
- **Back-reference from e2e repo**: this repo (`emergent.memory.e2e`) becomes a *consumer* of the new `runlog` module instead of containing it.

## Capabilities

### New Capabilities
- `module-extraction`: Go module split — separate `github.com/emergent-company/runlog` module with library + binary packages, clean import paths, and semantic versioning.
- `release-automation`: goreleaser config, GitHub Actions release workflow, cross-platform binary builds, install script, and optional homebrew tap.
- `dynamic-test-registry`: Replace hardcoded `knownTests` map with auto-discovery from the SQLite database plus optional config-file overrides, so the TUI works for any Go project without code changes.
- `standalone-docs`: README, quick-start guide, feature overview, and migration guide for the new standalone repository.

### Modified Capabilities
- `e2e-framework`: The framework package moves to the new module. Import paths in this repo change from `github.com/emergent-company/emergent.memory.e2e/framework` to `github.com/emergent-company/runlog/framework` (or similar). Existing API surface stays the same; only the import path changes.

## Impact

- **Code movement**: `framework/` and `cmd/runlog/` directories move to the new repository. This repo retains thin wrappers or re-exports during transition, then switches to importing the new module.
- **Go module**: `go.mod` in this repo adds a dependency on `github.com/emergent-company/runlog`. The old framework package path becomes an alias or is removed.
- **CI**: New repo needs its own CI (build, test, release). This repo's CI updates import paths.
- **Dependencies**: The new module carries `modernc.org/sqlite`, `charmbracelet/bubbletea`, `charmbracelet/lipgloss`, and `google.golang.org/adk` + `genai` (for the analyzer). The analyzer's Google AI dependency is optional — projects that don't use LLM analysis won't need it.
- **Breaking for current workflow**: After extraction, `go build ./cmd/runlog` no longer works in this repo. Users run `go install github.com/emergent-company/runlog/cmd/runlog@latest` or use a released binary.
- **Database schema**: No changes to the SQLite schema. Existing `runs.db` files remain compatible.
