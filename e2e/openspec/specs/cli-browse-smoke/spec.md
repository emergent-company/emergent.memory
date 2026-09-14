# Spec: cli-browse-smoke

## Purpose

Smoke tests for the `memory browse` TUI command, verifying graceful behavior in non-interactive environments and that help output is well-formed.

## Requirements

### Requirement: Browse exits gracefully without a TTY
The `memory browse` command SHALL detect that stdin/stdout is not an interactive terminal and exit with a non-zero status and a human-readable error message rather than panicking or hanging.

#### Scenario: No-TTY error message
- **WHEN** `memory browse` is invoked with no flags in a non-interactive process (no TTY)
- **THEN** the command exits with a non-zero exit code
- **THEN** the output contains a recognizable message indicating TUI cannot run (e.g. "terminal", "TTY", or usage hint)

### Requirement: Browse --help is well-formed
The `memory browse --help` output SHALL list the `--tempo-url` flag and a description of the TUI.

#### Scenario: Help flag output
- **WHEN** `memory browse --help` is invoked
- **THEN** the command exits 0
- **THEN** the output contains "tempo-url"
- **THEN** the output contains "browse" or "TUI" or "terminal"
