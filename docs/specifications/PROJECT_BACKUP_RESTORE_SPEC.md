# Project Backup & Restore Feature Specification

**Version:** 1.0.0-draft  
**Last Updated:** 2026-02-11  
**Status:** Draft - Under Review  
**Authors:** Development Team

---

## Table of Contents

1. [Overview](#overview)
2. [Objectives](#objectives)
3. [Scope](#scope)
4. [Data Model](#data-model)
5. [Architecture Decisions](#architecture-decisions)
6. [Incremental Backups](#incremental-backups)
7. [Performance Considerations](#performance-considerations)
8. [Edge Cases & Failure Modes](#edge-cases--failure-modes)
9. [Security & Privacy](#security--privacy)
10. [API Specification](#api-specification)
11. [Implementation Plan](#implementation-plan)
12. [Open Questions](#open-questions)

---

## 1. Overview

This specification defines a comprehensive backup and restore system that allows users to create backups of their projects stored in organization-level MinIO buckets. Backups are managed as a library of snapshots that can be downloaded, shared, or restored at any time.

### 1.1 Architecture Overview

**Storage Model:**

- Backups are stored in MinIO bucket: `backups` (separate from `documents` bucket)
- Organized by organization: `backups/{orgId}/{backupId}/backup.zip`
- Each organization has a dedicated namespace for their backups
- Backups persist until explicitly deleted or quota limit reached

**Access Model:**

- Backups are **organization-level resources** (not project-level)
- Users with `org:admin` scope can manage backups for any project in their org
- Backup library accessible via UI and API
- Supports multiple backups per project (versioned snapshots)

### 1.2 Use Cases

- **Data Portability**: Users can move projects between environments (dev → staging → production)
- **Disaster Recovery**: Full project restoration after data loss or corruption
- **Project Cloning**: Duplicate projects for testing or branching workflows
- **Compliance**: Data export for GDPR/data portability requirements
- **Archival**: Long-term storage of completed projects
- **Backup Library**: Maintain multiple snapshots of projects over time
- **Org-wide Management**: Administrators can manage all project backups in one place

---

## 2. Objectives

### 2.1 Functional Requirements

- **FR-1**: Export complete project data including configuration, database records, and files
- **FR-2**: Support projects of any size (from 1MB to 100GB+)
- **FR-3**: Maintain data integrity and relationships during export/import
- **FR-4**: Provide progress tracking for long-running operations
- **FR-5**: Store backups in organization-scoped MinIO buckets
- **FR-6**: Allow selective restore (e.g., only documents, not chat history)
- **FR-7**: Validate backup integrity before and after transfer
- **FR-8**: Support multiple backups per project (backup library/history)
- **FR-9**: Provide backup lifecycle management (retention policies, quotas)
- **FR-10**: Enable backup sharing within organization
- **FR-11**: Support backup download for external storage/archival

### 2.2 Non-Functional Requirements

- **NFR-1**: Backup creation should not significantly impact system performance
- **NFR-2**: Memory usage must remain constant regardless of backup size (streaming)
- **NFR-3**: Backup format must be forward-compatible with future schema versions
- **NFR-4**: Restore operation must be atomic (all-or-nothing)
- **NFR-5**: Support concurrent backup operations for different projects
- **NFR-6**: Provide clear error messages and recovery guidance
- **NFR-7**: Backup storage must be isolated per organization (tenant isolation)
- **NFR-8**: Backup quota enforcement must prevent storage abuse
- **NFR-9**: Backup listing/retrieval must be performant (<100ms for list operations)

---

## 3. Scope

### 3.1 Included

**Database Data:**

- ✅ Project configuration (`kb.projects`)
- ✅ Documents metadata (`kb.documents`)
- ✅ Document chunks with embeddings (`kb.chunks`)
- ✅ Graph objects and relationships (`kb.graph_objects`, `kb.graph_relationships`)
- ✅ Extraction jobs history (`kb.object_extraction_jobs`, `kb.object_extraction_logs`)
- ✅ Chat conversations and messages (`kb.chat_conversations`, `kb.chat_messages`)
- ✅ Template pack configurations (`kb.project_template_packs`)
- ✅ Project memberships (`kb.project_memberships`)

**Files:**

- ✅ All uploaded documents from MinIO storage
- ✅ Original filenames and metadata

**Configuration:**

- ✅ Chunking settings
- ✅ Extraction configuration
- ✅ Auto-extract settings
- ✅ Chat prompt templates
- ✅ KB purpose description

### 3.2 Excluded

**Organization-Level Data:**

- ❌ Organization metadata (separate feature)
- ❌ User profiles (tied to auth system)
- ❌ API tokens (security - must be regenerated)
- ❌ Audit logs (compliance - separate retention)

**System State:**

- ❌ In-progress extraction jobs (must complete or cancel before backup)
- ❌ Cached embeddings (regenerated on restore)
- ❌ Search indexes (rebuilt automatically)

**External Integrations:**

- ❌ Integration credentials (security - must be reconfigured)
- ❌ Data source sync state (re-sync required)

---

## 4. Data Model

### 4.1 Storage Structure

**MinIO Bucket Organization:**

```
backups/                                    # Dedicated backup bucket
├── {orgId}/                                # Organization namespace
│   ├── {backupId}/                         # Individual backup
│   │   ├── backup.zip                      # The actual backup archive
│   │   └── metadata.json                   # Quick metadata (no need to open ZIP)
│   ├── {backupId}/
│   │   ├── backup.zip
│   │   └── metadata.json
│   └── ...
└── {anotherOrgId}/
    └── ...
```

**Database: kb.backups table**

```sql
CREATE TABLE kb.backups (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES kb.orgs(id),
    project_id UUID NOT NULL REFERENCES kb.projects(id),
    project_name TEXT NOT NULL,                    -- Snapshot of project name

    -- Storage
    storage_key TEXT NOT NULL,                     -- backups/{orgId}/{backupId}/backup.zip
    size_bytes BIGINT NOT NULL,

    -- Status
    status TEXT NOT NULL,                          -- 'creating', 'ready', 'failed', 'deleted'
    progress INTEGER DEFAULT 0,                    -- 0-100
    error_message TEXT,

    -- Metadata
    backup_type TEXT NOT NULL DEFAULT 'full',      -- 'full', 'incremental' (future)
    includes JSONB NOT NULL DEFAULT '{}',          -- { documents: true, chat: true, ... }

    -- Statistics
    stats JSONB,                                   -- { documents: 150, chunks: 3000, ... }

    -- Lifecycle
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by UUID REFERENCES core.user_profiles(id),
    completed_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,                        -- Auto-delete after this date
    deleted_at TIMESTAMPTZ,                        -- Soft delete

    -- Checksums
    manifest_checksum TEXT,
    content_checksum TEXT
);

CREATE INDEX idx_backups_org_project ON kb.backups(organization_id, project_id);
CREATE INDEX idx_backups_status ON kb.backups(status) WHERE deleted_at IS NULL;
CREATE INDEX idx_backups_expires ON kb.backups(expires_at) WHERE deleted_at IS NULL;
```

### 4.2 Backup Archive Structure (ZIP Contents)

```
backup.zip                                  # Stored in MinIO
├── manifest.json                           # Backup metadata (5-10 KB)
├── project/
│   └── config.json                         # Project settings (1-5 KB)
├── database/
│   ├── documents.ndjson                    # 100-10K records (~1MB-100MB)
│   ├── chunks.ndjson                       # 1K-1M records (~10MB-1GB)
│   ├── graph_objects.ndjson                # 100-100K records (~1MB-100MB)
│   ├── graph_relationships.ndjson          # 100-500K records (~1MB-500MB)
│   ├── chat_conversations.ndjson           # 10-1K records (~100KB-10MB)
│   ├── chat_messages.ndjson                # 100-100K records (~1MB-100MB)
│   ├── extraction_jobs.ndjson              # 10-1K records (~100KB-10MB)
│   └── project_memberships.ndjson          # 1-100 records (~1KB-100KB)
└── files/
    ├── {uuid}-{sanitized-filename-1}.pdf   # Original files (1KB-500MB each)
    ├── {uuid}-{sanitized-filename-2}.docx
    └── ...
```

**Total Size Estimates:**

- Small project: 10MB-100MB (10 docs, no chat)
- Medium project: 100MB-1GB (100 docs, active chat)
- Large project: 1GB-10GB (1000 docs, extensive graph)
- Enterprise project: 10GB-100GB+ (10K+ docs, full extraction)

### 4.3 Metadata File (metadata.json in MinIO)

Quick access metadata without opening ZIP:

```json
{
  "backup_id": "uuid",
  "organization_id": "uuid",
  "project_id": "uuid",
  "project_name": "My Project",
  "created_at": "2026-02-11T08:30:00Z",
  "created_by": "user-uuid",
  "status": "ready",
  "size_bytes": 52428800,
  "stats": {
    "documents": 150,
    "chunks": 3000,
    "graph_objects": 450,
    "files": 150
  },
  "checksums": {
    "manifest": "sha256:...",
    "content": "sha256:..."
  }
}
```

### 4.4 Manifest Schema (manifest.json inside ZIP)

```json
{
  "version": "1.0.0",
  "schema_version": "20251210_000000",
  "created_at": "2026-02-11T08:30:00Z",
  "backup_type": "full",
  "project": {
    "id": "uuid",
    "name": "Project Name",
    "organization_id": "uuid"
  },
  "contents": {
    "documents": { "count": 150, "size_bytes": 52428800 },
    "chunks": { "count": 3000, "size_bytes": 15728640 },
    "graph_objects": { "count": 450, "size_bytes": 2097152 },
    "graph_relationships": { "count": 1200, "size_bytes": 524288 },
    "files": { "count": 150, "size_bytes": 52428800 }
  },
  "checksums": {
    "manifest": "sha256:...",
    "database": "sha256:...",
    "files": "sha256:..."
  },
  "metadata": {
    "server_version": "2.0.0",
    "go_version": "1.24",
    "backup_duration_ms": 45000
  }
}
```

---

## 5. Architecture Decisions

### 5.1 Archive Format: ZIP vs TAR.GZ

**Decision: Use ZIP format**

| Criteria         | ZIP              | TAR.GZ                             | Rationale                                          |
| ---------------- | ---------------- | ---------------------------------- | -------------------------------------------------- |
| Random Access    | ✅ Yes           | ❌ No                              | Can extract single files without decompressing all |
| Streaming        | ✅ Yes           | ✅ Yes                             | Both support streaming creation                    |
| Compression      | ✅ Per-file      | ✅ Global                          | Per-file allows parallel compression               |
| Cross-platform   | ✅ Native        | ⚠️ Requires tools                  | Works on Windows without extra tools               |
| Resume Support   | ✅ Easier        | ⚠️ Harder                          | ZIP central directory enables resume               |
| Standard Library | ✅ `archive/zip` | ✅ `archive/tar` + `compress/gzip` | Both available                                     |

**Why ZIP wins:** Better user experience (native support on all platforms), random access enables selective restore, easier resume implementation.

### 5.2 Data Format: NDJSON vs SQL Dump vs Binary

**Decision: Use NDJSON (Newline-Delimited JSON)**

| Criteria          | NDJSON      | SQL Dump               | Binary (Protocol Buffers) |
| ----------------- | ----------- | ---------------------- | ------------------------- |
| Human-readable    | ✅ Yes      | ⚠️ Partial             | ❌ No                     |
| Streaming         | ✅ Yes      | ✅ Yes                 | ✅ Yes                    |
| Language-agnostic | ✅ Yes      | ❌ PostgreSQL-specific | ⚠️ Requires schema        |
| Editing           | ✅ Easy     | ⚠️ Possible            | ❌ Hard                   |
| Size efficiency   | ⚠️ Medium   | ⚠️ Medium              | ✅ Small                  |
| Schema evolution  | ✅ Flexible | ❌ Brittle             | ⚠️ Versioned              |

**Why NDJSON wins:**

- Human-readable for debugging
- Easy to process line-by-line (memory efficient)
- JSON is universal (no SQL dialect issues)
- Flexible schema evolution (add fields without breaking)
- Simple to implement streaming parser/writer

### 5.3 Streaming vs Buffered

**Decision: Streaming to MinIO Storage**

```
Backup Request → Go Handler → Streaming Pipeline → MinIO Upload
                              ↓
                         [Project Config]
                              ↓
                         [Database Rows] ← Cursor Pagination
                              ↓
                         [Storage Files] ← MinIO Stream (documents bucket)
                              ↓
                         [ZIP Writer] ← No temp files
                              ↓
                         MinIO Upload (backups bucket)
                              ↓
                         Database Record (kb.backups)
```

**Download Flow (when user requests):**

```
Download Request → Go Handler → MinIO Stream → HTTP Response
                              ↓
                    Generate Presigned URL (1 hour expiry)
                              ↓
                    Return URL to client (client downloads directly from MinIO)
```

**Memory Budget:**

- Per-backup creation: **< 50MB constant** (regardless of backup size)
- Chunk buffer: 32KB (network writes)
- ZIP compression buffer: 4MB (per file being compressed)
- Database cursor: 1000 rows at a time (~1-5MB)
- Total concurrent backups: 10 (500MB total peak)
- Download: 0MB (presigned URL, direct MinIO access)

### 5.4 Restore Strategy: Atomic vs Progressive

**Decision: Hybrid Approach**

1. **Validation Phase** (fast, read-only)

   - Check manifest compatibility
   - Verify checksums
   - Validate target project state
   - **Fail fast** if incompatible

2. **Transaction Phase** (atomic, database only)

   - Begin PostgreSQL transaction
   - Import all database records
   - Commit or rollback (all-or-nothing)

3. **File Phase** (progressive with cleanup)
   - Upload files to storage
   - Track progress in database
   - If interrupted: resume or cleanup partial uploads

**Rationale:** Database must be atomic (no partial graph), but files can be progressive (too large for single transaction).

---

## 6. Incremental Backups

### 6.1 Problem Statement

**Storage Efficiency Challenge:**

- Full backups of large projects (50GB) are expensive
- Storing 30 days of history: 30 × 50GB = **1.5TB per project** (impractical!)
- Most days, only 1-5% of data changes
- Incremental backups: 50GB (full) + 30 × 500MB (incremental) = **65GB total** (96% savings!)

**Change Detection Challenge:**

- How do we know what changed since last backup?
- Need to detect: additions, modifications, deletions
- Must work across database records AND files
- Must be reliable (no missed changes)

### 6.2 Change Detection Approaches

| Approach                      | Complexity | Completeness              | Performance      | Recommended     |
| ----------------------------- | ---------- | ------------------------- | ---------------- | --------------- |
| **Timestamp-based**           | Low        | 85% (misses hard deletes) | Fast             | ✅ v1.0         |
| **Soft-delete tracking**      | Low        | 95% (with schema updates) | Fast             | ✅ v1.0         |
| **Change Data Capture (CDC)** | High       | 100%                      | Medium           | ⏸️ v2.0         |
| **PostgreSQL WAL**            | Very High  | 100%                      | Fast             | ❌ Too complex  |
| **Snapshot comparison**       | Medium     | 100%                      | Slow (full scan) | ❌ Not scalable |

### 6.3 Recommended Approach: Timestamp + Soft Deletes

**For Database Records:**

```sql
-- Incremental export query for each table
SELECT * FROM kb.documents
WHERE project_id = $1
  AND (
    updated_at > $2                    -- Modified since last backup
    OR created_at > $2                 -- Created since last backup
    OR (deleted_at IS NOT NULL         -- Soft-deleted since last backup
        AND deleted_at > $2)
  );
```

**For Files (MinIO):**

- Documents table already tracks file metadata
- Use same timestamp query on `kb.documents`
- Changed files detected via `updated_at`
- Deleted files detected via `deleted_at`
- File content changes trigger `updated_at` update

**Soft Delete Requirements:**

Current schema audit (need to verify):

```sql
-- Tables that MUST have deleted_at for incremental backups:
kb.documents           -- ✅ Likely has it
kb.chunks              -- ❓ Check if exists
kb.graph_objects       -- ❓ Check if exists
kb.graph_relationships -- ❓ Check if exists
kb.chat_conversations  -- ❓ Check if exists
kb.chat_messages       -- ❓ Check if exists
```

**Schema Updates Needed:**

```sql
-- Add deleted_at to tables that lack it
ALTER TABLE kb.chunks ADD COLUMN deleted_at TIMESTAMPTZ;
ALTER TABLE kb.graph_objects ADD COLUMN deleted_at TIMESTAMPTZ;
-- ... etc

-- Add indexes for incremental queries
CREATE INDEX idx_documents_updated ON kb.documents(project_id, updated_at);
CREATE INDEX idx_chunks_updated ON kb.chunks(document_id, updated_at);
CREATE INDEX idx_graph_objects_updated ON kb.graph_objects(project_id, updated_at);
```

### 6.4 Incremental Backup Data Model

**Extended kb.backups table:**

```sql
ALTER TABLE kb.backups ADD COLUMN backup_type TEXT NOT NULL DEFAULT 'full';
-- Values: 'full' | 'incremental'

ALTER TABLE kb.backups ADD COLUMN parent_backup_id UUID REFERENCES kb.backups(id);
-- For incremental: points to immediate parent backup
-- For full: NULL

ALTER TABLE kb.backups ADD COLUMN baseline_backup_id UUID REFERENCES kb.backups(id);
-- For incremental: points to the full backup that started this chain
-- For full: NULL or self-reference

ALTER TABLE kb.backups ADD COLUMN change_window JSONB;
-- { "from": "2026-02-10T00:00:00Z", "to": "2026-02-11T00:00:00Z" }
-- Timestamp range this incremental covers

CREATE INDEX idx_backups_parent ON kb.backups(parent_backup_id);
CREATE INDEX idx_backups_baseline ON kb.backups(baseline_backup_id);
```

**Backup Chain Example:**

```
Full Backup (Feb 1)      → baseline_backup_id: NULL, parent_backup_id: NULL
  ↓
Incremental 1 (Feb 2)    → baseline_backup_id: Feb 1, parent_backup_id: Feb 1
  ↓
Incremental 2 (Feb 3)    → baseline_backup_id: Feb 1, parent_backup_id: Feb 2
  ↓
Incremental 3 (Feb 4)    → baseline_backup_id: Feb 1, parent_backup_id: Feb 3
  ↓
Full Backup (Feb 8)      → baseline_backup_id: NULL, parent_backup_id: NULL
  ↓
Incremental 4 (Feb 9)    → baseline_backup_id: Feb 8, parent_backup_id: Feb 8
```

### 6.5 Incremental Archive Structure

**Full Backup ZIP:**

```
backup.zip
├── manifest.json              # type: "full"
├── project/config.json
├── database/
│   ├── documents.ndjson       # ALL documents
│   ├── chunks.ndjson          # ALL chunks
│   └── ...
└── files/
    ├── file1.pdf              # ALL files
    └── file2.docx
```

**Incremental Backup ZIP:**

```
backup.zip
├── manifest.json              # type: "incremental", parent_id, baseline_id
├── changes/
│   ├── summary.json           # Change statistics
│   ├── documents.ndjson       # Only changed/new/deleted documents
│   ├── chunks.ndjson          # Only changed/new/deleted chunks
│   └── ...
└── files/
    ├── new-file.pdf           # Only new/modified files
    └── updated-file.docx
```

**Incremental Manifest:**

```json
{
  "version": "1.0.0",
  "backup_type": "incremental",
  "parent_backup_id": "uuid-of-previous-backup",
  "baseline_backup_id": "uuid-of-full-backup",
  "change_window": {
    "from": "2026-02-10T08:30:00Z",
    "to": "2026-02-11T08:30:00Z"
  },
  "changes": {
    "documents": {
      "added": 5,
      "modified": 12,
      "deleted": 3,
      "total_changed": 20
    },
    "chunks": {
      "added": 120,
      "modified": 45,
      "deleted": 30,
      "total_changed": 195
    },
    "files": {
      "added": 5,
      "modified": 2,
      "deleted": 1,
      "total_changed": 8,
      "size_bytes": 5242880
    }
  },
  "restore_requirements": {
    "requires_backups": [
      "uuid-of-full-backup",
      "uuid-of-incremental-1",
      "uuid-of-incremental-2",
      "uuid-of-this-backup"
    ],
    "restore_order": ["full", "inc1", "inc2", "inc3"]
  }
}
```

### 6.6 Restore from Incremental Chain

**Algorithm:**

1. **Identify Chain**

   ```sql
   -- Find all backups needed to restore target incremental
   WITH RECURSIVE backup_chain AS (
     SELECT id, parent_backup_id, baseline_backup_id, backup_type, created_at
     FROM kb.backups
     WHERE id = $target_backup_id

     UNION ALL

     SELECT b.id, b.parent_backup_id, b.baseline_backup_id, b.backup_type, b.created_at
     FROM kb.backups b
     JOIN backup_chain bc ON b.id = bc.parent_backup_id
   )
   SELECT * FROM backup_chain ORDER BY created_at ASC;
   -- Results: [Full, Inc1, Inc2, Inc3, Target]
   ```

2. **Validate Chain Integrity**

   - All backups in chain exist and are `status = 'ready'`
   - No gaps in chain (each incremental's parent exists)
   - Checksums valid for all backups

3. **Restore Sequence**

   ```
   a. Restore full backup (baseline)
   b. Apply incremental 1 changes (overlay)
   c. Apply incremental 2 changes (overlay)
   d. Apply incremental 3 changes (overlay)
   ```

4. **Change Application Logic**
   ```javascript
   // For each incremental backup in chain:
   for (const record of incrementalBackup.documents) {
     if (record.deleted_at) {
       // Delete from target
       DELETE FROM kb.documents WHERE id = record.id;
     } else if (existsInTarget(record.id)) {
       // Update existing
       UPDATE kb.documents SET ... WHERE id = record.id;
     } else {
       // Insert new
       INSERT INTO kb.documents ...;
     }
   }
   ```

### 6.7 Storage Savings Analysis

**50GB Project, 1% Daily Change Rate:**

| Backup Strategy                      | 30 Days Storage                   | Savings           |
| ------------------------------------ | --------------------------------- | ----------------- |
| **30 Full Backups**                  | 30 × 50GB = **1.5TB**             | Baseline          |
| **1 Full + 29 Incremental**          | 50GB + 29 × 500MB = **64.5GB**    | **96% reduction** |
| **Weekly Full (4) + Daily Inc (26)** | 4 × 50GB + 26 × 500MB = **213GB** | **86% reduction** |

**Restore Time Comparison:**

| Scenario         | Backups to Process     | Time Estimate | Failure Risk       |
| ---------------- | ---------------------- | ------------- | ------------------ |
| Full only        | 1 backup (50GB)        | 1 hour        | Low                |
| 7-day inc chain  | 1 full + 6 inc (53GB)  | 1.2 hours     | Medium             |
| 30-day inc chain | 1 full + 29 inc (65GB) | 1.5 hours     | High (chain break) |

**Recommendation:** Weekly full + daily incremental (max 7-backup chain)

### 6.8 Backup Scheduling Strategy

**Proposed Policy:**

```yaml
backup_schedule:
  full_backup:
    frequency: weekly
    day: sunday
    time: '02:00 UTC'
    retention: 4 weeks

  incremental_backup:
    frequency: daily
    time: '02:00 UTC'
    retention: 30 days
    max_chain_length: 7

  auto_full_after:
    - chain_length: 6 # Force full if chain too long
    - chain_age_days: 7 # Force full weekly
    - storage_threshold: 80% # Force full if incrementals become large
```

**Smart Full Backup Triggers:**

1. **Time-based**: Every 7 days (Sunday)
2. **Chain-based**: After 6 incrementals
3. **Size-based**: If incremental > 20% of full size (lots of changes)
4. **Manual**: User can force full backup anytime

### 6.9 Edge Cases

**Case 1: Incremental Larger Than Full**

- If 50%+ of data changed, incremental might be 25GB+ (vs 50GB full)
- **Solution:** Auto-trigger full backup if incremental > 40% of full size

**Case 2: Broken Chain (Missing Backup)**

- Parent backup was deleted
- **Solution:**
  - Prevent deletion of backups with children
  - If chain broken, can only restore up to gap
  - Show clear error: "Cannot restore: requires backup {uuid} which was deleted"

**Case 3: Time Zone Issues**

- Timestamp comparisons must use UTC
- **Solution:** Always store timestamps in UTC, convert for display

**Case 4: Clock Skew**

- Server clock adjusted backward
- **Solution:** Use `completed_at` of previous backup, not current time

**Case 5: Concurrent Modifications During Backup**

- Data changes while incremental backup is running
- **Solution:**
  - Use snapshot timestamp (backup start time)
  - Document that incremental is "best effort" snapshot
  - Full backups use BEGIN TRANSACTION for consistency

### 6.10 Implementation Phases

**Phase 1: Foundation (v1.0)**

- ✅ Full backups only
- ✅ Backup chains data model (schema ready for incremental)
- ✅ Timestamp-based change detection (soft deletes)

**Phase 2: Incremental Basic (v1.1)**

- ⏸️ Manual incremental backup creation
- ⏸️ Timestamp-based change export
- ⏸️ Restore from chain (up to 7 backups)
- ⏸️ Validation of chain integrity

**Phase 3: Automation (v1.2)**

- ⏸️ Scheduled backups (weekly full, daily incremental)
- ⏸️ Smart full backup triggers
- ⏸️ Auto-cleanup of old chains

**Phase 4: Advanced (v2.0)**

- ⏸️ CDC-based change detection (100% accurate)
- ⏸️ Incremental restore optimization (parallel processing)
- ⏸️ Backup compression improvements

### 6.11 API Changes for Incremental

**Create Incremental Backup:**

```http
POST /api/v1/projects/:projectId/backups
Content-Type: application/json

{
  "backup_type": "incremental",  # New field
  "parent_backup_id": "uuid",    # Optional: auto-select latest if omitted
  "include_deleted": true
}

Response: 202 Accepted
{
  "id": "backup-uuid",
  "backup_type": "incremental",
  "parent_backup_id": "parent-uuid",
  "baseline_backup_id": "full-backup-uuid",
  "change_window": {
    "from": "2026-02-10T08:30:00Z",
    "to": "2026-02-11T08:30:00Z"
  },
  "status": "creating"
}
```

**Get Restore Chain:**

```http
GET /api/v1/organizations/:orgId/backups/:backupId/chain

Response: 200 OK
{
  "target_backup_id": "uuid",
  "chain": [
    {
      "id": "full-uuid",
      "backup_type": "full",
      "size_bytes": 52428800,
      "created_at": "2026-02-01T02:00:00Z",
      "status": "ready"
    },
    {
      "id": "inc1-uuid",
      "backup_type": "incremental",
      "size_bytes": 1048576,
      "created_at": "2026-02-02T02:00:00Z",
      "status": "ready"
    },
    {
      "id": "inc2-uuid",
      "backup_type": "incremental",
      "size_bytes": 524288,
      "created_at": "2026-02-03T02:00:00Z",
      "status": "ready"
    }
  ],
  "total_size_bytes": 54001664,
  "chain_valid": true,
  "estimated_restore_time_seconds": 3600
}
```

### 6.12 Open Questions for Incremental

1. **Should we support incremental-to-incremental chains, or always require a full backup?**

   - Current proposal: Always require full baseline
   - Alternative: Allow incremental chains indefinitely (complex)

2. **What's the max chain length before forcing full?**

   - Proposal: 7 backups (1 full + 6 incremental)
   - Rationale: Balances storage vs restore complexity

3. **How do we handle schema migrations in incremental backups?**

   - If schema changes between full and incremental
   - Proposal: Store schema version in manifest, auto-migrate on restore

4. **Should we allow restoring partial chains?**

   - e.g., Full + Inc1 + Inc2, skip Inc3
   - Proposal: No, chains must be contiguous

5. **Do we compress incremental backups differently?**
   - Incrementals are smaller, compression overhead may not be worth it
   - Proposal: Same compression (consistency)

---

## 7. Performance Considerations

### 7.1 Backup Performance

**Target Metrics:**
| Project Size | Records | Files | Target Time | Max Memory | Network |
|--------------|---------|-------|-------------|------------|---------|
| Small (10MB) | 1K | 10 | < 5 sec | 20MB | 2 MB/s |
| Medium (100MB) | 10K | 100 | < 30 sec | 30MB | 3.3 MB/s |
| Large (1GB) | 100K | 1K | < 5 min | 40MB | 3.3 MB/s |
| Enterprise (10GB) | 1M | 10K | < 30 min | 50MB | 5.5 MB/s |
| Huge (50GB) | 5M | 50K | < 2 hours | 50MB | 6.9 MB/s |

**Optimization Strategies:**

1. **Database Export** (~30% of time)

   - Cursor-based pagination (1000 rows/batch)
   - Parallel export of independent tables
   - Skip empty tables
   - Exclude soft-deleted records

2. **File Streaming** (~60% of time)

   - Direct MinIO → ZIP streaming (no disk writes)
   - Parallel file downloads (5 concurrent)
   - Skip compression for already-compressed files (.pdf, .jpg, .zip)
   - Use DEFLATE compression level 6 (balance speed vs size)

3. **Network Transfer** (~10% of time)
   - HTTP chunked transfer encoding
   - Gzip content-encoding (if client supports)
   - Resume support via HTTP Range requests

**Bottleneck Analysis:**

- **50GB scenario**: 50,000 files × 1MB avg
  - Sequential download: 50K × 20ms latency = 16 minutes (latency bound)
  - Parallel (5 workers): 50K / 5 × 20ms = 3.3 minutes
  - Network transfer: 50GB / 100 Mbps = 67 minutes (network bound)
  - **Critical path: Network bandwidth**, not CPU or disk

### 7.2 Restore Performance

**Target Metrics:**
| Project Size | Target Time | Max Memory | Validation | Import | Files Upload |
|--------------|-------------|------------|------------|--------|--------------|
| Small (10MB) | < 10 sec | 30MB | 1s | 3s | 6s |
| Medium (100MB) | < 1 min | 50MB | 2s | 10s | 48s |
| Large (1GB) | < 10 min | 100MB | 5s | 60s | 540s |
| Enterprise (10GB) | < 1 hour | 150MB | 10s | 600s | 3000s |
| Huge (50GB) | < 5 hours | 200MB | 30s | 3000s | 15000s |

**Optimization Strategies:**

1. **Validation Phase** (~1% of time)

   - Stream ZIP central directory (don't extract yet)
   - Verify checksums incrementally
   - Check manifest version compatibility

2. **Database Import** (~10-20% of time)

   - Single transaction (all tables)
   - Disable triggers temporarily
   - Defer constraint checks until commit
   - Bulk INSERT (1000 rows/batch)
   - Use `COPY` for large tables

3. **File Upload** (~80-90% of time)
   - Parallel uploads (10 concurrent)
   - Stream ZIP → MinIO directly
   - Skip duplicate files (by hash)
   - Resume on failure

**Concurrency Limits:**

- Database: 1 connection (transaction isolation)
- MinIO uploads: 10 concurrent
- CPU cores: Use all available for decompression

### 7.3 System Impact

**During Backup:**

- Database load: +5-10% (cursor queries)
- MinIO load: +20-30% (read operations)
- Network: Saturates user's download bandwidth
- CPU: +10-15% (compression)
- **Mitigation:** Rate limiting (max 2 concurrent backups per org)

**During Restore:**

- Database load: +30-50% (bulk inserts)
- MinIO load: +40-60% (write operations)
- CPU: +20-30% (decompression, validation)
- **Mitigation:** Queue system (1 restore at a time per project)

---

## 8. Edge Cases & Failure Modes

### 8.1 Backup Failures

| Scenario                                              | Detection                   | Recovery                                    | User Impact                         |
| ----------------------------------------------------- | --------------------------- | ------------------------------------------- | ----------------------------------- |
| **Database connection lost during export**            | Query error after 3 retries | Abort, return 500                           | Partial download, retry             |
| **MinIO file missing (deleted after metadata query)** | 404 on file download        | Log warning, continue without file          | Incomplete backup, note in manifest |
| **Out of disk space on client**                       | HTTP connection reset       | Abort, cleanup                              | User retries with more space        |
| **Network timeout during transfer**                   | Client disconnect           | Abort operation                             | User resumes download               |
| **Concurrent modification during backup**             | Inconsistent snapshot       | Accept (eventual consistency)               | Backup reflects state at start time |
| **User cancels mid-backup**                           | HTTP request cancellation   | Cleanup temp resources                      | No side effects                     |
| **50GB+ backup exceeds HTTP timeout**                 | 504 Gateway Timeout         | Implement chunked backup (separate feature) | User downloads in parts             |

**Critical Decision:** Backups are **eventually consistent snapshots**, not transactional. Accept minor inconsistencies (e.g., document added during backup might be missing).

### 8.2 Restore Failures

| Scenario                            | Detection                   | Recovery                                | User Impact                      |
| ----------------------------------- | --------------------------- | --------------------------------------- | -------------------------------- |
| **Invalid ZIP file**                | Checksum mismatch           | Reject before any writes                | Error message, no changes        |
| **Incompatible schema version**     | Manifest validation         | Reject with upgrade path                | User updates backup tool         |
| **Target project already has data** | Check before import         | Require empty project OR merge strategy | User creates new project         |
| **Database constraint violation**   | INSERT fails                | Rollback entire transaction             | No changes, detailed error       |
| **MinIO upload fails mid-restore**  | Upload error                | Retry 3x, then mark file as failed      | Database restored, files partial |
| **Out of disk space on server**     | MinIO error                 | Rollback database, cleanup files        | No changes, user notified        |
| **Duplicate object keys**           | Unique constraint violation | Skip duplicates OR merge                | Partial restore, report skipped  |
| **Network interruption**            | Connection lost             | Retry from last checkpoint              | Resume possible                  |

**Critical Decision:** Database import must be **atomic** (all-or-nothing), but file uploads can be **resumable** (track progress in database).

### 8.3 Edge Cases

**Large Files:**

- Problem: Single 5GB video file exceeds memory budget
- Solution: Stream file without buffering, use ZIP64 format
- Test: Create backup with 10GB single file

**Many Small Files:**

- Problem: 100K files × 20ms latency = 33 minutes
- Solution: Parallel downloads (5-10 workers), batch metadata queries
- Test: Create backup with 50K 10KB files

**Unicode Filenames:**

- Problem: Filename encoding in ZIP (UTF-8 vs CP437)
- Solution: Always use UTF-8 flag in ZIP headers
- Test: Files named `测试文档.pdf`, `Тест.docx`

**Duplicate Filenames:**

- Problem: Two files with same name (different UUIDs)
- Solution: Include UUID in filename: `{uuid}-{original-name}`
- Test: Upload two files both named `document.pdf`

**Symbolic Links/Special Files:**

- Problem: Shouldn't exist, but what if they do?
- Solution: Detect and skip with warning
- Test: Create document pointing to symlink (shouldn't be possible)

**Time Zones:**

- Problem: Timestamps in different zones
- Solution: Always use UTC in manifest and NDJSON
- Test: Backup in Europe, restore in Asia

**Soft-Deleted Records:**

- Problem: Should deleted items be included?
- Solution: Exclude by default, add `--include-deleted` flag
- Test: Backup project with deleted documents

**Circular Relationships:**

- Problem: Graph A → B → C → A
- Solution: NDJSON format handles this naturally (no traversal needed)
- Test: Create circular reference, verify restore

**Embeddings:**

- Problem: 768-dimensional vectors are large
- Solution: Include in backup (required for search), compress well
- Test: Verify search works after restore

---

## 9. Security & Privacy

### 9.1 Access Control

**Backup Creation:**

- Requires: `org:admin` OR `project:admin` scope
- Rate limit: 10 backups per organization per day
- Audit log: Record who created backup, project, when, size

**Backup Access:**

- List: Requires `org:member` scope (can see all org backups)
- Download: Requires `org:member` scope (can download any org backup)
- Delete: Requires `org:admin` scope
- Tenant isolation: Cannot access other organization's backups

**Restore Operation:**

- Requires: `org:admin` OR `project:admin` scope
- Cannot restore to another org's project (cross-tenant isolation)
- Audit log: Record who restored, source backup, target project

**Storage Isolation:**

- MinIO bucket path includes org ID: `backups/{orgId}/...`
- Database queries always filter by `organization_id`
- Presigned URLs include organization verification

### 9.2 Data Protection

**In Transit:**

- HTTPS only (TLS 1.3)
- No downgrade to HTTP
- Certificate pinning for MinIO

**At Rest (Backup File):**

- User responsible for encryption (outside scope)
- Recommendation: Encrypt ZIP with password or GPG
- Future: Built-in AES-256 encryption option

**Sensitive Data:**

- **MUST exclude:**

  - API tokens
  - Integration credentials
  - User passwords (not stored in project)
  - Private keys

- **MUST redact:**
  - Email addresses in chat (optional flag)
  - PII in documents (optional flag)

### 9.3 Compliance

**GDPR:**

- Backups are "data export" (Article 20)
- Must include all user data
- Must be in "commonly used format" (ZIP + JSON ✓)
- Must complete within 30 days (our target: instant)

**Data Retention:**

- Backups created by users are user-managed
- System does not store backups (user downloads immediately)
- Audit logs retained per policy (90 days)

---

## 10. API Specification

### 10.1 List Organization Backups

```http
GET /api/v1/organizations/:orgId/backups?project_id={projectId}&limit=20&cursor={cursor}
Authorization: Bearer {token}

Response: 200 OK
{
  "backups": [
    {
      "id": "backup-uuid",
      "project_id": "project-uuid",
      "project_name": "My Project",
      "status": "ready",
      "size_bytes": 52428800,
      "created_at": "2026-02-11T08:30:00Z",
      "created_by": "user-uuid",
      "expires_at": "2026-03-11T08:30:00Z",
      "stats": {
        "documents": 150,
        "chunks": 3000,
        "graph_objects": 450,
        "files": 150
      }
    }
  ],
  "total": 45,
  "next_cursor": "cursor-value"
}
```

### 10.2 Create Backup

```http
POST /api/v1/projects/:projectId/backups
Authorization: Bearer {token}
Content-Type: application/json

{
  "include_deleted": false,
  "include_chat": true,
  "retention_days": 30,
  "description": "Pre-migration backup"
}

Response: 202 Accepted
Location: /api/v1/organizations/:orgId/backups/:backupId

{
  "id": "backup-uuid",
  "organization_id": "org-uuid",
  "project_id": "project-uuid",
  "status": "creating",
  "progress": 0,
  "created_at": "2026-02-11T08:30:00Z",
  "expires_at": "2026-03-11T08:30:00Z"
}
```

### 10.3 Get Backup Status

```http
GET /api/v1/organizations/:orgId/backups/:backupId
Authorization: Bearer {token}

Response: 200 OK
{
  "id": "backup-uuid",
  "organization_id": "org-uuid",
  "project_id": "project-uuid",
  "project_name": "My Project",
  "status": "ready",
  "progress": 100,
  "size_bytes": 52428800,
  "storage_key": "backups/org-uuid/backup-uuid/backup.zip",
  "created_at": "2026-02-11T08:30:00Z",
  "completed_at": "2026-02-11T08:32:15Z",
  "created_by": "user-uuid",
  "expires_at": "2026-03-11T08:30:00Z",
  "stats": {
    "documents": 150,
    "chunks": 3000,
    "graph_objects": 450,
    "graph_relationships": 1200,
    "files": 150,
    "total_size_bytes": 52428800
  },
  "checksums": {
    "manifest": "sha256:abc...",
    "content": "sha256:def..."
  }
}
```

### 10.4 Download Backup

```http
GET /api/v1/organizations/:orgId/backups/:backupId/download
Authorization: Bearer {token}

Response: 302 Found
Location: https://minio.emergent.ai/backups/org-uuid/backup-uuid/backup.zip?X-Amz-Algorithm=...&X-Amz-Expires=3600...

# Client follows redirect to presigned MinIO URL (1 hour expiry)
# Direct download from MinIO (no server memory usage)
```

**Alternative (Stream through API):**

```http
GET /api/v1/organizations/:orgId/backups/:backupId/download?direct=false
Authorization: Bearer {token}

Response: 200 OK
Content-Type: application/zip
Content-Disposition: attachment; filename="backup-my-project-2026-02-11.zip"
Content-Length: 52428800

[Binary ZIP data streamed from MinIO through API server]
```

### 10.5 Delete Backup

```http
DELETE /api/v1/organizations/:orgId/backups/:backupId
Authorization: Bearer {token}

Response: 204 No Content

# Soft delete in database, actual MinIO deletion happens async
```

**Status Polling:**

```http
GET /api/v1/projects/:projectId/backups/:backupId

Response: 200 OK
{
  "id": "backup-uuid",
  "status": "ready",
  "size_bytes": 52428800,
  "created_at": "2026-02-11T08:30:00Z",
  "download_url": "/api/v1/projects/:projectId/backups/:backupId/download",
  "expires_at": "2026-02-11T09:30:00Z"
}
```

**Download:**

```http
GET /api/v1/projects/:projectId/backups/:backupId/download
Authorization: Bearer {token}

Response: 200 OK
Content-Type: application/zip
Content-Disposition: attachment; filename="backup-{projectName}-{timestamp}.zip"
Content-Length: 52428800
Transfer-Encoding: chunked

[Binary ZIP data stream]
```

### 10.6 Restore Backup

```http
POST /api/v1/projects/:projectId/restore
Authorization: Bearer {token}
Content-Type: multipart/form-data

{
  "file": <binary ZIP data>,
  "strategy": "require_empty",
  "validate_only": false
}

Response: 202 Accepted
{
  "id": "restore-uuid",
  "status": "validating",
  "progress": 0,
  "created_at": "2026-02-11T08:35:00Z"
}
```

**Progress Tracking:**

```http
GET /api/v1/projects/:projectId/restores/:restoreId

Response: 200 OK
{
  "id": "restore-uuid",
  "status": "importing_database",
  "progress": 45,
  "phase": "database",
  "stats": {
    "documents_imported": 75,
    "files_uploaded": 0,
    "total_files": 150
  },
  "errors": [],
  "created_at": "2026-02-11T08:35:00Z",
  "completed_at": null
}
```

### 10.7 WebSocket Progress Updates

```javascript
// Frontend
const ws = new WebSocket('wss://api.emergent.ai/ws/backups/{backupId}');

ws.onmessage = (event) => {
  const update = JSON.parse(event.data);
  console.log(`Progress: ${update.progress}%`);
};

// Server sends:
{
  "type": "progress",
  "phase": "exporting_files",
  "progress": 67,
  "bytes_transferred": 35000000,
  "total_bytes": 52428800,
  "current_file": "document-123.pdf"
}
```

---

## 11. Implementation Plan

### Phase 1: Core Backup (Week 1-2)

- [ ] Database export service (NDJSON)
- [ ] Streaming ZIP writer
- [ ] Basic manifest generation
- [ ] Single-table export/import
- [ ] Unit tests for streaming

### Phase 2: File Integration (Week 3)

- [ ] MinIO file streaming
- [ ] Parallel file downloads
- [ ] ZIP64 support (large files)
- [ ] Memory profiling (50MB limit)

### Phase 3: Restore Foundation (Week 4)

- [ ] ZIP validation
- [ ] Manifest parser
- [ ] Database import (single transaction)
- [ ] Constraint validation

### Phase 4: Production Hardening (Week 5-6)

- [ ] Progress tracking
- [ ] Error recovery
- [ ] Rate limiting
- [ ] Audit logging
- [ ] E2E tests (1MB, 100MB, 1GB scenarios)

### Phase 5: UX & Monitoring (Week 7-8)

- [ ] Frontend UI
- [ ] WebSocket progress
- [ ] Resumable downloads
- [ ] Metrics & alerts
- [ ] Documentation

---

## 12. Open Questions

### 11.1 Technical Decisions

**Q1: Should we support incremental backups?**

- Complexity: High (need to track changes)
- Value: Medium (most users do full backups rarely)
- **Recommendation:** Defer to v2.0

**Q2: Should we support backup scheduling?**

- Complexity: Medium (cron job system)
- Value: High (automated disaster recovery)
- **Recommendation:** Add in v1.1 (separate feature)

**Q3: What's the max backup size we support?**

- Technical limit: ZIP64 = 16 exabytes
- Practical limit: Network transfer time
- **Proposal:** Soft limit 100GB (warn user), hard limit 1TB (reject)

**Q4: Should restore be idempotent?**

- Problem: Running restore twice with same file
- Solution: Track restore ID in database, detect duplicates
- **Recommendation:** Yes, skip if already restored

**Q5: How do we handle schema migrations during restore?**

- Problem: Backup from v1.0, restore to v2.0 with new columns
- Solution: Auto-migrate manifest schema, provide default values
- **Recommendation:** Support N-1 version compatibility

### 11.2 Product Decisions

**Q6: What should the storage quota be per organization?**

- Free tier: 10GB backup storage per org
- Paid tier: 100GB-1TB based on plan
- After quota: Delete oldest backups OR block new backups
- **Recommendation:** Start with 10GB, implement quota enforcement

**Q7: What should the default retention period be?**

- Options: 7 days, 30 days, 90 days, indefinite
- Consider: Storage costs, user expectations, compliance
- **Recommendation:** 30 days default, configurable per backup, max 90 days free tier

**Q8: Should we support partial restore?**

- Use case: Only restore documents, not chat history
- Complexity: Medium (selective import)
- **Recommendation:** Add in v1.2 (nice-to-have)

**Q9: How do we handle backup cleanup?**

- Expired backups: Cron job runs daily, soft-deletes expired backups
- Soft-deleted backups: Hard-delete from MinIO after 7 days (grace period)
- Over-quota: Prevent new backups OR auto-delete oldest
- **Recommendation:** Grace period for accidental deletions, clear warnings before quota reached

**Q10: Should backups be visible across projects in the org?**

- Current proposal: Yes, org-level resource
- Alternative: Only visible to project members
- **Recommendation:** Org-level visibility (simplifies management), but log who accesses what

---

## Appendices

### A. Comparable Systems

- **GitHub Export:** Generates TAR.GZ, includes repos + wiki + issues
- **Notion Export:** ZIP with Markdown + CSV, no binary files
- **Confluence Export:** HTML + attachments, preserves structure
- **WordPress Export:** XML + uploads ZIP, separate files
- **MongoDB Backup:** BSON format, binary efficient but opaque

### B. References

- [RFC 6713: The 'application/zlib' and 'application/gzip' Media Types](https://www.rfc-editor.org/rfc/rfc6713)
- [ZIP File Format Specification](https://pkware.cachefly.net/webdocs/casestudies/APPNOTE.TXT)
- [NDJSON Specification](http://ndjson.org/)
- [Go archive/zip Package](https://pkg.go.dev/archive/zip)

---

**Next Steps:**

1. Review this specification with stakeholders
2. Validate performance assumptions with prototype
3. Get feedback on edge cases we missed
4. Approve and begin Phase 1 implementation
