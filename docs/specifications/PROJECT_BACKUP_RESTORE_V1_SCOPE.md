# Project Backup & Restore - v1.0 Implementation Scope

**Status:** Ready for Implementation  
**Full Spec:** See `PROJECT_BACKUP_RESTORE_SPEC.md`  
**Last Updated:** 2026-02-11

---

## v1.0 Scope: Full Backups Only

### ✅ What We're Building in v1.0

#### 1. **Organization-Level Backup Storage**

- Backups stored in MinIO `backups` bucket
- Organized by org: `backups/{orgId}/{backupId}/backup.zip`
- Database tracking in `kb.backups` table
- Multiple backups per project (backup library)

#### 2. **Full Backup Creation**

- Export complete project data:
  - Project configuration
  - All database records (documents, chunks, graph, chat)
  - All files from MinIO storage
- Streaming ZIP creation (constant 50MB memory)
- Progress tracking
- Async job processing

#### 3. **Backup Management**

- List all org backups (filterable by project)
- Get backup details and status
- Download backups (presigned URLs to MinIO)
- Delete backups (soft delete with 7-day grace period)

#### 4. **Full Restore**

- Validate backup integrity (checksums, manifest)
- Atomic database import (all-or-nothing)
- Progressive file upload with resume capability
- Restore to new or existing project

#### 5. **Lifecycle Management**

- Retention policies (30-day default)
- Auto-expiration and cleanup (cron jobs)
- Quota enforcement (10GB per org)
- Soft delete with grace period

#### 6. **Access Control & Audit**

- Org-level permissions (`org:admin`, `org:member`)
- Tenant isolation (org-scoped storage)
- Audit logging (who created/accessed/deleted)

---

## ⏸️ Deferred to Future Versions

### v1.1 - Incremental Backups (Future)

- Timestamp-based change detection
- Backup chains (full + incrementals)
- Smart scheduling (weekly full, daily incremental)
- 96% storage savings for large projects
- **See Section 6 of full spec for complete design**

### v1.2 - Advanced Features (Future)

- Scheduled automatic backups
- Partial restore (selective data)
- Backup compression improvements
- Cross-org backup sharing

### v2.0 - Enterprise (Future)

- CDC-based change detection (100% accurate)
- Multi-region backup replication
- Encrypted backups (built-in)
- Backup versioning and tagging

---

## Database Schema (v1.0)

### New Table: `kb.backups`

```sql
CREATE TABLE kb.backups (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES kb.orgs(id),
    project_id UUID NOT NULL REFERENCES kb.projects(id),
    project_name TEXT NOT NULL,

    -- Storage
    storage_key TEXT NOT NULL,              -- backups/{orgId}/{backupId}/backup.zip
    size_bytes BIGINT NOT NULL,

    -- Status
    status TEXT NOT NULL,                   -- 'creating', 'ready', 'failed', 'deleted'
    progress INTEGER DEFAULT 0,             -- 0-100
    error_message TEXT,

    -- Metadata
    backup_type TEXT NOT NULL DEFAULT 'full',
    includes JSONB NOT NULL DEFAULT '{}',   -- { documents: true, chat: true, ... }

    -- Statistics
    stats JSONB,                            -- { documents: 150, chunks: 3000, ... }

    -- Lifecycle
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by UUID REFERENCES core.user_profiles(id),
    completed_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ,

    -- Checksums
    manifest_checksum TEXT,
    content_checksum TEXT
);

CREATE INDEX idx_backups_org_project ON kb.backups(organization_id, project_id);
CREATE INDEX idx_backups_status ON kb.backups(status) WHERE deleted_at IS NULL;
CREATE INDEX idx_backups_expires ON kb.backups(expires_at) WHERE deleted_at IS NULL;
```

**Note:** Schema includes `backup_type` column for future incremental support, but v1.0 only uses `'full'`.

---

## MinIO Bucket Structure

