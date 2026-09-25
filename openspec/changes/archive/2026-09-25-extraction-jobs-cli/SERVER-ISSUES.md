# Server-Side Issues Found During Testing

This document describes server-side bugs discovered during e2e test execution for the `extraction-jobs-cli` change.

## Issue 1: Extraction Jobs Never Complete

**Test affected**: `TestCLIInstalled_DocumentExtractionWithSchema`

**Symptom**: Extraction jobs remain in "running" status indefinitely, even though the worker completes processing.

**Evidence** (from `docker logs emergent-server` at 2026-03-18T13:16:31):

```
[object-extraction-worker] - quality check orphan_rate=0 threshold=0.3 orphan_count=0 entity_count=1 relationship_count=24
[object-extraction-worker] - quality check passed, escalating orphan_rate=0
[object-extraction-worker] - extraction pipeline completed entity_count=1 relationship_count=24
...
[object-extraction-jobs] - object extraction job completed id=18bd3d46-c2cb-4191-a33e-23cf32a492b2 objectsCreated=0 successfulItems=0
```

The worker logs "object extraction job completed" but the job status in the database never transitions from "running" to "completed".

**Root cause**: Database status update is missing or failing after the extraction worker completes processing.

**Test behavior**: Test polls `memory extraction jobs get <id>` every 5 seconds for 6 minutes, sees "running" on every poll, then times out.

**Impact**: High — extraction jobs appear to never finish from the user's perspective, even when the work is done.

---

## Issue 2: PDF Conversion Fails with Kreuzberg Validation Error

**Test affected**: `TestCLIInstalled_DocumentConversion`

**Symptom**: PDF documents uploaded to the server remain in "pending" conversion status indefinitely.

**Evidence** (from `docker logs emergent-server` at 2026-03-18T13:16:46):

```
[document.parsing.worker] - processing document parsing job job_id=58a9248e-8644-4f46-8c5e-009544c7a8e1 source_type=file_upload filename=sample.pdf
[kreuzberg] - kreuzberg error filename=sample.pdf status_code=400 message=Validation error: No files provided for extraction detail=
[document.parsing.worker] - document extraction failed method=kreuzberg error=kreuzberg extraction: Validation error: No files provided for extraction
[document.parsing.jobs] - document parsing job failed, scheduled retry job_id=58a9248e-8644-4f46-8c5e-009544c7a8e1 retry=2 max_retries=3 next_retry_at=2026-03-18 13:17:16
```

After 3 retries (all with the same error), the job is moved to the dead letter queue:

```
[document.parsing.jobs] - document parsing job moved to dead letter queue job_id=58a9248e-8644-4f46-8c5e-009544c7a8e1 attempts=4 error=kreuzberg extraction: Validation error: No files provided for extraction
```

**Root cause**: The server is not correctly passing the uploaded PDF file to Kreuzberg for conversion. Kreuzberg receives an empty file list.

**Test behavior**: Test polls `memory documents get <id>` every 3 seconds for 3 minutes, sees `"conversionStatus": "pending"` on every poll, then times out.

**Impact**: High — PDF documents cannot be converted to text, making them unusable for extraction.

---

## Issue 3: Foreign Key Constraint Violations During Entity Creation

**Test affected**: `TestCLIInstalled_DocumentExtractionWithSchema`

**Symptom**: The extraction worker successfully extracts entities and relationships but fails to persist them to the database.

**Evidence** (from `docker logs emergent-server` at 2026-03-18T13:16:31):

```
[object-extraction-worker] - failed to create graph object name=Sarah Chen type=Person error=database_error: Database operation failed (ERROR: insert or update on table "graph_objects" violates foreign key constraint "FK_ff6be6062964f2462ee8e8b2ac1" (SQLSTATE 23503))
[object-extraction-worker] - relationship references unknown temp_id source_ref=person_sarah_chen target_ref=org_acme_corp src_found=false dst_found=false
```

All 24 relationships extracted by the worker fail to persist because they reference temporary IDs that were not persisted during entity creation.

**Root cause**: The foreign key constraint suggests that one of the required references (likely `project_id`, `document_id`, or `schema_id`) is invalid or missing when inserting graph objects.

**Test behavior**: Extraction job completes with `objectsCreated=0 successfulItems=0`, so test sees no extracted entities when querying `graph objects list --type Person`.

**Impact**: Medium-High — Extraction pipeline runs but produces no usable results.

---

## Environment

- **Server**: emergent-server container (Up 7 hours, healthy)
- **Port**: 127.0.0.1:4003 → 3002/tcp
- **Auth mode**: standalone (X-API-Key: e2e-test-user)
- **Test env**: `.env.local-fixed`

---

## Recommendations

1. **Issue 1**: Add database status update after extraction job completion (search for "object extraction job completed" log and add status transition)
2. **Issue 2**: Debug Kreuzberg file passing logic — ensure uploaded files are correctly staged and sent to Kreuzberg API
3. **Issue 3**: Add detailed logging for graph object insertion failures, verify all required foreign key references are present before insert

---

## Test Status

Both e2e tests (`TestCLIInstalled_DocumentExtractionWithSchema` and `TestCLIInstalled_DocumentConversion`) are **correctly implemented** and **correctly fail** due to these server-side bugs.

The CLI implementation (`memory extraction jobs` commands) is **working correctly** and successfully exercises the admin API endpoints.
