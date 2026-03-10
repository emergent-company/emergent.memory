## Why

The emergent CLI has no mechanism for discovering and installing Agent Skills into a project's `.agents/skills/` directory. Skills — as defined by the [agentskills.io](https://agentskills.io) open standard — are versioned, portable capability packages (a `SKILL.md` + optional `scripts/`, `references/`, `assets/`) that AI agents can load on-demand to extend their knowledge and workflows. Without an `install-skills` command, teams must manually copy skill directories, can't reference a registry, and have no consistent layout contract for skills they publish or consume.

## What Changes

- **New command** `emergent install-skills` added to the emergent CLI, with subcommands:
  - `install-skills install <path>` — install a skill from a local directory path into `.agents/skills/<skill-name>/` of the current working directory
  - `install-skills list` — list installed skills (reads `SKILL.md` frontmatter from `.agents/skills/*/SKILL.md`)
  - `install-skills validate [path]` — validate a skill directory's `SKILL.md` against the agentskills.io spec
  - `install-skills remove <skill-name>` — remove an installed skill
- **Source**: local path only — `emergent install-skills install ./path/to/skill-dir`
- **Existing skill handling**: if a skill with the same name is already installed, the CLI prompts the user interactively to confirm the update; `--force` skips the prompt
- **Target directory** defaults to `.agents/skills/` relative to CWD; overridable with `--dir`
- **Validation** of `SKILL.md` frontmatter (name, description, naming rules) happens automatically on install; `--skip-validate` flag to bypass
- **No new server-side component** — this is a purely local/filesystem CLI feature; no new API endpoints are required

## Capabilities

### New Capabilities

- `cli-install-skills`: CLI command for discovering, installing, listing, validating, and removing Agent Skills (agentskills.io format) into a project's `.agents/skills/` directory

### Modified Capabilities

- `cli-tool`: The `emergent` CLI gains a new top-level command group `install-skills`; no existing requirements change but the command inventory expands

## Impact

- **Code**: New file `tools/emergent-cli/internal/cmd/install_skills.go`; no changes to existing command files beyond registration via `init()`
- **Dependencies**: No new Go module dependencies — `gopkg.in/yaml.v3` (already direct), stdlib `os`, `path/filepath`, `io/fs` cover all local filesystem operations
- **Filesystem**: Reads/writes `.agents/skills/` in the user's current project directory — no server calls, no auth required (works fully offline)
- **No API changes**: Zero new endpoints, no migrations, no frontend changes