```
backups/
├── {orgId}/
│   ├── {backupId}/
│   │   ├── backup.zip          # The backup archive
│   │   └── metadata.json       # Quick metadata (size, stats, checksums)
│   └── ...
└── ...
```

---

## ZIP Archive Structure (Full Backup)

```
backup.zip
├── manifest.json                    # Backup metadata, version, checksums
├── project/
│   └── config.json                  # Project settings
├── database/
│   ├── documents.ndjson             # All documents
│   ├── chunks.ndjson                # All chunks
│   ├── graph_objects.ndjson         # All graph objects
│   ├── graph_relationships.ndjson   # All relationships
│   ├── chat_conversations.ndjson    # All conversations
│   ├── chat_messages.ndjson         # All messages
│   ├── extraction_jobs.ndjson       # All extraction jobs
│   └── project_memberships.ndjson   # All memberships
└── files/
    ├── {uuid}-{filename1}.pdf       # All uploaded files
    ├── {uuid}-{filename2}.docx
    └── ...
```

---

## API Endpoints (v1.0)

### Backup Management

```http
# List org backups
GET /api/v1/organizations/:orgId/backups?project_id={projectId}&limit=20&cursor={cursor}

# Create backup
POST /api/v1/projects/:projectId/backups
Body: { "include_deleted": false, "include_chat": true, "retention_days": 30 }

# Get backup status
GET /api/v1/organizations/:orgId/backups/:backupId

# Download backup (presigned URL)
GET /api/v1/organizations/:orgId/backups/:backupId/download

# Delete backup
DELETE /api/v1/organizations/:orgId/backups/:backupId
```

### Restore

```http
# Restore from backup
POST /api/v1/projects/:projectId/restore
Body: multipart/form-data with backup.zip file

# Get restore status
GET /api/v1/projects/:projectId/restores/:restoreId
```

### WebSocket (Progress Updates)

```javascript
wss://api.emergent.ai/ws/backups/{backupId}
// Real-time progress for backup creation/restore
```

---

## Performance Targets (v1.0)

| Project Size | Backup Time | Restore Time | Memory Usage |
| ------------ | ----------- | ------------ | ------------ |
| 10MB         | < 5s        | < 10s        | 20MB         |
| 100MB        | < 30s       | < 1min       | 30MB         |
| 1GB          | < 5min      | < 10min      | 40MB         |
| 10GB         | < 30min     | < 1hr        | 50MB         |
| 50GB         | < 2hrs      | < 5hrs       | 50MB         |

**Key Design Principles:**

- **Streaming architecture** - Constant memory regardless of size
- **Parallel processing** - Multiple file downloads/uploads
- **Presigned URLs** - Direct MinIO access for downloads
- **Async jobs** - Don't block API requests

---

## Implementation Phases

### Phase 1: Database & Storage Foundation (Week 1)

- [ ] Create `kb.backups` table migration
- [ ] MinIO bucket setup and configuration
- [ ] Backup service scaffolding (Go domain module)
- [ ] Storage key generation logic

### Phase 2: Backup Creation (Week 2)

- [ ] Database export (NDJSON streaming)
- [ ] Streaming ZIP writer
- [ ] MinIO file streaming (documents → ZIP)
- [ ] Manifest generation
- [ ] Progress tracking
- [ ] Unit tests

### Phase 3: Backup Management (Week 3)

- [ ] List backups endpoint
- [ ] Get backup details endpoint
- [ ] Download endpoint (presigned URLs)
- [ ] Delete endpoint (soft delete)
- [ ] Metadata.json generation

### Phase 4: Restore (Week 4)

- [ ] ZIP validation
- [ ] Manifest parsing
- [ ] Database import (atomic transaction)
- [ ] File upload to MinIO
- [ ] Progress tracking
- [ ] Constraint validation

### Phase 5: Lifecycle & Polish (Week 5)

- [ ] Retention policy cron job
- [ ] Quota enforcement
- [ ] Cleanup job (soft-deleted backups)
- [ ] Audit logging
- [ ] Error handling & recovery

### Phase 6: Frontend & Testing (Week 6)

