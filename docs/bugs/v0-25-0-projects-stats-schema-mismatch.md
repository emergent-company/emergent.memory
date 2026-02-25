# Bug Report: v0.25.0 Projects --stats Fails Due to Schema Mismatch

**Status:** Open  
**Severity:** High  
**Component:** API / Database / CLI  
**Discovered:** 2026-02-25  
**Discovered by:** AI Agent (during release verification)  
**Assigned to:** Unassigned

---

## Summary

CLI command `emergent projects get <name> --stats` fails with database error due to schema/struct mismatch in v0.25.0.

---

## Description

**Actual behavior:**
When running `emergent projects get <name> --stats` or `emergent projects list --stats`, the CLI returns:

```
Error: failed to list projects for name resolution: [500] database_error: Database operation failed
```

**Expected behavior:**
The command should return project information with statistics including document counts, object counts, job summaries, and template pack information.

**Root cause:**
The database schema has a `chunking_config` column in the `kb.projects` table, but the Go struct used for scanning with `--stats` doesn't include this field, causing Bun ORM to fail during row scanning.

---

## Reproduction Steps

1. Deploy v0.25.0 to mcj-emergent server
2. Run: `emergent projects get huma --stats`
3. Observe error: `[500] database_error: Database operation failed`
4. Check server logs: See error `bun: ProjectWithStats does not have column "chunking_config"`

---

## Logs / Evidence

```
2026-02-25T12:42:56.528Z [ERROR] [projects.repo] - failed to list projects error=sql: Scan error on column index 9, name "chunking_config": bun: ProjectWithStats does not have column "chunking_config"
2026-02-25T12:42:56.528Z [ERROR] - request failed method=GET uri=/api/projects status=200 latency=1.304324367s request_id=FskZgPTVbWSBHrBIcLTXUWdCqaDnYTGH error=database_error: Database operation failed (sql: Scan error on column index 9, name "chunking_config": bun: ProjectWithStats does not have column "chunking_config")
2026-02-25T12:42:56.529Z [ERROR] - request error status=500 error=database_error: Database operation failed (sql: Scan error on column index 9, name "chunking_config": bun: ProjectWithStats does not have column "chunking_config")
```

**Log Location:** `mcj-emergent:/var/lib/docker/containers/[emergent-server]/`  
**Timestamp:** 2026-02-25T12:42:56Z

---

## Impact

- **User Impact:** Users cannot use the new `--stats` flag feature introduced in v0.25.0. Regular project listing without `--stats` works fine.
- **System Impact:** No system stability issues. Only affects the stats query path.
- **Frequency:** 100% reproducible when using `--stats` flag
- **Workaround:** Use projects commands without the `--stats` flag. Query statistics separately via individual API endpoints.

---

## Root Cause Analysis

The issue occurs because:

1. The database schema includes `chunking_config` column in `kb.projects` table
2. The Go struct in `apps/server-go/domain/projects/entity.go` has a comment acknowledging `chunking_config` exists but states it should be "added when needed"
3. When `--stats` is used, the query selects all columns including `chunking_config`
4. Bun ORM attempts to scan the result into a struct that doesn't have the `chunking_config` field
5. The scan fails with the error

**Related Files:**

- `apps/server-go/domain/projects/entity.go` - Project struct definition (missing chunking_config field)
- `apps/server-go/domain/projects/repository.go` - Query logic for projects with stats
- Database: `kb.projects` table (has chunking_config column)

**Additional Issue:**
There's also a secondary issue with `schedule_at` column missing from object extraction jobs:

```
ERROR: column "schedule_at" does not exist (SQLSTATE 42703)
```

---

## Proposed Solution

**Option 1: Add missing fields to Project struct (Recommended)**

Add the `chunking_config` field to the Project struct in `entity.go`:

```go
type Project struct {
    bun.BaseModel `bun:"table:kb.projects,alias:proj"`
    // ... existing fields ...
    ChunkingConfig json.RawMessage `bun:"chunking_config,type:jsonb"`
    // Add other missing columns mentioned in comment
}
```

**Option 2: Explicitly exclude columns in query**

Modify the repository query to explicitly select only the columns that exist in the struct when using `--stats`.

**Changes Required:**

1. Update `apps/server-go/domain/projects/entity.go` to include all current schema columns
2. Add migration check or version compatibility handling if needed
3. Fix `schedule_at` issue in object extraction jobs
4. Create regression test for `--stats` functionality

**Testing Plan:**

- [ ] Test `emergent projects list --stats` with multiple projects
- [ ] Test `emergent projects get <name> --stats` with project name
- [ ] Test `emergent projects get <id> --stats` with project ID
- [ ] Verify stats data is accurate (document counts, object counts, etc.)
- [ ] Test without `--stats` flag to ensure no regression
- [ ] Run on server with existing production data

---

## Related Issues

- Related to v0.25.0 release
- Blocks adoption of new `--stats` CLI feature
- Related to incomplete struct/schema synchronization

---

## Notes

This appears to be an incomplete feature implementation where:

- The CLI flag was added
- The database schema already had the columns
- The Go struct was never updated to match the schema
- The feature wasn't tested end-to-end before release

The comment in `entity.go` explicitly mentions that `chunking_config` should be added "when needed" - it's now needed for the `--stats` feature.

**Recommendation:** Before the next release, implement automated testing that exercises all CLI commands with all flags against a production-like database to catch schema mismatches.

---

**Last Updated:** 2026-02-25 by AI Agent
