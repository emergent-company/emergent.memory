# Capability Spec: skills-install-list-all

## Purpose

`runlog skills list` prints a table of all supported AI agent tools — their tool ID, marker path, install directory, and whether they are currently detected in the project — without installing anything.

## Requirements

### Requirement: Print all supported tools
The command SHALL print one row per entry in the tool registry, regardless of detection status.

#### Scenario: All tools listed
- **WHEN** `runlog skills list` is run
- **THEN** every tool ID in the registry appears in the output

### Requirement: Show detection status
Each row SHALL include whether the tool's marker currently exists in the working directory.

#### Scenario: Detected tool annotated
- **WHEN** `.opencode/` exists in the project root
- **THEN** the `opencode` row shows `yes` (or equivalent) in the detected column

#### Scenario: Undetected tool annotated
- **WHEN** `.claude/` does not exist
- **THEN** the `claude` row shows `no` (or equivalent) in the detected column

### Requirement: Show marker and install paths
Each row SHALL include the tool's marker path and install directory (both relative to project root).

#### Scenario: Paths shown for opencode
- **WHEN** `runlog skills list` is run
- **THEN** the `opencode` row shows marker `.opencode` and install dir `.opencode/skills`

### Requirement: No side effects
The command SHALL NOT create, modify, or delete any files or directories.
