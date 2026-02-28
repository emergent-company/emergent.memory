## Why

The project has migrated to a Tempo-based observability stack, making the existing Langfuse and Signoz integrations redundant. Removing them will simplify the codebase, reduce configuration overhead, and eliminate unnecessary dependencies and database columns.

## What Changes

- **BREAKING**: Remove database columns `langfuse_observation_id` and `langfuse_trace_id` from the `monitoring` table.
- Remove all Langfuse-related environment variables from `.env` and configuration files.
- Delete all Go code related to Langfuse integration in `apps/server-go`.
- Remove the `mcp-langfuse-wrapper.sh` script.
- Purge all documentation related to Langfuse and Signoz.
- Remove Signoz environment variables and commented-out configurations.

## Capabilities

### New Capabilities
- `observability-cleanup`: Defines the requirements for completely removing the Langfuse and Signoz integrations.

### Modified Capabilities
- `observability`: This existing capability will be modified to remove all references to Langfuse and Signoz, focusing solely on the new Tempo-based stack.

## Impact

- **Database**: Requires a database migration to drop the two Langfuse-related columns.
- **Backend**: The Go server will be simplified by removing all Langfuse-specific logic.
- **Configuration**: `.env` files will be cleaner, and dead configurations will be removed.
- **Documentation**: All developer and operational documentation will be updated to reflect the removal of these systems.
