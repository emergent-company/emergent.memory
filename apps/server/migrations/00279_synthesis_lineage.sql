-- +goose Up
-- Input lineage for synthesized graph versions (slice 4 of #2376).
--
-- `data_class = synthesized` says Memory produced a row; `input_hash` proves
-- something was unchanged. Neither records what the row was derived from, so
-- extraction output cannot be traced to its document/job and re-extraction
-- cannot find its own earlier output without key/name heuristics.
--
-- Keyed to the VERSION ROW id, like kb.provenance_contributions and for the
-- same reason: a branch fork re-uses canonical_id at version = 1, so
-- (canonical_id, version) is not unique across branches.
--
-- No FK to kb.graph_objects / kb.graph_relationships: provenance outlives the
-- row it describes. `input_id` is polymorphic (document, extraction job, or a
-- version row of either subject kind), so it cannot carry an FK either; the
-- CHECK below is what keeps the vocabulary closed. A lineage row naming a
-- deleted document remains meaningful because it still names the document id.
--
-- Rows are immutable: a new version inherits its predecessor's inputs and
-- records the predecessor itself as `version`, so the chain is readable without
-- mutating history. The unique index makes a re-write of the same input on the
-- same version a no-op rather than a duplicate.

CREATE TABLE IF NOT EXISTS kb.synthesis_lineage (
    id                 uuid PRIMARY KEY DEFAULT public.uuid_generate_v4(),
    project_id         uuid NOT NULL,
    subject_kind       text NOT NULL,
    subject_version_id uuid NOT NULL,
    input_kind         text NOT NULL,
    input_id           uuid NOT NULL,
    derived_at         timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT synthesis_lineage_subject_kind_check
        CHECK (subject_kind = ANY (ARRAY['object'::text, 'relationship'::text])),
    CONSTRAINT synthesis_lineage_input_kind_check
        CHECK (input_kind = ANY (ARRAY['document'::text, 'extraction_job'::text, 'version'::text]))
);

-- One recorded input per (version row, kind, id). A re-extraction that names the
-- same document again, or a version that inherits an input it already had, must
-- not duplicate the row.
CREATE UNIQUE INDEX IF NOT EXISTS uq_synthesis_lineage_input
    ON kb.synthesis_lineage (subject_kind, subject_version_id, input_kind, input_id);

-- Forward read: the inputs of one or more version rows (object detail/history)
-- in one batched query.
CREATE INDEX IF NOT EXISTS idx_synthesis_lineage_version
    ON kb.synthesis_lineage (subject_version_id);

-- Reverse read: everything derived from a document/job in a project, which is
-- what supersede-by-lineage asks ("which rows did this re-extraction replace?").
CREATE INDEX IF NOT EXISTS idx_synthesis_lineage_project_input
    ON kb.synthesis_lineage (project_id, input_kind, input_id);

-- +goose Down
DROP TABLE IF EXISTS kb.synthesis_lineage;
