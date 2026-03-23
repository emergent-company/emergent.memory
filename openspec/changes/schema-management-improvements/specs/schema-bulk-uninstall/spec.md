## ADDED Requirements

### Requirement: Bulk uninstall schemas from a project
The `memory schemas uninstall` command SHALL accept two new flags for bulk operations:
- `--all-except <schema-id,...>` — uninstall every currently installed schema except the ones listed
- `--keep-latest` — uninstall all but the most recently installed schema (by `installed_at` descending)

Both flags are mutually exclusive with each other and with passing an explicit `<schema-id>` positional argument. The command prints each uninstalled schema name/ID before removing it and shows a final count. A `--dry-run` flag prints what would be removed without making changes.

#### Scenario: Uninstall all except specified IDs
- **WHEN** user runs `memory schemas uninstall --all-except abc123,def456`
- **THEN** every installed schema whose ID is not `abc123` or `def456` is uninstalled
- **AND** the output lists each removed schema name and ID

#### Scenario: Keep only latest schema
- **WHEN** user runs `memory schemas uninstall --keep-latest`
- **THEN** all installed schemas except the one with the most recent `installed_at` timestamp are uninstalled
- **AND** the output confirms which schema was kept

#### Scenario: Dry run shows what would be removed
- **WHEN** user runs `memory schemas uninstall --all-except abc123 --dry-run`
- **THEN** the CLI prints each schema that would be uninstalled but makes no API calls that modify data
- **AND** the output line prefix is `[dry-run]`

#### Scenario: Flags are mutually exclusive
- **WHEN** user provides both `--all-except` and `--keep-latest` flags
- **THEN** the CLI returns an error: "flags --all-except and --keep-latest are mutually exclusive"

#### Scenario: Positional arg conflicts with bulk flags
- **WHEN** user provides a positional `<schema-id>` along with `--all-except` or `--keep-latest`
- **THEN** the CLI returns an error: "cannot combine a specific schema ID with bulk uninstall flags"

#### Scenario: Nothing to uninstall
- **WHEN** the filter leaves no schemas to remove (e.g., all are in the except list)
- **THEN** the CLI outputs "Nothing to uninstall." and exits successfully
