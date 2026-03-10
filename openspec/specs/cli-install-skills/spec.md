## ADDED Requirements

### Requirement: Install Skill from Local Path

The CLI SHALL install a skill from a local directory path into the project's `.agents/skills/` directory.

#### Scenario: Install from local path

- **WHEN** user runs `memory install-skills install ./path/to/my-skill`
- **THEN** CLI validates the directory contains a `SKILL.md` with valid frontmatter
- **AND** copies the directory to `.agents/skills/<skill-name>/` where `<skill-name>` is the `name` field from `SKILL.md`
- **AND** prints `✓ Installed skill '<skill-name>'`

#### Scenario: Install from local path — target already exists (non-interactive)

- **WHEN** user runs `memory install-skills install ./path/to/my-skill`
- **AND** `.agents/skills/<skill-name>/` already exists
- **AND** CLI is running non-interactively (no TTY)
- **THEN** CLI exits with error `skill '<skill-name>' is already installed; use --force to overwrite`

#### Scenario: Install from local path — target already exists (interactive)

- **WHEN** user runs `memory install-skills install ./path/to/my-skill`
- **AND** `.agents/skills/<skill-name>/` already exists
- **AND** CLI is running in an interactive terminal
- **THEN** CLI prompts `Skill '<skill-name>' is already installed. Update? [y/N]:`
- **AND** on `y`/`Y`: removes the existing directory, installs the new version, prints `✓ Reinstalled skill '<skill-name>'`
- **AND** on anything else: aborts without modifying the filesystem

#### Scenario: Install from local path with --force

- **WHEN** user runs `memory install-skills install ./path/to/my-skill --force`
- **AND** `.agents/skills/<skill-name>/` already exists
- **THEN** CLI removes the existing directory and installs the new version
- **AND** prints `✓ Reinstalled skill '<skill-name>'`

#### Scenario: Install from local path — SKILL.md missing

- **WHEN** user runs `memory install-skills install ./path/to/dir`
- **AND** the directory does not contain a `SKILL.md` file
- **THEN** CLI exits with error `no SKILL.md found in <path>`

---

### Requirement: SKILL.md Frontmatter Validation

The CLI SHALL validate a skill's `SKILL.md` frontmatter against the agentskills.io specification before and during install.

#### Scenario: Valid SKILL.md passes validation

- **WHEN** a `SKILL.md` has `name` and `description` fields that comply with spec rules
- **THEN** validation passes with no errors

#### Scenario: Invalid name — uppercase characters

- **WHEN** a `SKILL.md` has `name: MySkill`
- **THEN** validation fails with error `name must contain only lowercase letters, numbers, and hyphens`

#### Scenario: Invalid name — leading hyphen

- **WHEN** a `SKILL.md` has `name: -my-skill`
- **THEN** validation fails with error `name must not start or end with a hyphen`

#### Scenario: Invalid name — consecutive hyphens

- **WHEN** a `SKILL.md` has `name: my--skill`
- **THEN** validation fails with error `name must not contain consecutive hyphens`

#### Scenario: Invalid name — exceeds 64 characters

- **WHEN** a `SKILL.md` has a `name` longer than 64 characters
- **THEN** validation fails with error `name must be 64 characters or fewer`

#### Scenario: Missing description

- **WHEN** a `SKILL.md` has `name` but no `description` field
- **THEN** validation fails with error `description is required`

#### Scenario: Skip validation with --skip-validate

- **WHEN** user runs `memory install-skills install ./path --skip-validate`
- **THEN** CLI installs the skill without checking `SKILL.md` frontmatter
- **AND** prints a warning `⚠ Skipping SKILL.md validation`

#### Scenario: Standalone validate command — valid skill

- **WHEN** user runs `memory install-skills validate ./path/to/skill`
- **AND** the skill directory has a valid `SKILL.md`
- **THEN** CLI prints `✓ Valid SKILL.md for skill '<skill-name>'` and exits with code 0

#### Scenario: Standalone validate command — invalid skill

- **WHEN** user runs `memory install-skills validate ./path/to/skill`
- **AND** the `SKILL.md` fails validation
- **THEN** CLI prints each validation error and exits with non-zero code

---

### Requirement: List Installed Skills

The CLI SHALL list all skills installed in the project's `.agents/skills/` directory.

#### Scenario: List skills — table output

- **WHEN** user runs `memory install-skills list`
- **AND** `.agents/skills/` contains one or more valid skill directories
- **THEN** CLI prints a table with columns: Name, Description, Version, License
- **AND** Name and Description are read from each skill's `SKILL.md` frontmatter

#### Scenario: List skills — JSON output

- **WHEN** user runs `memory install-skills list --output json`
- **THEN** CLI outputs a JSON array of objects with `name`, `description`, `version`, `license`, `path` fields

#### Scenario: List skills — no skills installed

- **WHEN** user runs `memory install-skills list`
- **AND** `.agents/skills/` is empty or does not exist
- **THEN** CLI prints `No skills installed in .agents/skills/`

#### Scenario: List with custom directory

- **WHEN** user runs `memory install-skills list --dir ./custom/skills`
- **THEN** CLI reads skills from the specified directory

---

### Requirement: Remove Installed Skill

The CLI SHALL remove an installed skill by name.

#### Scenario: Remove existing skill

- **WHEN** user runs `memory install-skills remove my-skill`
- **THEN** CLI removes `.agents/skills/my-skill/` recursively
- **AND** prints `✓ Removed skill 'my-skill'`

#### Scenario: Remove non-existent skill

- **WHEN** user runs `memory install-skills remove unknown-skill`
- **AND** `.agents/skills/unknown-skill/` does not exist
- **THEN** CLI exits with error `skill 'unknown-skill' is not installed`

#### Scenario: Remove with --force skips confirmation

- **WHEN** user runs `memory install-skills remove my-skill`
- **AND** CLI is running in an interactive terminal
- **THEN** CLI prompts `Remove skill 'my-skill'? [y/N]:`
- **AND** only removes if user confirms with `y` or `Y`

#### Scenario: Remove non-interactively

- **WHEN** user runs `memory install-skills remove my-skill --force`
- **THEN** CLI removes the skill without prompting

---

### Requirement: Custom Target Directory

The CLI SHALL support a `--dir` flag to override the default `.agents/skills/` target for all subcommands.

#### Scenario: Install to custom directory

- **WHEN** user runs `memory install-skills install ./my-skill --dir ./custom/path`
- **THEN** CLI installs to `./custom/path/<skill-name>/` instead of `.agents/skills/<skill-name>/`

#### Scenario: Directory created automatically

- **WHEN** the target directory does not exist
- **THEN** CLI creates it (including parent directories) before installing
