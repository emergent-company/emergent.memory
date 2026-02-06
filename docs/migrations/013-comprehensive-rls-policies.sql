-- Migration: 013-comprehensive-rls-policies.sql
-- Date: 2026-01-10
-- Description: Add RLS policies to high-priority tables without multi-tenant isolation
-- 
-- This migration adds RLS policies to 6 high-priority tables:
--   1. kb.chat_messages (via chat_conversations JOIN)
--   2. kb.document_artifacts (via documents JOIN)
--   3. kb.object_chunks (via graph_objects JOIN)
--   4. kb.object_extraction_logs (via object_extraction_jobs JOIN)
--   5. kb.document_parsing_jobs (direct project_id)
--   6. kb.graph_embedding_jobs (via graph_objects JOIN)
--
-- Pattern used: JOIN-based policies that inherit project isolation from parent tables
-- Session variable: app.current_project_id (set by DatabaseService.runWithTenantContext)

BEGIN;

-- ============================================================================
-- 1. kb.chat_messages - Inherit isolation from chat_conversations
-- ============================================================================

ALTER TABLE kb.chat_messages ENABLE ROW LEVEL SECURITY;

-- SELECT: Allow if conversation belongs to current project (or no project context)
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

-- INSERT: Verify conversation belongs to current project
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

-- UPDATE: Same as SELECT
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

-- DELETE: Same as SELECT
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

-- ============================================================================
-- 2. kb.document_artifacts - Inherit isolation from documents
-- ============================================================================

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

-- ============================================================================
-- 3. kb.object_chunks - Inherit isolation from graph_objects
-- ============================================================================

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

-- ============================================================================
-- 4. kb.object_extraction_logs - Inherit isolation from object_extraction_jobs
-- ============================================================================

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

-- ============================================================================
-- 5. kb.document_parsing_jobs - Direct project_id isolation
-- ============================================================================

ALTER TABLE kb.document_parsing_jobs ENABLE ROW LEVEL SECURITY;

CREATE POLICY document_parsing_jobs_select_policy ON kb.document_parsing_jobs
  FOR SELECT USING (
    COALESCE(current_setting('app.current_project_id', true), '') = ''
    OR project_id::text = current_setting('app.current_project_id', true)
  );

-- INSERT: Allow all (project_id will be set by application)
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

-- ============================================================================
-- 6. kb.graph_embedding_jobs - Inherit isolation from graph_objects
-- ============================================================================

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

COMMIT;

-- ============================================================================
-- Verification queries (run after migration)
-- ============================================================================

-- Check RLS is enabled on all 6 tables
-- SELECT relname, relrowsecurity 
-- FROM pg_class 
-- WHERE relname IN ('chat_messages', 'document_artifacts', 'object_chunks', 
--                   'object_extraction_logs', 'document_parsing_jobs', 'graph_embedding_jobs')
-- AND relnamespace = 'kb'::regnamespace;

-- Check policy counts
-- SELECT tablename, COUNT(*) as policy_count
-- FROM pg_policies
-- WHERE schemaname = 'kb'
-- AND tablename IN ('chat_messages', 'document_artifacts', 'object_chunks', 
--                   'object_extraction_logs', 'document_parsing_jobs', 'graph_embedding_jobs')
-- GROUP BY tablename;
