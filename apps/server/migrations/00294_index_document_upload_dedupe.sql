-- +goose NO TRANSACTION
-- +goose Up
-- Index behind the MCP upload retry dedupe.
--
-- Repository.FindUploadByFileHash filters kb.documents on (project_id, file_hash)
-- to answer "did these bytes already produce a document?" for a retried upload.
-- 00142 dropped the unique index that used to cover the pair (00041) when
-- uploads stopped being deduplicated at insert time, and nothing replaced it, so
-- the lookup — the MISS path of every first-time MCP upload, which is the path
-- an agent pays on — scanned the project's whole document history.
--
-- Deliberately NOT unique. 00142 dropped the unique index because a second
-- upload of byte-identical content must be allowed to insert; uniqueness would
-- turn that insert into a unique violation. Two concurrent identical uploads may
-- therefore both create a row, which FindUploadByFileHash documents and the
-- caller tolerates (the caller that matters is a sequential retry). A non-unique
-- index keeps the insert path unchanged and only makes the lookup an index scan.
--
-- The predicate matches the select's shape: every caller of the lookup passes a
-- non-empty file_hash, and documents without one (sources that never hash their
-- bytes) cannot match a lookup, so they stay out of the index.
--
-- Built CONCURRENTLY (NO TRANSACTION) like 00249 so a large kb.documents is not
-- locked exclusively. A failed CONCURRENTLY build leaves an INVALID index
-- behind, so drop any leftover first.
--
-- Verified with EXPLAIN (enable_seqscan=off) on a scratch database holding a
-- project's documents: the lookup plans as an Index Scan on this index with
-- Index Cond (project_id = ... AND file_hash = ...), and no Seq Scan on
-- kb.documents. With a small table the planner prefers a sequential scan
-- regardless of the index, which is why the proof disables it.
DROP INDEX CONCURRENTLY IF EXISTS kb.idx_documents_project_file_hash;
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_documents_project_file_hash
  ON kb.documents (project_id, file_hash)
  WHERE file_hash IS NOT NULL;

-- +goose Down
-- Kept CONCURRENTLY and outside any StatementBegin/End block: goose runs a
-- StatementBegin block as a single ExecContext, which Postgres treats as a
-- transaction, where DROP INDEX CONCURRENTLY is illegal. As separate statements
-- under NO TRANSACTION each commits on its own.
DROP INDEX CONCURRENTLY IF EXISTS kb.idx_documents_project_file_hash;
