# RLS Comprehensive Security Audit

**Date:** 2026-01-10  
**Status:** In Progress  
**Author:** AI Agent

---

## Executive Summary

This audit examines Row-Level Security (RLS) policies across all 65 database tables in the `kb`, `core`, and `public` schemas. The goal is to ensure proper multi-tenant isolation at the database level.

### Key Findings

| Category                       | Count | Status                 |
| ------------------------------ | ----- | ---------------------- |
| Tables WITH RLS                | 26    | ✅ Protected           |
| Tables WITHOUT RLS (Need RLS)  | 20    | ⚠️ **Action Required** |
| Tables WITHOUT RLS (Global/OK) | 19    | ✅ No action needed    |

---

## Current RLS Status

### ✅ Tables WITH RLS Enabled (26 tables)

These tables have proper RLS policies in place:

| Schema | Table                         | Policy Count | Isolation Level             |
| ------ | ----------------------------- | ------------ | --------------------------- |
| kb     | branches                      | 4            | Project                     |
| kb     | chat_conversations            | 4            | Project                     |
| kb     | chunk_embedding_jobs          | 1            | Project (via JOIN)          |
| kb     | chunks                        | 4            | Project (via document JOIN) |
| kb     | data_source_integrations      | 2            | Project + User/Role         |
| kb     | data_source_sync_jobs         | 2            | Project + User/Role         |
| kb     | documents                     | 4            | Project                     |
| kb     | embedding_policies            | 4            | Project                     |
| kb     | external_sources              | 4            | Project                     |
| kb     | graph_objects                 | 4            | Project                     |
| kb     | graph_relationships           | 4            | Project                     |
| kb     | integrations                  | 4            | Project                     |
| kb     | invites                       | 4            | Org + Project               |
| kb     | notifications                 | 4            | Project                     |
| kb     | object_extraction_jobs        | 4            | Project                     |
| kb     | object_type_schemas           | 4            | Project                     |
| kb     | product_versions              | 4            | Project                     |
| kb     | project_memberships           | 4            | Project                     |
| kb     | project_object_type_registry  | 4            | Project                     |
| kb     | project_template_packs        | 4            | Project                     |
| kb     | tags                          | 4            | Project                     |
| kb     | tasks                         | 4            | Project                     |
| kb     | template_pack_studio_sessions | 4            | Project                     |
| kb     | user_recent_items             | 1            | User                        |

---

## ⚠️ Tables Requiring RLS Implementation

### Priority 1: HIGH - Sensitive Data Tables

These tables contain sensitive data that could leak across tenants:

| Table                    | Schema | Risk                        | Has FK to Protected Table | Recommended Policy   |
| ------------------------ | ------ | --------------------------- | ------------------------- | -------------------- |
| `chat_messages`          | kb     | Messages from conversations | `chat_conversations`      | JOIN to conversation |
| `document_artifacts`     | kb     | Document content/images     | `documents`               | JOIN to document     |
| `object_chunks`          | kb     | Object-chunk mappings       | `graph_objects`, `chunks` | JOIN to object       |
| `object_extraction_logs` | kb     | Extraction details          | `object_extraction_jobs`  | JOIN to job          |
| `document_parsing_jobs`  | kb     | Parsing job data            | `documents`               | Direct project_id    |
| `graph_embedding_jobs`   | kb     | Embedding job data          | `graph_objects`           | JOIN to object       |

### Priority 2: MEDIUM - Organization/Access Control Tables

| Table                      | Schema | Risk            | Recommended Policy       |
| -------------------------- | ------ | --------------- | ------------------------ |
| `projects`                 | kb     | Project list    | Org membership check     |
| `orgs`                     | kb     | Org list        | Org membership check     |
| `organization_memberships` | kb     | Membership data | User-based isolation     |
| `api_tokens`               | core   | API credentials | User + Project isolation |

### Priority 3: LOW - Supporting Tables

| Table                           | Schema | Risk                 | Recommended Policy      |
| ------------------------------- | ------ | -------------------- | ----------------------- |
| `agent_processing_log`          | kb     | Agent execution logs | JOIN to graph_object    |
| `product_version_members`       | kb     | Version membership   | JOIN to product_version |
| `branch_lineage`                | kb     | Branch ancestry      | JOIN to branch          |
| `clickup_import_logs`           | kb     | Import logs          | JOIN to integration     |
| `clickup_sync_state`            | kb     | Sync state           | JOIN to integration     |
| `template_pack_studio_messages` | kb     | Studio messages      | JOIN to session         |

