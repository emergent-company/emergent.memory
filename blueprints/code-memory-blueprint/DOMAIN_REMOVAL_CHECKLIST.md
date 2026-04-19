# Domain Removal Checklist

When removing a Go domain package, run these steps IN ORDER to keep the graph consistent.

## Before merging the graph branch

1. **Enumerate ALL graph object types** for the domain — not just Domain/Service/APIEndpoint:
   - `Method`, `SQLQuery`, `Job`, `Event`, `ConfigVar`, `ExtDep`
   - Query: `memory graph objects list --type <Type> --json | grep <domain-name>`

2. **Run `graph-logic-audit`** — catches structural orphans (JOB_NO_ENDPOINT, etc.)
   ```
   MEMORY_API_KEY=... MEMORY_PROJECT_ID=... MEMORY_SERVER_URL=... \
     go run ./blueprints/code-memory-blueprint/tools/graph-logic-audit/...
   ```

3. **Run `graph-sync-routes --dry-run`** — catches stale APIEndpoint objects whose handlers no longer exist in code
   ```
   MEMORY_API_KEY=... MEMORY_PROJECT_ID=... MEMORY_SERVER_URL=... \
     go run ./blueprints/code-memory-blueprint/tools/graph-sync-routes/... \
     --repo . --dry-run
   ```
   Look for the removed domain in the **STALE GRAPH ENDPOINTS** section.

## After merging PR + graph branch

4. **Re-run `graph-logic-audit`** — verify 0 findings for removed domain.
5. **Re-run `graph-sync-routes --dry-run`** — verify removed domain absent from stale list.
6. **Run `graph-complexity`** — verify removed domain no longer appears.

## Root cause of missed objects

The `DataSourceSyncJob` (Job), 4 superadmin sync-job endpoints (APIEndpoint),
4 githubapp Methods, 2 datasource Methods, 2 datasource SQLQueries, and 1 ConfigVar
were all missed because:
- Graph branch only deleted Domain/Service/APIEndpoint objects explicitly
- `graph-sync-routes` and `graph-logic-audit` were not run before merging
- No checklist existed for non-obvious object types
