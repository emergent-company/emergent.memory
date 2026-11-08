-- +goose Up
-- +goose StatementBegin

-- content_hash is the content identity hash: lowercase sha256 hex of
-- kb.documents.content. It is maintained by the repository on every insert path
-- and when conversion fills in content. This is deliberately different from
-- source_text_hash, which hashes the parsed source text for change detection on
-- the source-sync seam.
--
-- Drop the old global partial unique index BEFORE backfilling. Since the index
-- hashed every non-NULL content_hash across a project, it would reject the very
-- rows that are now legitimate: two source items with identical parsed text, or
-- a superseded version sharing bytes with its HEAD. Backfilling first would make
-- that collision possible mid-migration.
DROP INDEX IF EXISTS kb."IDX_3bbf4ea30357bf556110f034d4";

-- Backfill rows whose hash is missing or stale. pgcrypto (digest) is installed
-- by the baseline migration.
UPDATE kb.documents
SET content_hash = encode(digest(content, 'sha256'), 'hex')
WHERE content IS NOT NULL
  AND (
      content_hash IS NULL
      OR content_hash <> encode(digest(content, 'sha256'), 'hex')
  );

-- Manual HEAD dedupe is preserved: at most one live manual document per project
-- may own a given content hash. Source-synced documents, superseded version
-- rows, and uploads are excluded, because they may legitimately share content
-- bytes (token ingestion is explicitly not deduplicated, and history keeps
-- every version row).
CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_project_content_hash_manual_head
    ON kb.documents (project_id, content_hash)
    WHERE content_hash IS NOT NULL
      AND supersedes_id IS NULL
      AND source_id IS NULL
      AND source_type IS DISTINCT FROM 'upload';

-- One-shot GC for version rows retired before this migration. Retired versions
-- stay in kb.documents for history, but they are intentionally no longer
-- chunk-searchable: keeping their chunks would leak stale text into retrieval
-- and waste storage. Deleting a chunk cascades to kb.object_chunks,
-- kb.chunk_revisions, and kb.chunk_embedding_jobs. A later re-sync creates fresh
-- chunks for the new HEAD.
DELETE FROM kb.chunks
WHERE document_id IN (
    SELECT id FROM kb.documents WHERE supersedes_id IS NOT NULL
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS kb.idx_documents_project_content_hash_manual_head;

-- Restore the pre-migration index shape. This can fail if the database now holds
-- content hashes that the old global uniqueness forbade (for example two source
-- items with identical content), which is expected after the Up migration.
CREATE UNIQUE INDEX IF NOT EXISTS "IDX_3bbf4ea30357bf556110f034d4"
    ON kb.documents USING btree (project_id, content_hash)
    WHERE (content_hash IS NOT NULL);

-- The content_hash backfill and the retired-version chunk GC are not
-- reversible: the Down migration cannot restore recomputed hashes to their
-- previous NULL/stale values, and it cannot restore deleted chunks.

-- +goose StatementEnd