- [ ] Backup list UI
- [ ] Create backup UI
- [ ] Download backup UI
- [ ] Restore backup UI
- [ ] WebSocket progress integration
- [ ] E2E tests (10MB, 100MB, 1GB)
- [ ] Load testing

---

## Success Criteria

### Functional

- ✅ Create full backup of 1GB project in < 5 minutes
- ✅ Restore backup successfully to new project
- ✅ Download backup via presigned URL
- ✅ List and filter backups by project
- ✅ Auto-expire backups after 30 days
- ✅ Enforce 10GB org quota

### Non-Functional

- ✅ Memory usage < 50MB constant
- ✅ Support concurrent backups (10 max)
- ✅ Tenant isolation (org-scoped storage)
- ✅ Checksums validate integrity
- ✅ Audit logs track all operations

### User Experience

- ✅ Clear progress indicators
- ✅ Estimated time remaining
- ✅ Meaningful error messages
- ✅ Resume capability for large restores
- ✅ Download works on all platforms (ZIP native support)

---

## Out of Scope (v1.0)

- ❌ Incremental backups
- ❌ Scheduled automatic backups
- ❌ Partial restore (selective data)
- ❌ Built-in backup encryption
- ❌ Backup versioning/tagging
- ❌ Cross-org backup sharing
- ❌ Backup to external storage (S3, GCS)
- ❌ Backup compression options

---

## Technical Decisions (v1.0)

### Architecture

- **Format:** ZIP (cross-platform, random access)
- **Data Format:** NDJSON (human-readable, streaming)
- **Storage:** MinIO (S3-compatible, org-scoped)
- **Download:** Presigned URLs (no server memory)
- **Restore:** Hybrid (atomic DB + progressive files)

### Go Implementation

- **Module:** `apps/server-go/domain/backup`
- **Dependencies:**
  - `archive/zip` (standard library)
  - `storage.Service` (MinIO client)
  - `database` (Bun ORM)
  - `jobs` (async processing)

### Database

- **ORM:** Bun
- **Migration:** Goose
- **Transaction:** Single transaction for all DB imports
- **Constraints:** Deferred until commit

---

## Open Questions (Resolve Before Implementation)

### Q1: Project State During Backup

- Should we prevent project modifications during backup?
- **Recommendation:** No - document that backup is "best effort snapshot"

### Q2: Restore Target Project

- Require empty project, or allow overwrite?
- **Recommendation:** Require empty project in v1.0 (safer)

### Q3: Download Method

- Presigned URL (direct MinIO) vs stream through API?
- **Recommendation:** Presigned URL (faster, no server load)

### Q4: Backup Naming

- Auto-generate name or user-provided?
- **Recommendation:** Auto-generate: `backup-{projectName}-{timestamp}.zip`

### Q5: Retention Enforcement

- Block new backups when over quota, or auto-delete oldest?
- **Recommendation:** Warn at 80%, block at 100%

---

## Dependencies

### External Services

- MinIO (storage)
- PostgreSQL (metadata)
- Job queue (async processing)

### Internal Modules

- `storage.Service` (MinIO client)
- `projects.Repository` (project data)
- `documents.Repository` (documents)
- `graph.Repository` (graph objects)
- `chat.Repository` (conversations)

---

## Next Steps

1. ✅ **Specification approved** - This document
2. ⏭️ **Create database migration** - `kb.backups` table
3. ⏭️ **Implement backup service** - Core streaming logic
4. ⏭️ **Build API endpoints** - CRUD operations
5. ⏭️ **Add frontend UI** - Backup management interface
6. ⏭️ **Write tests** - Unit + E2E
7. ⏭️ **Deploy to dev** - Validate with real data

---

## References

- **Full Specification:** `PROJECT_BACKUP_RESTORE_SPEC.md`
- **Incremental Design:** See Section 6 of full spec
- **Database Schema:** `docs/database/schema-context.md`
- **Storage Service:** `apps/server-go/internal/storage/storage.go`
