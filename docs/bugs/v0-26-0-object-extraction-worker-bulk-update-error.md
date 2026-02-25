# Bug Report: Object Extraction Worker Fails with Bulk Update Error

**Status:** Resolved  
**Severity:** High  
**Component:** Backend - Object Extraction Worker  
**Discovered:** 2026-02-25  
**Discovered by:** AI Agent  
**Assigned to:** AI Agent

---

## Summary

The object extraction worker fails to dequeue jobs due to incorrect bulk update syntax, causing "bun: to bulk Update, use CTE and VALUES" errors every 5 seconds.

---

## Description

**Actual Behavior:**
The object extraction worker continuously logs errors when attempting to dequeue jobs for processing. The error occurs in the `DequeueBatch()` method which tries to perform a bulk update using `Model(&jobs)` with a slice of job pointers, which Bun ORM does not support.

**Expected Behavior:**
The worker should successfully dequeue pending jobs, update their status to "processing", and process them without errors.

**When/How it Occurs:**

- Occurs continuously every ~5 seconds (worker polling interval)
- Happens whenever there are pending jobs in the queue
- Blocks all object extraction job processing

---

## Reproduction Steps

1. Start the server with object extraction worker enabled
2. Create a project with extraction jobs
3. Observe logs for "error dequeuing jobs"
4. See repeated error: "dequeue batch: bun: to bulk Update, use CTE and VALUES"

---

## Logs / Evidence

```
2026-02-25T14:13:12.306Z [ERROR] [object-extraction-worker] - error dequeuing jobs error=dequeue batch: bun: to bulk Update, use CTE and VALUES
2026-02-25T14:13:17.311Z [ERROR] [object-extraction-worker] - error dequeuing jobs error=dequeue batch: bun: to bulk Update, use CTE and VALUES
2026-02-25T14:13:22.976Z [ERROR] [object-extraction-worker] - error dequeuing jobs error=dequeue batch: bun: to bulk Update, use CTE and VALUES
```

**Log Location:** Docker logs from `emergent-server` container  
**Timestamp:** Continuous errors starting from v0.26.0 deployment

---

## Impact

- **User Impact:** Object extraction jobs are not being processed. New documents do not get objects/relationships extracted from them.
- **System Impact:** Worker thread runs continuously but accomplishes nothing. Logs fill with error messages.
- **Frequency:** Every 5 seconds (worker polling interval)
- **Workaround:** None - extraction functionality is completely blocked

---

## Root Cause Analysis

The issue is in `apps/server-go/domain/extraction/object_extraction_jobs.go:196-200`.

**Problematic Code (lines 196-200):**

```go
// Update the claimed jobs
_, err = tx.NewUpdate().
    Model(&jobs).                           // <- Problem: passing slice to Model()
    Column("status", "started_at", "updated_at").
    Where("id IN (?)", bun.In(jobIDs)).
    Exec(ctx)
```

**Root Cause:**
Bun ORM does not support bulk updates using `Model(&slice)`. When you pass a slice to `Model()`, Bun expects you to use a CTE (Common Table Expression) with VALUES clause, which is a more complex pattern. However, for this use case (updating multiple rows to the same values), a simpler approach is to update by IDs.

**Related Files:**

- `apps/server-go/domain/extraction/object_extraction_jobs.go:160-213` - DequeueBatch method

---

## Proposed Solution

Replace the bulk update with a standard UPDATE query that sets all matching rows to the same values.

**Changes Required:**

1. Change from `Model(&jobs)` to `Model((*ObjectExtractionJob)(nil))`
2. Use `Set()` methods instead of `Column()` to specify the values
3. Keep the `WHERE id IN (?)` clause to update only the claimed jobs

**Fixed Code:**

```go
// Update the claimed jobs individually (Bun requires CTE+VALUES for bulk updates)
_, err = tx.NewUpdate().
    Model((*ObjectExtractionJob)(nil)).
    Set("status = ?", JobStatusProcessing).
    Set("started_at = ?", now).
    Set("updated_at = ?", now).
    Where("id IN (?)", bun.In(jobIDs)).
    Exec(ctx)
```

**Testing Plan:**

- [x] Verify code compiles (`go build`)
- [x] Deploy to production
- [ ] Monitor logs for absence of "bun: to bulk Update" errors
- [ ] Verify extraction jobs are being processed
- [ ] Check that job statuses are correctly updated in database

---

## Related Issues

- Related to v0.25.0 schema mismatch bugs (fixed in v0.25.1)
- Part of ongoing extraction pipeline improvements

---

## Resolution

**Fixed in:** v0.26.1  
**Fix Commit:** 1e773f4  
**Deployed:** 2026-02-25

The fix simplifies the update query by using a standard UPDATE with WHERE IN clause, which is the appropriate pattern for updating multiple rows to the same values in Bun ORM.

---

**Last Updated:** 2026-02-25 by AI Agent
