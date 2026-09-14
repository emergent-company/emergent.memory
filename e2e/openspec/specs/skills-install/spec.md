# Capability Spec: skills-install

## Purpose

The `runlog skills install` command installs agent skills from a project's skill source directories into the install paths expected by each supported AI agent tool (e.g., opencode, claude, cursor).

## Requirements

### Requirement: Skill source discovery
The command SHALL discover available skills by scanning `.agents/skills/` and `.opencode/skills/` relative to the project root. Each immediate subdirectory of a source path is treated as one installable skill. Skills with the same name found in multiple sources SHALL be deduplicated (first-found wins, `.agents/skills/` takes precedence).

#### Scenario: Skills discovered from primary source
- **WHEN** `.agents/skills/` contains directories `foo` and `bar`
- **THEN** `runlog skills install --dry-run` lists both `foo` and `bar` as available skills

#### Scenario: Deduplication across sources
- **WHEN** `.agents/skills/foo` and `.opencode/skills/foo` both exist
- **THEN** only one `foo` skill is listed, sourced from `.agents/skills/foo`

#### Scenario: No skills found
- **WHEN** neither `.agents/skills/` nor `.opencode/skills/` exists
- **THEN** the command exits with a non-zero code and prints an error: "no skill sources found"

### Requirement: Agent tool detection
The command SHALL auto-detect which AI agent tools are configured in the project by probing for each tool's known marker (config dir or file) relative to the project root.

#### Scenario: Opencode detected
- **WHEN** `.opencode/` directory exists in the project root
- **THEN** `opencode` appears in the detected tools list

#### Scenario: Claude detected
- **WHEN** `.claude/` directory exists in the project root
- **THEN** `claude` appears in the detected tools list

### Requirement: Interactive tool selection
When run without `--all` or `--tools`, the command SHALL present a numbered list of ALL supported agent tools (sorted by ID) and prompt the user to enter a comma-separated selection. Detected tools are annotated with `[detected]` after their ID.

#### Scenario: User selects a subset
- **WHEN** detected tools are `[opencode, claude]` and user enters `1`
- **THEN** skills are installed only for the tool corresponding to entry `1`

#### Scenario: Undetected tool selectable
- **WHEN** `.claude/` does not exist and user enters the number corresponding to `claude`
- **THEN** skills are installed for `claude` (marker provisioned automatically)

#### Scenario: Detected annotation shown
- **WHEN** `.opencode/` exists and the prompt is displayed
- **THEN** the `opencode` entry shows `[detected]` in the list

#### Scenario: User enters "all"
- **WHEN** user enters `all` at the prompt
- **THEN** skills are installed for all supported tools

#### Scenario: User enters "none" or empty
- **WHEN** user enters `none` or presses Enter without input
- **THEN** the command exits cleanly with no changes

### Requirement: Non-interactive tool selection via flags
The command SHALL accept `--all` and `--tools <id,...>` flags to bypass the interactive prompt.

#### Scenario: --all flag installs for all supported tools
- **WHEN** `runlog skills install --all` is run
- **THEN** skills are installed for every supported tool in the registry (detected or not) without prompting

#### Scenario: --all installs for undetected tool
- **WHEN** `runlog skills install --all` is run and `.claude/` does not exist
- **THEN** skills are installed for `claude` (marker provisioned automatically) along with all other tools

#### Scenario: --tools flag targets specific tools
- **WHEN** `runlog skills install --tools opencode,claude` is run
- **THEN** skills are installed for `opencode` and `claude` only, regardless of detection

#### Scenario: --tools with unknown ID
- **WHEN** `runlog skills install --tools unknowntool` is run
- **THEN** the command exits with a non-zero code and prints an error listing valid tool IDs

### Requirement: Marker auto-provisioning
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

### Requirement: Skill installation
The command SHALL recursively copy each skill directory from its source path to the target tool's install directory. Existing installs SHALL NOT be overwritten unless `--force` is passed.

#### Scenario: Successful install
- **WHEN** `runlog skills install --tools opencode` is run and `.agents/skills/foo` exists
- **THEN** `.opencode/skills/foo/` is created with contents mirroring `.agents/skills/foo/`

#### Scenario: Skip existing install
- **WHEN** `.opencode/skills/foo` already exists and `--force` is not passed
- **THEN** the skill is skipped and the command prints "skipped: foo (already exists; use --force to overwrite)"

#### Scenario: Force overwrite
- **WHEN** `.opencode/skills/foo` already exists and `--force` is passed
- **THEN** the existing directory is removed and replaced with a fresh copy

#### Scenario: Install summary
- **WHEN** install completes
- **THEN** the command prints a summary: "installed N skills for M tools"

### Requirement: Dry-run mode
The command SHALL support `--dry-run` which prints what would be installed without modifying the filesystem.

#### Scenario: Dry-run output
- **WHEN** `runlog skills install --dry-run --all` is run
- **THEN** the command prints each planned action (e.g., "would install: foo → .opencode/skills/foo") and exits without writing any files

#### Scenario: Dry-run does not write
- **WHEN** `--dry-run` is passed
- **THEN** no directories or files are created or modified

### Requirement: Tool install path registry
The command SHALL maintain a static registry mapping each supported tool ID to its skill install path pattern (relative to project root), consistent with the OpenSpec supported-tools table.

#### Scenario: opencode install path
- **WHEN** target tool is `opencode`
- **THEN** skills are installed under `.opencode/skills/<skill-name>/`

#### Scenario: claude install path
- **WHEN** target tool is `claude`
- **THEN** skills are installed under `.claude/skills/<skill-name>/`

#### Scenario: cursor install path
- **WHEN** target tool is `cursor`
- **THEN** skills are installed under `.cursor/skills/<skill-name>/`

#### Scenario: Generic agents fallback
- **WHEN** target tool is `agents` (the generic fallback)
- **THEN** skills are installed under `.agents/skills/<skill-name>/`
