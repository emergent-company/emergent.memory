## Why

`runlog skills install` currently silently exits when no tool markers are detected, leaving users with no way to install skills into a tool they haven't yet configured — even though the tool is fully supported. Users should always be able to see what tools are supported and install for any of them, with the command provisioning any missing marker dirs/files automatically.

## What Changes

- **Detection becomes listing**: when no markers are detected (or `--all` is used with no detected tools), show the full supported-tools list instead of exiting with a "nothing detected" message.
- **Auto-provision markers**: before installing skills for a target tool, create its marker dir/file if it doesn't exist, so future invocations detect it automatically.
- **Interactive mode shows all**: the prompt always shows all supported tools, marking which are already detected, so users can select any tool — not just detected ones.
- **`runlog skills list`**: new subcommand that prints the full supported-tools table (tool ID, marker path, install dir, detected yes/no) without installing anything.

## Capabilities

### New Capabilities

- `skills-install-list-all`: `runlog skills list` prints all supported tools with detection status and install paths.

### Modified Capabilities

- `skills-install`: detection no longer gates the interactive tool list; markers are provisioned automatically when missing; `--all` targets all supported tools (not just detected ones); interactive mode shows all tools with a `[detected]` annotation.

## Impact

- `cmd/runlog/skills.go` in the `github.com/emergent-company/runlog` repository: `detectTools`, `selectTools`, `cmdSkillsInstall`, and the top-level `cmdSkills` dispatcher all change.
- No schema, DB, or framework changes.
- `--tools <id>` already bypasses detection and already works for any registered tool — no change needed there.
- No breaking changes: all existing flags (`--all`, `--tools`, `--force`, `--dry-run`) keep the same semantics. The only behaviour difference is that `--all` now targets all supported tools and the interactive list shows all tools.
