-- +goose Up
-- +goose StatementBegin

-- Chunk curation: keep an append-only revision history per chunk and let a
-- chunk carry structural links (parent-child) plus an edited flag. The
-- original chunk text is backfilled as revision 1 so the curation API can
-- always return at least the original content.
ALTER TABLE kb.chunks
    ADD COLUMN IF NOT EXISTS parent_chunk_id uuid NULL,
    ADD COLUMN IF NOT EXISTS edited boolean NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS current_revision integer NOT NULL DEFAULT 1;

CREATE INDEX IF NOT EXISTS idx_chunks_parent_chunk_id
    ON kb.chunks (parent_chunk_id)
    WHERE parent_chunk_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS kb.chunk_revisions (
    id         uuid        PRIMARY KEY DEFAULT uuid_generate_v4(),
    chunk_id   uuid        NOT NULL REFERENCES kb.chunks(id) ON DELETE CASCADE,
    seq        integer     NOT NULL,
    text       text        NOT NULL,
    author     text,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (chunk_id, seq)
);

COMMENT ON TABLE kb.chunk_revisions IS 'Append-only revision history for curated chunks; seq is monotonic per chunk and defines ordering.';

-- Backfill the original content as revision 1 for every existing chunk.
INSERT INTO kb.chunk_revisions (chunk_id, seq, text, created_at)
SELECT id, 1, text, created_at FROM kb.chunks
ON CONFLICT (chunk_id, seq) DO NOTHING;

-- Every newly inserted chunk gets its original content recorded as revision 1,
-- regardless of which creation path inserted it.
CREATE OR REPLACE FUNCTION kb.seed_chunk_revision() RETURNS trigger AS $$
BEGIN
    INSERT INTO kb.chunk_revisions (chunk_id, seq, text, created_at)
    VALUES (NEW.id, 1, NEW.text, COALESCE(NEW.created_at, now()))
    ON CONFLICT (chunk_id, seq) DO NOTHING;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_seed_chunk_revision ON kb.chunks;
CREATE TRIGGER trg_seed_chunk_revision
    AFTER INSERT ON kb.chunks
    FOR EACH ROW EXECUTE FUNCTION kb.seed_chunk_revision();

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TRIGGER IF EXISTS trg_seed_chunk_revision ON kb.chunks;
DROP FUNCTION IF EXISTS kb.seed_chunk_revision();

DROP TABLE IF EXISTS kb.chunk_revisions;

DROP INDEX IF EXISTS idx_chunks_parent_chunk_id;

ALTER TABLE kb.chunks
    DROP COLUMN IF EXISTS current_revision,
    DROP COLUMN IF EXISTS edited,
    DROP COLUMN IF EXISTS parent_chunk_id;

-- +goose StatementEnd
