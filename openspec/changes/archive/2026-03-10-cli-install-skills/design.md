## Context

The emergent CLI (`tools/emergent-cli/`) is a Cobra-based Go tool that manages the Emergent platform. Commands are self-registered via `init()` in individual files under `internal/cmd/`. The CLI already handles local filesystem operations (install, upgrade, ctl) and remote API operations (mcp-servers, agents, projects). It has `gopkg.in/yaml.v3` as a direct dependency and `archive/tar`, `compress/gzip`, `archive/zip` available from the stdlib.

The target install location for skills in any project is `.agents/skills/<skill-name>/` (a directory containing at minimum `SKILL.md`). The agentskills.io spec requires that the `SKILL.md` frontmatter `name` field matches the parent directory name and follows strict naming rules (lowercase alphanumeric + hyphens, no leading/trailing/consecutive hyphens, max 64 chars).

## Goals / Non-Goals

**Goals:**
- Add `emergent install-skills` command group with `install`, `list`, `validate`, and `remove` subcommands
- Support local path as the only install source — `emergent install-skills install ./path/to/skill`
- Prompt user to confirm when installing over an existing skill (interactive); `--force` skips the prompt
- Auto-validate `SKILL.md` frontmatter on install (agentskills.io spec compliance)
- Default target: `.agents/skills/` relative to CWD; overridable with `--dir`
- All logic pure-CLI / local filesystem — no server API calls, no auth required, works fully offline

**Non-Goals:**
- Remote sources (GitHub shortcuts, HTTPS archive URLs) — not in scope
- A centralised skill registry/index (no server-side component)
- Dependency resolution between skills
- Skill versioning/locking (no lockfile)
- Auto-update installed skills
- Supporting non-agentskills.io skill formats

## Decisions

### Decision 1: Single file, self-contained command — `internal/cmd/install_skills.go`

The existing pattern (one file per command group, `init()` self-registration) is uniform across the CLI. A single new file `install_skills.go` keeps the change isolated and mergeable without touching any existing file.

*Alternatives considered:*
- Separate `internal/skills/` package — adds indirection without benefit given the limited scope and zero reuse from other commands.

### Decision 2: No new Go module dependencies

All install logic uses only stdlib (`os`, `path/filepath`, `io/fs`) and `gopkg.in/yaml.v3` already in `go.mod`. Local path copy is a simple recursive `os.ReadDir` + `os.CopyFS` / manual file copy. No network stack needed.

*Alternatives considered:*
- `go-git` or HTTP archive fetching for remote sources — removed from scope entirely per design decision to keep install local-only.

### Decision 3: Existing skill — prompt to update, not hard error

If `.agents/skills/<skill-name>/` already exists, rather than erroring (the original design) or silently overwriting, the CLI prompts interactively:

```
Skill 'my-skill' is already installed. Update it? [y/N]:
```

`--force` skips the prompt and updates directly. Non-interactive (piped) mode without `--force` prints an informative error and exits non-zero.

This is friendlier than a hard error for the common case of re-running an install, while still protecting against accidental overwrites in automation.

*Alternatives considered:*
- Hard error (original design) — unhelpful UX when a user just wants to refresh a skill they already have.
- Silent overwrite — too surprising in scripted contexts.

### Decision 4: Validate SKILL.md frontmatter inline, no external `skills-ref` tool

Parse YAML frontmatter manually using `gopkg.in/yaml.v3` (already a direct dependency). Apply the agentskills.io name rules as a local regex check. This keeps the CLI self-contained and offline-capable.

Validation rules implemented:
- `name` present, ≤64 chars, matches `^[a-z0-9][a-z0-9-]*[a-z0-9]$` or single char `^[a-z0-9]$`, no `--`
- `description` present, 1-1024 chars, non-empty
- Directory name matches `SKILL.md` `name` field

*Alternatives considered:*
- Shelling out to `skills-ref validate` — adds an external runtime dependency; not available in most environments.

### Decision 5: `list` reads from `.agents/skills/` in CWD; outputs table by default, JSON with `--output json`

Consistent with the rest of the CLI's output formatting conventions (table default, `--output json` for scripting). Reads `SKILL.md` frontmatter from each subdirectory to extract `name`, `description`, optional `metadata.version`, `license`.

### Decision 6: `install` is idempotent with `--force` flag

If `.agents/skills/<skill-name>/` already exists, default behavior is to error with a clear message. `--force` overwrites. This prevents silent overwrites in automation while allowing easy re-install.

## Risks / Trade-offs

- **`SKILL.md` `name` ≠ directory name in source** → Enforced at install time: the target directory is renamed to match the `name` field from `SKILL.md`. If no `SKILL.md` is found in the source directory, install fails with a clear error.
- **Accidental overwrite in CI** → Mitigated by the interactive prompt; non-interactive mode without `--force` always errors.

## Migration Plan

Pure additive change — no existing commands modified, no data migrations, no server deployment required. The new command is available immediately after a CLI binary update. No rollback concerns.

## Open Questions

- Should `install` also support GitLab/Bitbucket archive URLs in the future? (Deferred — HTTP URL source type already handles arbitrary HTTPS archives from any host.)
- Should there be a `search` / `browse` subcommand pointing at a curated skills registry? (Out of scope for this change; left as a future extension point.)
