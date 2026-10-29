-- +goose NO TRANSACTION
-- +goose Up
-- Indexes behind the documents list's derived columns and its new filters.
--
-- The documents listing projects chunk counts, the latest extraction job, and a
-- derived processing status per row: three correlated subqueries against
-- kb.object_extraction_jobs and one against kb.document_parsing_jobs. Neither
-- table had an index on document_id, so every row scanned its own job history
-- (and the new status filter / status counts evaluate the same subqueries for
-- every row in the project). The (document_id, created_at DESC) index serves
-- both the "latest job" lookup and the EXISTS(pending/processing) test.
--
-- The two documents indexes cover the keyset pagination the list now offers:
-- newest-first by created_at and by updated_at, both with id as the tiebreaker.
-- A single-column (project_id) index cannot satisfy either ordered scan.
--
-- Built CONCURRENTLY (NO TRANSACTION) so the job table is not locked
-- exclusively. A failed CONCURRENTLY build leaves an INVALID index behind, so
-- drop any leftover first.
DROP INDEX CONCURRENTLY IF EXISTS kb.idx_object_extraction_jobs_document_created;
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_object_extraction_jobs_document_created
  ON kb.object_extraction_jobs (document_id, created_at DESC);

DROP INDEX CONCURRENTLY IF EXISTS kb.idx_documents_project_created_id;
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_documents_project_created_id
  ON kb.documents (project_id, created_at DESC, id DESC);

DROP INDEX CONCURRENTLY IF EXISTS kb.idx_documents_project_updated_id;
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_documents_project_updated_id
  ON kb.documents (project_id, updated_at DESC, id DESC);

-- +goose Down
-- Kept CONCURRENTLY and outside any StatementBegin/End block: goose accumulates a
-- block into one statement and runs it in a single ExecContext, which Postgres
-- treats as a transaction — where DROP INDEX CONCURRENTLY is illegal. Left as
-- separate statements, each commits on its own (the file is NO TRANSACTION).
DROP INDEX CONCURRENTLY IF EXISTS kb.idx_documents_project_updated_id;
DROP INDEX CONCURRENTLY IF EXISTS kb.idx_documents_project_created_id;
DROP INDEX CONCURRENTLY IF EXISTS kb.idx_object_extraction_jobs_document_created;
