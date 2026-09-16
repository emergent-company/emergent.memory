# Design: skills-install-always-list

## Overview

Three focused changes to `cmd/runlog/skills.go`:

1. **`runlog skills list`** — currently lists only embedded skill names. Change it to list all supported *tools* (from `toolRegistry`) with their marker path, install dir, and live detection status.
2. **`--all` targets all supported tools** — currently `--all` targets only detected tools. Change it to target all tools in `toolRegistry`.
3. **Interactive mode shows all tools** — currently `selectTools` shows only detected tools. Change it to show all tools annotated with `[detected]`, so users can install for any supported tool regardless of detection.
4. **Auto-provision markers** — before installing skills for a tool, create its marker dir/file if it doesn't exist.

## Data Model

No changes to stored data. All changes are in-process logic within `skills.go`.

`toolEntry` gains no new fields — detection is computed live by `detectTools` (or an inline check). For display, `selectTools` and `cmdSkillsList` need the full sorted registry plus a set of detected IDs.

## Algorithm Changes

### `allToolIDs() []string`
New helper. Returns all keys of `toolRegistry` sorted alphabetically.

### `cmdSkillsList(projectRoot string)`
Signature changes to accept `projectRoot` (passed from `cmdSkillsInstall` caller or computed from `os.Getwd()`). Prints a table: tool ID, marker path, install dir, detected (`yes`/`no`).

### `selectTools(allIDs, detectedSet)`
Signature changes: receives all tool IDs and a set of detected IDs. Shows all tools, appending `[detected]` to those in the detected set. User can pick any.

### `cmdSkillsInstall` — target resolution
```
if --tools:
    validate and use as-is (no change)
else if --all:
    targetTools = allToolIDs()          # was: detected only
else:
    detected = detectTools(projectRoot)
    detectedSet = set(detected)
    targetTools = selectTools(allToolIDs(), detectedSet)
```

### `provisionMarker(projectRoot, toolID)`
New helper. Called once per target tool before installing skills.
- Looks up `toolRegistry[toolID].MarkerPath`
- If path ends with known file extension (`.md`) → treat as file marker: create parent dirs + touch the file if missing
- Otherwise → treat as dir marker: `os.MkdirAll`

## Edge Cases

- **`copilot` marker is a file** (`.github/copilot-instructions.md`): provisioning must `MkdirAll` the parent dir and create an empty file, not a directory.
- **`--dry-run`**: `provisionMarker` must be skipped (or logged-only) when dry-run is active.
- **`--tools` flag**: provisioning still applies — if the user explicitly targets a tool, we should provision its marker so future detection works.

## Affected Functions

| Function | Change |
|---|---|
| `cmdSkillsList` | Rewrite: show tool table, not skill names |
| `selectTools` | Signature + body: show all tools, annotate detected |
| `cmdSkillsInstall` | `--all` uses `allToolIDs()`; call `provisionMarker` per target |
| `allToolIDs` | New helper |
| `provisionMarker` | New helper |
| `skillsInstallUsage` | Update `--all` description |

`discoverEmbeddedSkills`, `installEmbeddedSkill`, `detectTools` are unchanged.
