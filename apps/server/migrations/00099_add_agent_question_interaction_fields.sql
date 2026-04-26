-- +goose Up
-- Add interaction_type, placeholder, and max_length columns to kb.agent_questions
ALTER TABLE kb.agent_questions
  ADD COLUMN IF NOT EXISTS interaction_type text NOT NULL DEFAULT 'buttons',
  ADD COLUMN IF NOT EXISTS placeholder text,
  ADD COLUMN IF NOT EXISTS max_length integer;

-- +goose Down
ALTER TABLE kb.agent_questions
  DROP COLUMN IF EXISTS interaction_type,
  DROP COLUMN IF EXISTS placeholder,
  DROP COLUMN IF EXISTS max_length;
