-- +goose Up
-- +goose StatementBegin

-- Normalize legacy structured model references in kb.agent_definitions.model
-- (issue #1465).
--
-- kb.agent_definitions.model has historically stored two shapes:
--   - canonical:         {"name":"provider/model"} — all current write paths
--     normalize via NormalizeAgentModelConfig (domain/agents/model_ref.go)
--   - legacy structured: {"name":"<bare>","provider":"<slug>"}
--
-- No migration ever backfilled kb.agent_definitions.model (only
-- kb.project_model_config and provider configs were backfilled), so rows written
-- before the canonical invariant shipped can still hold the structured shape.
-- The list/get read paths render the same definition with two different MODEL
-- cells: the override branch echoes the stored name verbatim (bare "<bare>")
-- while the no-override branch reports the project default as "provider/model".
-- This folds the legacy shape into the canonical one and drops the now-redundant
-- provider key. Both statements are idempotent and skip model = '{}' rows.

-- (a) Fold: bare name + structured provider → prefixed name, drop provider key.
--     Preserves every other key (temperature, maxTokens, enableThinking,
--     nativeTools, ...) via `model - 'provider' || jsonb_build_object(...)`.
UPDATE kb.agent_definitions
SET model = (model - 'provider') || jsonb_build_object(
    'name', (model->>'provider') || '/' || (model->>'name')
)
WHERE model ? 'provider'
  AND (model->>'provider') <> ''
  AND (model->>'name') <> ''
  AND position('/' in (model->>'name')) = 0;

-- (b) Purity: a name that already contains '/' with a redundant structured
--     provider → just drop the provider key.
UPDATE kb.agent_definitions
SET model = model - 'provider'
WHERE model ? 'provider'
  AND (model->>'provider') <> ''
  AND position('/' in (model->>'name')) > 0;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- No-op: the folded/prefixed values are the canonical form the invariant
-- requires; restoring the legacy structured shape would re-introduce the
-- divergence this migration repairs.
-- +goose StatementEnd