---

## ✅ Tables NOT Requiring RLS (Global/System)

These tables are intentionally global or system-wide:

| Table                      | Schema | Reason                                        |
| -------------------------- | ------ | --------------------------------------------- |
| `user_profiles`            | core   | Global user accounts (cross-tenant by design) |
| `user_emails`              | core   | User email addresses (user-scoped)            |
| `user_email_preferences`   | core   | User preferences (user-scoped)                |
| `superadmins`              | core   | System-wide admins                            |
| `agents`                   | kb     | Global agent configurations                   |
| `agent_runs`               | kb     | Agent execution (global)                      |
| `settings`                 | kb     | Global app settings                           |
| `email_templates`          | kb     | Global email templates                        |
| `email_template_versions`  | kb     | Template versions                             |
| `email_jobs`               | kb     | Email queue (global)                          |
| `email_logs`               | kb     | Email events                                  |
| `audit_log`                | kb     | Security audit (global)                       |
| `auth_introspection_cache` | kb     | Token cache                                   |
| `llm_call_logs`            | kb     | LLM logging                                   |
| `system_process_logs`      | kb     | System logging                                |
| `merge_provenance`         | kb     | Merge history                                 |
| `release_notification_*`   | kb     | Release notifications                         |

### Public Schema (System Tables)

| Table                   | Reason                |
| ----------------------- | --------------------- |
| `checkpoints`           | LangGraph persistence |
| `checkpoint_blobs`      | LangGraph persistence |
| `checkpoint_writes`     | LangGraph persistence |
| `checkpoint_migrations` | LangGraph persistence |
| `typeorm_migrations`    | Schema migrations     |

---

## Implementation Plan

### Phase 1: High-Priority Tables (Immediate)

**Target:** 6 tables with direct data exposure risk

#### 1.1 `kb.chat_messages` - JOIN-based policy via conversation

```sql
-- Enable RLS
ALTER TABLE kb.chat_messages ENABLE ROW LEVEL SECURITY;

-- SELECT policy
CREATE POLICY chat_messages_select_policy ON kb.chat_messages
  FOR SELECT USING (
    EXISTS (
      SELECT 1 FROM kb.chat_conversations c
      WHERE c.id = chat_messages.conversation_id
      AND (
        COALESCE(current_setting('app.current_project_id', true), '') = ''
        OR c.project_id::text = current_setting('app.current_project_id', true)
      )
    )
  );

-- INSERT policy
CREATE POLICY chat_messages_insert_policy ON kb.chat_messages
  FOR INSERT WITH CHECK (
    EXISTS (
      SELECT 1 FROM kb.chat_conversations c
      WHERE c.id = chat_messages.conversation_id
      AND (
        COALESCE(current_setting('app.current_project_id', true), '') = ''
        OR c.project_id::text = current_setting('app.current_project_id', true)
      )
    )
  );

-- UPDATE policy
CREATE POLICY chat_messages_update_policy ON kb.chat_messages
  FOR UPDATE USING (
    EXISTS (
      SELECT 1 FROM kb.chat_conversations c
      WHERE c.id = chat_messages.conversation_id
      AND (
        COALESCE(current_setting('app.current_project_id', true), '') = ''
        OR c.project_id::text = current_setting('app.current_project_id', true)
      )
    )
  );

-- DELETE policy
CREATE POLICY chat_messages_delete_policy ON kb.chat_messages
  FOR DELETE USING (
    EXISTS (
      SELECT 1 FROM kb.chat_conversations c
      WHERE c.id = chat_messages.conversation_id
      AND (
        COALESCE(current_setting('app.current_project_id', true), '') = ''
        OR c.project_id::text = current_setting('app.current_project_id', true)
      )
    )
  );
```

#### 1.2 `kb.document_artifacts` - JOIN-based policy via document

