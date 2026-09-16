-- +goose Up
-- Add a structured proposal payload to agent questions so the conversation UI
-- can render agent-proposed blueprint/schema changes as a reviewable card
-- instead of a raw JSON code fence. Nullable: plain-text questions carry no
-- proposal and behave exactly as before.
ALTER TABLE kb.agent_questions
  ADD COLUMN IF NOT EXISTS proposal JSONB;

COMMENT ON COLUMN kb.agent_questions.proposal IS 'Optional structured proposal envelope {kind, summary, body} attached to an ask_user checkpoint';

-- +goose Down
ALTER TABLE kb.agent_questions
  DROP COLUMN IF EXISTS proposal;
