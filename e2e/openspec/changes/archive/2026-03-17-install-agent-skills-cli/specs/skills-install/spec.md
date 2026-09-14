## ADDED Requirements

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

#### Scenario: No tools detected
- **WHEN** no known tool marker directories exist
- **THEN** the command prints a notice "no agent tools detected; use --tools to specify targets" and exits cleanly (exit 0)

### Requirement: Interactive tool selection
When run without `--all` or `--tools`, the command SHALL present a numbered list of detected agent tools and prompt the user to enter a comma-separated selection.

#### Scenario: User selects a subset
- **WHEN** detected tools are `[opencode, claude]` and user enters `1`
- **THEN** skills are installed only for `opencode`

#### Scenario: User enters "all"
- **WHEN** user enters `all` at the prompt
- **THEN** skills are installed for all detected tools

#### Scenario: User enters "none" or empty
- **WHEN** user enters `none` or presses Enter without input
- **THEN** the command exits cleanly with no changes

### Requirement: Non-interactive tool selection via flags
The command SHALL accept `--all` and `--tools <id,...>` flags to bypass the interactive prompt.

#### Scenario: --all flag installs for all detected tools
- **WHEN** `runlog skills install --all` is run
- **THEN** skills are installed for every detected tool without prompting

#### Scenario: --tools flag targets specific tools
- **WHEN** `runlog skills install --tools opencode,claude` is run
- **THEN** skills are installed for `opencode` and `claude` only, regardless of detection

#### Scenario: --tools with unknown ID
- **WHEN** `runlog skills install --tools unknowntool` is run
- **THEN** the command exits with a non-zero code and prints an error listing valid tool IDs

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
