# Spec: cli-output-formats

## Purpose

Requirements for alternative output formats (CSV, JSON) across CLI commands that support structured output modes.

## Requirements

### Requirement: Projects list supports CSV output
`memory projects list --output csv` SHALL produce a CSV-formatted response with a header row containing field names and one data row per project.

#### Scenario: CSV header row present
- **WHEN** `memory projects list --output csv` is invoked against an authenticated server with at least one project
- **THEN** the command exits 0
- **THEN** the output's first line contains comma-separated column names (e.g. "id", "name")
- **THEN** subsequent lines contain comma-separated values

### Requirement: Status command supports JSON output
`memory status --json` SHALL produce valid JSON containing the CLI version, server health, and authentication mode fields.

#### Scenario: JSON output structure
- **WHEN** `memory status --json` is invoked against an authenticated server
- **THEN** the command exits 0
- **THEN** the output is valid JSON
- **THEN** the JSON contains a field for CLI version or server version
- **THEN** the JSON contains a field indicating authentication status or mode

### Requirement: Graph list supports JSON output
`memory graph list --project <id> --output json` SHALL produce a JSON array of graph objects.

#### Scenario: JSON array output
- **WHEN** `memory graph list --output json` is invoked for an existing project
- **THEN** the command exits 0
- **THEN** the output is a valid JSON array (may be empty)
