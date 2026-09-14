---
name: runlog-install-skills
description: Install runlog agent skills into the correct directories for all configured AI agent tools using the runlog skills install command.
metadata:
  author: emergent
  version: "2.0"
---

# runlog-install-skills

Use `runlog skills install` to copy embedded runlog skills into each configured AI agent tool's install path. Skills are bundled inside the `runlog` binary — no external skill source directories are needed.

## When to use

- A new contributor is onboarding and needs to set up skills for their preferred AI agent tool
- The `runlog` binary has been updated and skills need refreshing in tool directories
- You want to verify which skills would be installed without making changes

## How to invoke

```bash
# Install for all detected tools (recommended for onboarding)
runlog skills install --all

# Preview what would be installed first
runlog skills install --dry-run --all

# Install for a specific tool only
runlog skills install --tools opencode

# Refresh existing installs (overwrite)
runlog skills install --all --force
```

## Embedded skills

The following skills are bundled in the `runlog` binary:

| Skill Name | Description |
|---|---|
| `runlog-test-designer` | Design and write high-quality e2e tests |
| `runlog-verify-e2e-changes` | Compile and smoke-test e2e suite changes |
| `runlog-verify-runs` | Audit run log quality |
| `runlog-clear` | Clear runs database and log files |
| `runlog-install-skills` | This skill — install runlog skills |

## Tool detection

The command detects tools by checking for their config directory:

| Tool ID   | Marker         | Install path            |
|-----------|----------------|-------------------------|
| opencode  | `.opencode/`   | `.opencode/skills/`     |
| claude    | `.claude/`     | `.claude/skills/`       |
| cursor    | `.cursor/`     | `.cursor/skills/`       |
| agents    | `.agents/`     | `.agents/skills/`       |
| windsurf  | `.windsurf/`   | `.windsurf/skills/`     |

Use `--tools <id>` to bypass detection and target tools explicitly.

## Notes

- Skills are embedded in the `runlog` binary and do not require `.agents/skills/` on disk.
- Only `runlog-*` prefixed skills are installed — no unrelated skills (openspec, commit, etc.) are included.
- Existing installs are skipped unless `--force` is passed.
- Dry-run mode (`--dry-run`) never writes to the filesystem.
