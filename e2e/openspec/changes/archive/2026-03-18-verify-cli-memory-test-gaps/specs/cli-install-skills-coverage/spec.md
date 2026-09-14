## ADDED Requirements

### Requirement: install-memory-skills respects --dir flag
`memory install-memory-skills --dir <path>` SHALL install embedded memory skills into the specified directory rather than the default `.agents/skills/` relative to cwd.

#### Scenario: Skills installed into custom directory
- **WHEN** `memory install-memory-skills --dir <tmpdir>` is invoked
- **THEN** the command exits 0
- **THEN** the target directory contains at least one `memory-*` skill subdirectory
- **THEN** the default `.agents/skills/` in cwd is NOT created

### Requirement: install-memory-skills is idempotent without --force
Running `memory install-memory-skills` twice in the same directory (no `--force`) SHALL succeed on the second run without overwriting existing skill directories and SHALL report that skills were skipped.

#### Scenario: Second run skips existing skills
- **WHEN** `memory install-memory-skills --dir <tmpdir>` is run a first time
- **THEN** it exits 0 and installs skills
- **WHEN** `memory install-memory-skills --dir <tmpdir>` is run a second time without `--force`
- **THEN** the command exits 0
- **THEN** the output contains a message indicating skills were skipped or already exist

### Requirement: install-memory-skills --force overwrites existing skills
`memory install-memory-skills --force --dir <path>` SHALL overwrite any existing skill directories at the target path.

#### Scenario: Force overwrites existing skill directory
- **WHEN** a dummy file is placed inside a skill subdirectory at the target path
- **WHEN** `memory install-memory-skills --force --dir <path>` is run
- **THEN** the command exits 0
- **THEN** the skill directory is replaced with the embedded version (dummy file is gone or overwritten)

### Requirement: Embedded skills from runlog v0.1.2 are valid YAML
Each installed `memory-*` skill directory SHALL contain a valid `skill.yaml` (or equivalent manifest) that is parseable and references the `memory` binary.

#### Scenario: Skill manifest is parseable
- **WHEN** skills are installed via `install-memory-skills`
- **THEN** each `memory-*` subdirectory contains a `skill.yaml` or `*.yaml` file
- **THEN** each manifest file is valid YAML
- **THEN** at least one manifest references the `memory` command
