## Context

The project's `runlog` CLI binary (`cmd/runlog/`, distributed from `github.com/emergent-company/runlog`) is the operator's primary interface for inspecting test run history. Skills for multiple AI agents live in `.agents/skills/` and `.opencode/skills/` but are installed manually. The new `skills install` subcommand brings the same self-service ergonomics as OpenSpec's `openspec init` to this project's skill ecosystem.

The canonical skill source is `.agents/skills/` (this repo's convention). Secondary sources may exist (e.g., `.opencode/skills/`). Each AI tool has its own required install directory (matching the OpenSpec supported-tools table).

**Current state:** No automated install mechanism. Contributors copy skill folders by hand into whichever tool dir they use.

## Goals / Non-Goals

**Goals:**
- Detect which AI agent tools are active in the project (by probing for tool-specific config dirs/files)
- Discover available skill folders from known source paths
- Present an interactive multi-select prompt for target agents
- Copy skill directories into each agent's required install path
- Provide a `--all` flag to install for all detected agents without prompting
- Provide a `--dry-run` flag to show what would be installed without touching the filesystem
- No external dependencies — use only Go standard library

**Non-Goals:**
- Symlinks (copy is simpler and more portable across OS/Docker environments)
- Downloading skills from a remote registry
- Managing skill updates / diffing existing installs
- Installing OpenSpec commands (slash commands) — only skills
- Cross-platform path normalisation beyond what `filepath` provides

## Decisions

### D1: Subcommand placement — `runlog skills install`

**Decision:** Add a `skills` subcommand to the existing `runlog` binary with `install` as its action.

**Rationale:** Keeps all project tooling under one binary. Matches the pattern used by OpenSpec (`openspec init`, `openspec update`). Alternative — a standalone `install-skills` binary — would require a separate `cmd/install-skills/` entry point and doesn't reuse existing CLI plumbing.

**Alternative considered:** Separate binary in `cmd/install-skills/main.go`. Rejected: adds cognitive overhead; users already have `runlog` on PATH from `install.sh`.

### D2: Source discovery — static map + probe

**Decision:** Define a static Go map `toolSkillPaths` that maps tool ID → install dir (relative to project root). Probe each tool by checking if its config dir or a known marker file exists. Build a detected list from that probe.

**Rationale:** Keeps code simple and auditable. No reflection, no external config file to maintain at runtime.

**Alternative considered:** Read a YAML/JSON config file for the map. Rejected: violates the "no external dependencies, standard library only" constraint and adds a config file that could go stale.

### D3: Interactive prompt — terminal line-by-line, not a TUI widget

**Decision:** Use a numbered list printed to stdout and read a comma-separated selection from stdin (e.g., `1,3`). Supports "all" and "none" shortcuts.

**Rationale:** The `runlog` TUI uses `bubbletea`, but spawning a full TUI for a one-shot install command is heavy. A simple line prompt works in all terminals including non-interactive CI (where `--all` or `--tools` flags bypass it).

**Alternative considered:** Full bubbletea checkbox UI. Rejected: overkill for a setup command; interactive TTY detection adds complexity.

### D4: Install mechanism — recursive copy

**Decision:** Recursively copy each skill directory from source to target using `os.MkdirAll` + `io.Copy`. Skip if the target already exists (no overwrite by default); add `--force` to overwrite.

**Rationale:** Copying avoids symlink issues inside Docker containers and is more portable. No need for atomic replace since these are doc files, not binaries.

### D5: Where `runlog` binary is extended

**Decision:** Add `cmd/runlog/skills.go` in the existing `runlog` module (i.e., `github.com/emergent-company/runlog`), not in this e2e repo.

**Rationale:** The `runlog` binary is the tool — it lives in its own module. This e2e repo only consumes it. Implementation tasks therefore describe changes to make in `cmd/runlog/` of the standalone `runlog` repo.

## Risks / Trade-offs

- **[Risk] runlog module is a separate repo** → The implementation lives in `github.com/emergent-company/runlog`, not here. Tasks must be clear that code changes go there.
- **[Risk] Tool detection is heuristic** → A tool may be present but its config dir deleted, causing false negatives. Mitigation: allow `--tools <ids>` flag to override detection.
- **[Risk] Source skill dirs evolve independently** → A stale copy in a tool dir won't auto-update. Mitigation: document that re-running `runlog skills install --force` refreshes installs. Future `runlog skills update` command is out of scope here.
- **[Trade-off] No symlinks** → Disk usage is N×skills for N tools. Acceptable: skill files are small markdown docs.

## Migration Plan

1. Implement `skills install` subcommand in `github.com/emergent-company/runlog` (`cmd/runlog/skills.go`)
2. Tag a new patch release of `runlog`
3. Update `go.mod` in this e2e repo to require the new version
4. Update `AGENTS.md` to document `runlog skills install`
5. Rollback: remove the subcommand; old `runlog` versions continue working (additive change only)

## Open Questions

- Should `runlog skills install` also discover skills from `.opencode/skills/` as a second source directory, or only `.agents/skills/`? (Recommendation: both, deduplicated by name.)
- Should the tool map be user-extensible via a local config file in the future? (Out of scope now; static map is sufficient.)
