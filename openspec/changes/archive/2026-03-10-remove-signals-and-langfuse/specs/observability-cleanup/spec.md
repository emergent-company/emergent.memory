## ADDED Requirements

### Requirement: Remove Langfuse and Signoz code
All Go code, scripts, and configurations related to the Langfuse and Signoz integrations SHALL be removed from the codebase.

#### Scenario: Code removal verification
- **WHEN** the codebase is inspected
- **THEN** no files or code referencing Langfuse or Signoz clients, SDKs, or APIs exist.

### Requirement: Remove Langfuse database columns
A database migration SHALL be created to drop the `langfuse_observation_id` and `langfuse_trace_id` columns from the `monitoring` table.

#### Scenario: Database migration
- **WHEN** the migration is applied
- **THEN** the specified columns no longer exist in the `monitoring` table schema.

### Requirement: Clean up environment variables
All environment variables related to Langfuse and Signoz SHALL be removed from `.env`, `.env.example`, and all other configuration files.

#### Scenario: Configuration cleanup
- **WHEN** the configuration files are reviewed
- **THEN** no environment variables starting with `LANGFUSE_` or `SIGNOZ_` are present.
