-- +goose Up
-- +goose StatementBegin

-- Automatic skill capture for product agents.
--
-- A project opts in with kb.projects.skill_capture (JSONB). A typed per-agent
-- override lives on kb.agent_definitions.skill_capture and may only constrain
-- the project policy (off), never widen it. Both default to "off" so the
-- feature is inert for every existing project and agent.
ALTER TABLE kb.projects
    ADD COLUMN IF NOT EXISTS skill_capture jsonb NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE kb.agent_definitions
    ADD COLUMN IF NOT EXISTS skill_capture jsonb;

-- Learned skills are named "{agentSlug}.{topic}" so AutoLoadSkills can match
-- them by prefix. The original constraint (migration 00052) only admitted
-- hyphens, so the database rule has to widen with the Go validator
-- (skills.NamePattern) or every captured lesson fails the insert.
ALTER TABLE kb.skills DROP CONSTRAINT IF EXISTS skills_name_check;
ALTER TABLE kb.skills ADD CONSTRAINT skills_name_check
    CHECK (name ~ '^[a-z0-9]+([-.][a-z0-9]+)*$' AND char_length(name) BETWEEN 1 AND 64);

-- One review per completed agent run. The row is inserted by the executor's
-- AfterRun hook and claimed by the background review worker with
-- FOR UPDATE SKIP LOCKED, so several replicas cannot review the same run twice.
CREATE TABLE IF NOT EXISTS kb.skill_reviews (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      uuid NOT NULL REFERENCES kb.projects(id) ON DELETE CASCADE,
    org_id          uuid,
    agent_id        uuid,
    agent_run_id    uuid,
    agent_name      text NOT NULL DEFAULT '',
    status          text NOT NULL DEFAULT 'pending'
                    CHECK (status IN ('pending', 'running', 'done', 'failed', 'skipped')),
    model           text NOT NULL DEFAULT '',
    input           jsonb NOT NULL DEFAULT '{}'::jsonb,
    input_tokens    integer NOT NULL DEFAULT 0,
    output_tokens   integer NOT NULL DEFAULT 0,
    candidate_count integer NOT NULL DEFAULT 0,
    error           text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    started_at      timestamptz,
    finished_at     timestamptz
);

CREATE INDEX IF NOT EXISTS idx_skill_reviews_claim
    ON kb.skill_reviews (status, created_at)
    WHERE status = 'pending';

CREATE INDEX IF NOT EXISTS idx_skill_reviews_agent
    ON kb.skill_reviews (agent_id, created_at DESC);

CREATE UNIQUE INDEX IF NOT EXISTS idx_skill_reviews_run
    ON kb.skill_reviews (agent_run_id)
    WHERE agent_run_id IS NOT NULL;

-- At most one active review per agent. This closes the HasActiveReview-then-
-- insert race: two concurrent runs cannot both queue a review, and the plain
-- ON CONFLICT DO NOTHING insert drops the loser.
CREATE UNIQUE INDEX IF NOT EXISTS idx_skill_reviews_one_active
    ON kb.skill_reviews (agent_id)
    WHERE status IN ('pending', 'running') AND agent_id IS NOT NULL;

-- A candidate the review proposes. Staged rows are inert until a human (or the
-- auto-apply guardrail) applies them through the normal skills store, so
-- embeddings, content_hash and provenance stay consistent.
CREATE TABLE IF NOT EXISTS kb.skill_proposals (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id        uuid NOT NULL REFERENCES kb.projects(id) ON DELETE CASCADE,
    agent_id          uuid,
    agent_run_id      uuid,
    agent_name        text NOT NULL DEFAULT '',
    review_id         uuid REFERENCES kb.skill_reviews(id) ON DELETE SET NULL,
    kind              text NOT NULL
                      CHECK (kind IN ('create', 'patch', 'delete')),
    name              text NOT NULL DEFAULT '',
    description       text NOT NULL DEFAULT '',
    content           text NOT NULL DEFAULT '',
    base_skill_id     uuid REFERENCES kb.skills(id) ON DELETE SET NULL,
    base_content_hash text NOT NULL DEFAULT '',
    patch_old         text NOT NULL DEFAULT '',
    patch_new         text NOT NULL DEFAULT '',
    evidence          text NOT NULL DEFAULT '',
    confidence        double precision NOT NULL DEFAULT 0,
    source            text NOT NULL DEFAULT 'background',
    status            text NOT NULL DEFAULT 'pending'
                      CHECK (status IN ('pending', 'applying', 'applied', 'rejected', 'failed')),
    auto_applied      boolean NOT NULL DEFAULT false,
    reviewed_by       uuid,
    review_note       text NOT NULL DEFAULT '',
    created_at        timestamptz NOT NULL DEFAULT now(),
    applied_at        timestamptz,
    claimed_at        timestamptz
);

CREATE INDEX IF NOT EXISTS idx_skill_proposals_inbox
    ON kb.skill_proposals (project_id, status, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_skill_proposals_agent
    ON kb.skill_proposals (agent_id, created_at DESC);

-- Runtime use telemetry. One row per skill; the curator uses last_used_at to
-- age agent-created skills and humans see real use counts instead of the
-- agent-definition reference count the skills UI shows today.
CREATE TABLE IF NOT EXISTS kb.skill_usage (
    skill_id       uuid PRIMARY KEY REFERENCES kb.skills(id) ON DELETE CASCADE,
    project_id     uuid REFERENCES kb.projects(id) ON DELETE CASCADE,
    use_count      bigint NOT NULL DEFAULT 0,
    patch_count    bigint NOT NULL DEFAULT 0,
    last_used_at   timestamptz,
    last_patched_at timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_skill_usage_project
    ON kb.skill_usage (project_id, last_used_at);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Refuse to downgrade while dotted (learned) names exist: silently renaming
-- them could collapse two skills onto one name. A human must archive/rename
-- them first.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM kb.skills WHERE name !~ '^[a-z0-9]+(-[a-z0-9]+)*$') THEN
        RAISE EXCEPTION 'cannot downgrade: dotted skill names exist; archive or rename them first';
    END IF;
END $$;

ALTER TABLE kb.skills DROP CONSTRAINT IF EXISTS skills_name_check;
ALTER TABLE kb.skills ADD CONSTRAINT skills_name_check
    CHECK (name ~ '^[a-z0-9]+(-[a-z0-9]+)*$' AND char_length(name) BETWEEN 1 AND 64);

DROP TABLE IF EXISTS kb.skill_usage;
DROP TABLE IF EXISTS kb.skill_proposals;
DROP TABLE IF EXISTS kb.skill_reviews;
ALTER TABLE kb.agent_definitions DROP COLUMN IF EXISTS skill_capture;
ALTER TABLE kb.projects DROP COLUMN IF EXISTS skill_capture;
-- +goose StatementEnd
