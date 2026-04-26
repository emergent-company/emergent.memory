-- Add interaction_type, placeholder, and max_length columns to kb.agent_questions
ALTER TABLE kb.agent_questions
  ADD COLUMN IF NOT EXISTS interaction_type text NOT NULL DEFAULT 'buttons',
  ADD COLUMN IF NOT EXISTS placeholder text,
  ADD COLUMN IF NOT EXISTS max_length integer;