```sql
ALTER TABLE kb.document_artifacts ENABLE ROW LEVEL SECURITY;

CREATE POLICY document_artifacts_select_policy ON kb.document_artifacts
  FOR SELECT USING (
    EXISTS (
      SELECT 1 FROM kb.documents d
      WHERE d.id = document_artifacts.document_id
      AND (
        COALESCE(current_setting('app.current_project_id', true), '') = ''
        OR d.project_id::text = current_setting('app.current_project_id', true)
      )
    )
  );

CREATE POLICY document_artifacts_insert_policy ON kb.document_artifacts
  FOR INSERT WITH CHECK (
    EXISTS (
      SELECT 1 FROM kb.documents d
      WHERE d.id = document_artifacts.document_id
      AND (
        COALESCE(current_setting('app.current_project_id', true), '') = ''
        OR d.project_id::text = current_setting('app.current_project_id', true)
      )
    )
  );

CREATE POLICY document_artifacts_update_policy ON kb.document_artifacts
  FOR UPDATE USING (
    EXISTS (
      SELECT 1 FROM kb.documents d
      WHERE d.id = document_artifacts.document_id
      AND (
        COALESCE(current_setting('app.current_project_id', true), '') = ''
        OR d.project_id::text = current_setting('app.current_project_id', true)
      )
    )
  );

CREATE POLICY document_artifacts_delete_policy ON kb.document_artifacts
  FOR DELETE USING (
    EXISTS (
      SELECT 1 FROM kb.documents d
      WHERE d.id = document_artifacts.document_id
      AND (
        COALESCE(current_setting('app.current_project_id', true), '') = ''
        OR d.project_id::text = current_setting('app.current_project_id', true)
      )
    )
  );
```

#### 1.3 `kb.object_chunks` - JOIN-based policy via graph_object

```sql
ALTER TABLE kb.object_chunks ENABLE ROW LEVEL SECURITY;

CREATE POLICY object_chunks_select_policy ON kb.object_chunks
  FOR SELECT USING (
    EXISTS (
      SELECT 1 FROM kb.graph_objects o
      WHERE o.id = object_chunks.object_id
      AND (
        COALESCE(current_setting('app.current_project_id', true), '') = ''
        OR o.project_id::text = current_setting('app.current_project_id', true)
      )
    )
  );

CREATE POLICY object_chunks_insert_policy ON kb.object_chunks
  FOR INSERT WITH CHECK (
    EXISTS (
      SELECT 1 FROM kb.graph_objects o
      WHERE o.id = object_chunks.object_id
      AND (
        COALESCE(current_setting('app.current_project_id', true), '') = ''
        OR o.project_id::text = current_setting('app.current_project_id', true)
      )
    )
  );

CREATE POLICY object_chunks_update_policy ON kb.object_chunks
  FOR UPDATE USING (
    EXISTS (
      SELECT 1 FROM kb.graph_objects o
      WHERE o.id = object_chunks.object_id
      AND (
        COALESCE(current_setting('app.current_project_id', true), '') = ''
        OR o.project_id::text = current_setting('app.current_project_id', true)
      )
    )
  );

CREATE POLICY object_chunks_delete_policy ON kb.object_chunks
  FOR DELETE USING (
    EXISTS (
      SELECT 1 FROM kb.graph_objects o
      WHERE o.id = object_chunks.object_id
      AND (
        COALESCE(current_setting('app.current_project_id', true), '') = ''
        OR o.project_id::text = current_setting('app.current_project_id', true)
      )
    )
  );
```

#### 1.4 `kb.object_extraction_logs` - JOIN-based policy via extraction_job

```sql
ALTER TABLE kb.object_extraction_logs ENABLE ROW LEVEL SECURITY;

CREATE POLICY object_extraction_logs_select_policy ON kb.object_extraction_logs
  FOR SELECT USING (
    EXISTS (
      SELECT 1 FROM kb.object_extraction_jobs j
      WHERE j.id = object_extraction_logs.extraction_job_id
      AND (
        COALESCE(current_setting('app.current_project_id', true), '') = ''
        OR j.project_id::text = current_setting('app.current_project_id', true)
      )
    )
  );

CREATE POLICY object_extraction_logs_insert_policy ON kb.object_extraction_logs
  FOR INSERT WITH CHECK (
    EXISTS (
      SELECT 1 FROM kb.object_extraction_jobs j
      WHERE j.id = object_extraction_logs.extraction_job_id
      AND (
        COALESCE(current_setting('app.current_project_id', true), '') = ''
        OR j.project_id::text = current_setting('app.current_project_id', true)
      )
    )
  );

CREATE POLICY object_extraction_logs_update_policy ON kb.object_extraction_logs
  FOR UPDATE USING (
    EXISTS (
      SELECT 1 FROM kb.object_extraction_jobs j
      WHERE j.id = object_extraction_logs.extraction_job_id
      AND (
        COALESCE(current_setting('app.current_project_id', true), '') = ''
        OR j.project_id::text = current_setting('app.current_project_id', true)
      )
    )
  );

CREATE POLICY object_extraction_logs_delete_policy ON kb.object_extraction_logs
  FOR DELETE USING (
    EXISTS (
      SELECT 1 FROM kb.object_extraction_jobs j
      WHERE j.id = object_extraction_logs.extraction_job_id
      AND (
        COALESCE(current_setting('app.current_project_id', true), '') = ''
        OR j.project_id::text = current_setting('app.current_project_id', true)
      )
    )
  );
```

