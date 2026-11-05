-- +goose Up
-- Multi-source contributions for graph rows (slice 3 of #2376, PR B of
-- graph-write-attribution).
--
-- A source pushes records it does not own into Memory: the object is shared,
-- but each contributing source keeps its own external id and revision, and can
-- withdraw its contribution without deleting the object. That needs a row per
-- (version row, source) — the `_sources` property array cannot represent it: it
-- is keyed by document rather than by source, it caps at 25 and silently drops
-- the oldest, and for source ownership a dropped entry loses an owner.
--
-- Keyed to the VERSION ROW id, not (canonical_id, version): a branch fork
-- re-uses canonical_id at version = 1, so (canonical_id, version) is not unique
-- across branches, and GetHistory is branch-blind.
--
-- No FK to kb.graph_objects / kb.graph_relationships: provenance has to outlive
-- the row it describes. Deleting an object should not silently erase the record
-- of who contributed to it; source_id does FK to kb.sources because a
-- contribution without a source is meaningless, and is CASCADE because deleting
-- a source is an explicit administrative act.
--
-- external_revision_seq is a bigint because external_revision is an opaque
-- string from the source and cannot answer "is this push older than what I
-- have". Without a total order, staleness cannot be specified at all — this is
-- the column the reconciliation rule needs.
--
-- merged_from_version_id is the version row a contribution was carried from
-- when a merge unioned it onto the target version; NULL when the contribution
-- arrived on the row it is attached to. That is what makes "which version each
-- contribution arrived on" expressible without a second table.

CREATE TABLE IF NOT EXISTS kb.provenance_contributions (
    id                     uuid PRIMARY KEY DEFAULT public.uuid_generate_v4(),
    project_id             uuid NOT NULL,
    branch_id              uuid,
    subject_kind           text NOT NULL,
    subject_version_id     uuid NOT NULL,
    source_id              uuid NOT NULL REFERENCES kb.sources (id) ON DELETE CASCADE,
    external_id            text NOT NULL,
    external_revision      text NOT NULL DEFAULT '',
    external_revision_seq  bigint NOT NULL DEFAULT 0,
    merged_from_version_id uuid,
    state                  text NOT NULL DEFAULT 'present',
    first_seen_at          timestamptz NOT NULL DEFAULT now(),
    last_seen_at           timestamptz NOT NULL DEFAULT now(),
    withdrawn_at           timestamptz,
    CONSTRAINT provenance_contributions_subject_kind_check
        CHECK (subject_kind = ANY (ARRAY['object'::text, 'relationship'::text])),
    CONSTRAINT provenance_contributions_state_check
        CHECK (state = ANY (ARRAY['present'::text, 'superseded'::text, 'withdrawn'::text])),
    CONSTRAINT provenance_contributions_withdrawn_pairs
        CHECK (state <> 'withdrawn' OR withdrawn_at IS NOT NULL),
    CONSTRAINT provenance_contributions_revision_seq_nonnegative
        CHECK (external_revision_seq >= 0)
);

-- The addressing key: an external record maps to at most one subject per
-- branch. branch_id is denormalized from the version row so the lookup does not
-- need a join, and it is part of the key because a fork copies contributions
-- onto its own version rows — without the branch, the fork's copy would shadow
-- the main-line record and the apply could not tell which object to version.
--
-- Two partial indexes rather than one with COALESCE, matching the shape
-- uq_graph_relationships_head_main / _branch already uses for a nullable branch
-- id. Enforcing this in the database (not a read-then-write check) is what
-- serializes two concurrent applies for the same record: one of them loses on
-- the index instead of both creating an object.
CREATE UNIQUE INDEX IF NOT EXISTS uq_provenance_contributions_present_main
    ON kb.provenance_contributions (project_id, subject_kind, source_id, external_id)
    WHERE state = 'present' AND branch_id IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uq_provenance_contributions_present_branch
    ON kb.provenance_contributions (project_id, branch_id, subject_kind, source_id, external_id)
    WHERE state = 'present' AND branch_id IS NOT NULL;

-- One present contribution per (version row, source): a source contributes its
-- record once, and a re-contribution supersedes the old row rather than
-- duplicating it.
CREATE UNIQUE INDEX IF NOT EXISTS uq_provenance_contributions_version_source
    ON kb.provenance_contributions (subject_kind, subject_version_id, source_id)
    WHERE state = 'present';

-- The HEAD read: all contributions of one or more version rows in one query.
CREATE INDEX IF NOT EXISTS idx_provenance_contributions_version
    ON kb.provenance_contributions (subject_version_id);

-- One row per applied changeset, written in the same transaction as the data it
-- describes. Because MVCC commits both together, readers never observe a
-- partially applied generation; the row's job is to say which push a
-- contribution arrived in, and to give the source an idempotency anchor.
CREATE TABLE IF NOT EXISTS kb.source_generations (
    id          uuid PRIMARY KEY DEFAULT public.uuid_generate_v4(),
    project_id  uuid NOT NULL,
    source_id   uuid NOT NULL REFERENCES kb.sources (id) ON DELETE CASCADE,
    generation  bigint NOT NULL,
    item_count  integer NOT NULL DEFAULT 0,
    applied_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT source_generations_generation_positive CHECK (generation > 0),
    CONSTRAINT source_generations_item_count_nonnegative CHECK (item_count >= 0)
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_source_generations_source_generation
    ON kb.source_generations (source_id, generation);

-- +goose Down
DROP TABLE IF EXISTS kb.source_generations;
DROP TABLE IF EXISTS kb.provenance_contributions;
