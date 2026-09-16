# Delta Spec: skills-install (modified)

Base spec: `openspec/specs/skills-install/spec.md`

## Changes

### Modified Requirement: Agent tool detection / no-tools-detected behaviour
**Old:** When no tool markers are detected, the command prints "no agent tools detected; use --tools to specify targets" and exits with code 0.
**New:** Detection is no longer a gate for interactive mode. The interactive prompt always shows all supported tools (annotated with `[detected]` where applicable). The "no agent tools detected" early-exit is removed.

### Modified Requirement: Interactive tool selection — shows all tools
**Old:** The numbered prompt shows only detected tools.
**New:** The numbered prompt shows all supported tools (sorted by ID). Detected tools are annotated with `[detected]` after the ID.

#### Scenario: Undetected tool selectable
- **WHEN** `.claude/` does not exist and user enters the number corresponding to `claude`
- **THEN** skills are installed for `claude` (marker provisioned automatically)

#### Scenario: Detected annotation shown
- **WHEN** `.opencode/` exists and the prompt is displayed
- **THEN** the `opencode` entry shows `[detected]` in the list

### Modified Requirement: `--all` targets all supported tools
**Old:** `--all` installs for all *detected* tools.
**New:** `--all` installs for all *supported* tools in the registry (detected or not).

#### Scenario: --all installs for undetected tool
- **WHEN** `runlog skills install --all` is run and `.claude/` does not exist
- **THEN** skills are installed for `claude` (marker provisioned automatically) along with all other tools

### New Requirement: Marker auto-provisioning
Before installing skills for a tool, the command SHALL create the tool's marker if it does not exist.

#### Scenario: Dir marker provisioned
- **WHEN** `.claude/` does not exist and `claude` is selected
- **THEN** `.claude/` is created before skills are copied into `.claude/skills/`

#### Scenario: File marker provisioned (copilot)
- **WHEN** `.github/copilot-instructions.md` does not exist and `copilot` is selected
- **THEN** `.github/` directory is created and `.github/copilot-instructions.md` is created as an empty file

#### Scenario: Provisioning skipped in dry-run
- **WHEN** `--dry-run` is active
- **THEN** no marker dirs or files are created

#### Scenario: Provisioning skipped when marker exists
- **WHEN** the tool's marker already exists
- **THEN** `provisionMarker` is a no-op (no error, no message)