#### 1.5 `kb.document_parsing_jobs` - Direct project_id policy

```sql
ALTER TABLE kb.document_parsing_jobs ENABLE ROW LEVEL SECURITY;

CREATE POLICY document_parsing_jobs_select_policy ON kb.document_parsing_jobs
  FOR SELECT USING (
    COALESCE(current_setting('app.current_project_id', true), '') = ''
    OR project_id::text = current_setting('app.current_project_id', true)
  );

CREATE POLICY document_parsing_jobs_insert_policy ON kb.document_parsing_jobs
  FOR INSERT WITH CHECK (true);

CREATE POLICY document_parsing_jobs_update_policy ON kb.document_parsing_jobs
  FOR UPDATE USING (
    COALESCE(current_setting('app.current_project_id', true), '') = ''
    OR project_id::text = current_setting('app.current_project_id', true)
  );

CREATE POLICY document_parsing_jobs_delete_policy ON kb.document_parsing_jobs
  FOR DELETE USING (
    COALESCE(current_setting('app.current_project_id', true), '') = ''
    OR project_id::text = current_setting('app.current_project_id', true)
  );
```

#### 1.6 `kb.graph_embedding_jobs` - JOIN-based policy via graph_object

```sql
ALTER TABLE kb.graph_embedding_jobs ENABLE ROW LEVEL SECURITY;

CREATE POLICY graph_embedding_jobs_select_policy ON kb.graph_embedding_jobs
  FOR SELECT USING (
    EXISTS (
      SELECT 1 FROM kb.graph_objects o
      WHERE o.id = graph_embedding_jobs.object_id
      AND (
        COALESCE(current_setting('app.current_project_id', true), '') = ''
        OR o.project_id::text = current_setting('app.current_project_id', true)
      )
    )
  );

CREATE POLICY graph_embedding_jobs_insert_policy ON kb.graph_embedding_jobs
  FOR INSERT WITH CHECK (
    EXISTS (
      SELECT 1 FROM kb.graph_objects o
      WHERE o.id = graph_embedding_jobs.object_id
      AND (
        COALESCE(current_setting('app.current_project_id', true), '') = ''
        OR o.project_id::text = current_setting('app.current_project_id', true)
      )
    )
  );

CREATE POLICY graph_embedding_jobs_update_policy ON kb.graph_embedding_jobs
  FOR UPDATE USING (
    EXISTS (
      SELECT 1 FROM kb.graph_objects o
      WHERE o.id = graph_embedding_jobs.object_id
      AND (
        COALESCE(current_setting('app.current_project_id', true), '') = ''
        OR o.project_id::text = current_setting('app.current_project_id', true)
      )
    )
  );

CREATE POLICY graph_embedding_jobs_delete_policy ON kb.graph_embedding_jobs
  FOR DELETE USING (
    EXISTS (
      SELECT 1 FROM kb.graph_objects o
      WHERE o.id = graph_embedding_jobs.object_id
      AND (
        COALESCE(current_setting('app.current_project_id', true), '') = ''
        OR o.project_id::text = current_setting('app.current_project_id', true)
      )
    )
  );
```

### Phase 2: Organization/Access Control Tables

#### 2.1 `kb.projects` - Organization membership check

```sql
ALTER TABLE kb.projects ENABLE ROW LEVEL SECURITY;

CREATE POLICY projects_select_policy ON kb.projects
  FOR SELECT USING (
    COALESCE(current_setting('app.current_org_id', true), '') = ''
    OR organization_id::text = current_setting('app.current_org_id', true)
  );

CREATE POLICY projects_insert_policy ON kb.projects
  FOR INSERT WITH CHECK (true);

CREATE POLICY projects_update_policy ON kb.projects
  FOR UPDATE USING (
    COALESCE(current_setting('app.current_org_id', true), '') = ''
    OR organization_id::text = current_setting('app.current_org_id', true)
  );

CREATE POLICY projects_delete_policy ON kb.projects
  FOR DELETE USING (
    COALESCE(current_setting('app.current_org_id', true), '') = ''
    OR organization_id::text = current_setting('app.current_org_id', true)
  );
```

#### 2.2 `kb.organization_memberships` - User-based isolation

