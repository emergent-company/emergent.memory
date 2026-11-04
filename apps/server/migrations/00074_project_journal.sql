-- +goose Up
CREATE TABLE kb.project_journal (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id  UUID NOT NULL,
    event_type  TEXT NOT NULL,
    entity_type TEXT,
    entity_id   UUID,
    object_type TEXT,
    actor_type  TEXT NOT NULL DEFAULT 'system',
    actor_id    UUID,
    metadata    JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_project_journal_project_created ON kb.project_journal (project_id, created_at DESC);

CREATE TABLE kb.project_journal_notes (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id  UUID NOT NULL,
    journal_id  UUID REFERENCES kb.project_journal(id) ON DELETE CASCADE,
    body        TEXT NOT NULL,
    actor_type  TEXT NOT NULL DEFAULT 'user',
    actor_id    UUID,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_project_journal_notes_project ON kb.project_journal_notes (project_id, created_at DESC);
CREATE INDEX idx_project_journal_notes_journal  ON kb.project_journal_notes (journal_id);

-- +goose Down
DROP TABLE IF EXISTS kb.project_journal_notes;
DROP TABLE IF EXISTS kb.project_journal;
