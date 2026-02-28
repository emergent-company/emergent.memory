## Context

The project currently contains code, configuration, database columns, and documentation for Langfuse and Signoz integrations. These were used for observability but have been superseded by a new Tempo-based solution. This document outlines the technical plan for their complete removal.

## Goals / Non-Goals

**Goals:**
- Completely remove all traces of Langfuse and Signoz from the repository.
- Ensure the application builds and runs correctly after the removal.
- Update all relevant documentation to reflect the change.

**Non-Goals:**
- Implement any new features in the Tempo-based observability stack. This change is strictly for removal of the old systems.

## Decisions

- **Database Migration**: A new database migration will be created to drop the `langfuse_observation_id` and `langfuse_trace_id` columns from the `monitoring` table. This is a destructive change but is acceptable as the data is no longer being collected or used.
- **File Deletion**: All files and directories related to Langfuse and Signoz will be deleted directly. This includes Go source files, scripts, and documentation.
- **Configuration Cleanup**: Environment variables related to these services will be removed from `.env`, `.env.example`, and any other configuration files.

## Risks / Trade-offs

- **Risk**: Incomplete removal leaves dead code or configuration.
  - **Mitigation**: A thorough search of the codebase has been performed, and a detailed task list will be created to ensure all references are removed.
- **Risk**: The database migration fails.
  - **Mitigation**: The migration will be tested locally before being applied to staging or production environments. A rollback plan will be included in the migration script.