```sql
ALTER TABLE kb.organization_memberships ENABLE ROW LEVEL SECURITY;

CREATE POLICY organization_memberships_select_policy ON kb.organization_memberships
  FOR SELECT USING (
    COALESCE(current_setting('app.user_id', true), '') = ''
    OR user_id::text = current_setting('app.user_id', true)
    OR COALESCE(current_setting('app.current_org_id', true), '') = ''
    OR organization_id::text = current_setting('app.current_org_id', true)
  );

CREATE POLICY organization_memberships_insert_policy ON kb.organization_memberships
  FOR INSERT WITH CHECK (true);

CREATE POLICY organization_memberships_update_policy ON kb.organization_memberships
  FOR UPDATE USING (
    COALESCE(current_setting('app.current_org_id', true), '') = ''
    OR organization_id::text = current_setting('app.current_org_id', true)
  );

CREATE POLICY organization_memberships_delete_policy ON kb.organization_memberships
  FOR DELETE USING (
    COALESCE(current_setting('app.current_org_id', true), '') = ''
    OR organization_id::text = current_setting('app.current_org_id', true)
  );
```

#### 2.3 `core.api_tokens` - User + Project isolation

```sql
ALTER TABLE core.api_tokens ENABLE ROW LEVEL SECURITY;

CREATE POLICY api_tokens_select_policy ON core.api_tokens
  FOR SELECT USING (
    COALESCE(current_setting('app.user_id', true), '') = ''
    OR user_id::text = current_setting('app.user_id', true)
  );

CREATE POLICY api_tokens_insert_policy ON core.api_tokens
  FOR INSERT WITH CHECK (
    COALESCE(current_setting('app.user_id', true), '') = ''
    OR user_id::text = current_setting('app.user_id', true)
  );

CREATE POLICY api_tokens_update_policy ON core.api_tokens
  FOR UPDATE USING (
    COALESCE(current_setting('app.user_id', true), '') = ''
    OR user_id::text = current_setting('app.user_id', true)
  );

CREATE POLICY api_tokens_delete_policy ON core.api_tokens
  FOR DELETE USING (
    COALESCE(current_setting('app.user_id', true), '') = ''
    OR user_id::text = current_setting('app.user_id', true)
  );
```

### Phase 3: Supporting Tables

(Lower priority - implement after Phase 1 and 2)

- `kb.agent_processing_log`
- `kb.product_version_members`
- `kb.branch_lineage`
- `kb.clickup_import_logs`
- `kb.clickup_sync_state`
- `kb.template_pack_studio_messages`

---

## Application Code Changes Required

For tables where we add RLS, we must also ensure the application code:

1. **Uses `DatabaseService.query()`** instead of TypeORM QueryBuilder
2. **Calls `runWithTenantContext()`** to set the session variables
3. **Requires appropriate headers** (`x-project-id`, `x-org-id`) in controllers

### Files to Review/Update

| Table                    | Service File                  | Controller File                  |
| ------------------------ | ----------------------------- | -------------------------------- |
| `chat_messages`          | `chat.service.ts`             | `chat.controller.ts`             |
| `document_artifacts`     | `documents.service.ts`        | `documents.controller.ts`        |
| `object_chunks`          | `graph.service.ts`            | `graph.controller.ts`            |
| `object_extraction_logs` | `extraction-jobs.service.ts`  | Already protected                |
| `document_parsing_jobs`  | `document-parsing.service.ts` | `document-parsing.controller.ts` |
| `graph_embedding_jobs`   | `embedding.service.ts`        | Internal/cron                    |

---

## Testing Requirements

For each table with new RLS policies:

1. **Unit Test:** Verify policies exist and are enabled
2. **E2E Test:** Cross-project isolation test
3. **E2E Test:** Cross-org isolation test (where applicable)
4. **Manual Test:** Verify no performance degradation

---

## Migration File

Create `docs/migrations/013-comprehensive-rls-policies.sql` with all policies from Phase 1.

---

## Timeline

| Phase   | Tables           | Effort    | Target    |
| ------- | ---------------- | --------- | --------- |
| Phase 1 | 6 high-priority  | 2-3 hours | Immediate |
| Phase 2 | 4 access control | 1-2 hours | This week |
| Phase 3 | 6 supporting     | 1-2 hours | Next week |

---

## References

- Previous RLS implementations: `docs/bugs/049-document-rls-not-enforced.md`, `docs/bugs/050-chunks-rls-not-enforced.md`
- Database schema: `docs/database/schema-context.md`
- Entity definitions: `apps/server/src/entities/`
