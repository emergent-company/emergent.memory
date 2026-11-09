-- +goose Up
-- +goose StatementBegin

-- Repair stored agent model references that the executor cannot parse.
--
-- Legacy rows can hold a bare model name with too few fields to construct an
-- ADK model, e.g.:
--   {"name":"gemini-3.1-flash-lite-preview"}          -- no provider prefix
--   {"name":"deepseek-v4-flash","provider":"openai"}  -- structured pre-#1465
-- A bare name reaches pkg/adk.CreateModelWithName and fails every run with
-- "model name ... must include a provider prefix". The query worker surfaces
-- that as "Agent execution failed" and the search-knowledge tool reports it.
--
-- 00234 folded the structured shape but left bare names with no provider
-- untouched. This migration finishes the repair:
--   1. fold a legacy structured provider into the canonical name,
--   2. drop a redundant structured provider when the name is already prefixed,
--   3. clear a bare name that has no provider at all, so the agent falls back
--      to the project's default generative model.

-- 1. Fold: bare name + structured provider -> provider/model.
UPDATE kb.agent_definitions
SET model = (model - 'name' - 'provider') || jsonb_build_object(
        'name', (model->>'provider') || '/' || (model->>'name')
    ),
    updated_at = now()
WHERE model ? 'provider'
  AND coalesce(model->>'provider', '') <> ''
  AND coalesce(model->>'name', '') <> ''
  AND position('/' in (model->>'name')) = 0;

-- 2. Purity: name already carries a prefix -> drop the redundant provider key.
UPDATE kb.agent_definitions
SET model = model - 'provider',
    updated_at = now()
WHERE model ? 'provider'
  AND coalesce(model->>'provider', '') <> ''
  AND position('/' in coalesce(model->>'name', '')) > 0;

-- 3. Bare name with no provider cannot be routed -> clear it so the run uses
--    the project default. Preserve every other model key (temperature,
--    maxTokens, enableThinking, nativeTools, ...).
UPDATE kb.agent_definitions
SET model = model - 'name' - 'provider',
    updated_at = now()
WHERE coalesce(model->>'name', '') <> ''
  AND position('/' in (model->>'name')) = 0;

-- 4. The same legacy references can be persisted in a project agent override
--    (kb.project_settings, category='agent_override'). Apply the identical
--    fold/purity/clear rules to the nested model object.
UPDATE kb.project_settings
SET value = jsonb_set(
        value,
        '{model,name}',
        to_jsonb((value->'model'->>'provider') || '/' || (value->'model'->>'name'))
    ) #- '{model,provider}',
    updated_at = now()
WHERE category = 'agent_override'
  AND value ? 'model'
  AND coalesce(value->'model'->>'provider', '') <> ''
  AND coalesce(value->'model'->>'name', '') <> ''
  AND position('/' in (value->'model'->>'name')) = 0;

UPDATE kb.project_settings
SET value = value #- '{model,provider}',
    updated_at = now()
WHERE category = 'agent_override'
  AND value ? 'model'
  AND coalesce(value->'model'->>'provider', '') <> ''
  AND position('/' in coalesce(value->'model'->>'name', '')) > 0;

UPDATE kb.project_settings
SET value = value #- '{model,name}' #- '{model,provider}',
    updated_at = now()
WHERE category = 'agent_override'
  AND value ? 'model'
  AND coalesce(value->'model'->>'name', '') <> ''
  AND position('/' in (value->'model'->>'name')) = 0;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- No-op: the repaired values are the canonical form the model-reference
-- invariant requires; restoring unparsable references would re-break every run.
-- +goose StatementEnd
