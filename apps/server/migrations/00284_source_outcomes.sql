-- +goose Up
-- Per-record outcomes for source-pushed records (slice 5 of #2376, adoption
-- feedback).
--
-- A source pushes records into a shared graph (changesets, migration 00261) but
-- cannot learn what Memory then did with them: whether the record became a
-- shared object another source merged into, whether a user adopted it, whether a
-- contribution was withdrawn, or whether its revision was skipped as stale. The
-- only alternative is polling every object it ever pushed and diffing.
--
-- The row is keyed by the source's own addressing standpoint
-- `(project_id, source_id, external_id)` — the same key the apply lookup uses —
-- and holds the CURRENT outcome, not a history: the states and who decides them
-- are in source_outcome.go. Storing the outcome where the decision is taken
-- (apply, withdraw, merge, adopt) is what makes "what changed since cursor X"
-- answerable; deriving it from contributions + versions + merge ledgers at read
-- time cannot be paged.
--
-- No backfill: a record pushed before this migration has no row and is reported
-- as unknown until it is touched again. Inventing an outcome for it would be a
-- guess about a decision nobody made.
--
-- seq is a project-scoped monotonic counter, not a timestamp: a source resuming
-- from a cursor must neither skip nor duplicate records, and timestamps tie. The
-- counter lives in kb.source_outcome_sequences so the allocation is one atomic
-- upsert per write and the sequence is dense per project.

CREATE TABLE IF NOT EXISTS kb.source_outcomes (
    id                    uuid PRIMARY KEY DEFAULT public.uuid_generate_v4(),
    project_id            uuid NOT NULL,
    source_id             uuid NOT NULL REFERENCES kb.sources (id) ON DELETE CASCADE,
    subject_kind          text NOT NULL DEFAULT 'object',
    external_id           text NOT NULL,
    state                 text NOT NULL,
    canonical_id          uuid,
    external_revision     text NOT NULL DEFAULT '',
    external_revision_seq bigint NOT NULL DEFAULT 0,
    generation            bigint NOT NULL DEFAULT 0,
    adopted_by_source_id  uuid REFERENCES kb.sources (id) ON DELETE SET NULL,
    reason                text NOT NULL DEFAULT '',
    seq                   bigint NOT NULL,
    first_seen_at         timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT source_outcomes_state_check CHECK (
        state = ANY (ARRAY['materialized'::text, 'merged'::text, 'adopted'::text,
                           'withdrawn'::text, 'skipped'::text])
    ),
    CONSTRAINT source_outcomes_subject_kind_check CHECK (
        subject_kind = ANY (ARRAY['object'::text, 'relationship'::text])
    ),
    CONSTRAINT source_outcomes_seq_positive CHECK (seq > 0),
    CONSTRAINT source_outcomes_revision_seq_nonnegative CHECK (external_revision_seq >= 0),
    CONSTRAINT source_outcomes_generation_nonnegative CHECK (generation >= 0)
);

-- One current outcome per record. The source addresses a record by external id,
-- so this is the key a write resolves with ON CONFLICT.
CREATE UNIQUE INDEX IF NOT EXISTS uq_source_outcomes_record
    ON kb.source_outcomes (project_id, source_id, external_id);

-- The pull surface: outcomes of one source changed after a cursor, in sequence
-- order. Covers the WHERE and the ORDER BY of the page read.
CREATE INDEX IF NOT EXISTS idx_source_outcomes_cursor
    ON kb.source_outcomes (project_id, source_id, seq);

-- The project's monotonic sequence. One row per project, allocated with
-- INSERT ... ON CONFLICT DO UPDATE ... RETURNING inside the writer's transaction,
-- so two writers cannot be handed the same value and the order a source pages in
-- is the order the outcomes were written in.
CREATE TABLE IF NOT EXISTS kb.source_outcome_sequences (
    project_id uuid PRIMARY KEY,
    last_seq   bigint NOT NULL DEFAULT 0,
    CONSTRAINT source_outcome_sequences_last_seq_nonnegative CHECK (last_seq >= 0)
);

-- +goose Down
DROP TABLE IF EXISTS kb.source_outcome_sequences;
DROP TABLE IF EXISTS kb.source_outcomes;
