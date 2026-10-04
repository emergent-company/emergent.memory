# fix-agent-model-ref-validation

## Why

The agent-definition write API (`POST`/`PATCH /api/projects/:projectId/agent-definitions`)
validates `model.name` with only `strings.Contains(name, "/")`. A malformed reference with an
empty provider segment (e.g. `/deepseek-v4-flash`) passes, is persisted verbatim, and is
reported back as the agent's effective model. A real dev agent ("operator") carries exactly
this value. Project model config already rejects it via the strict `modelref.Parse`, so the
two write paths disagree on what a valid model reference is.

## What Changes

- `CreateDefinition` and `UpdateDefinition` validate `model.name` with `modelref.Parse`,
  rejecting an empty provider segment, an empty model, or a value with no `/`. The stored
  value is normalized to the canonical `provider/model` string form.
- No request/response shape change; a previously-accepted malformed value now returns 400.

## Capabilities

### Added Capabilities

- `agent-definitions`: the agent-definition write contract, starting with model-reference
  validation.

## Impact

- `apps/server/domain/agents/handler.go` — strict validation via `modelref.Parse`.
- `apps/server/domain/agents/model_validation_test.go` — negative-path tests.
- No migration, no schema change.

## Out of scope (reported, not fixed)

- The MCP agent-update tool (`mcp_tools.go`) and project agent-override application
  (`project_settings_store.go`) also set `model.name` without this validation.
- Repairing already-persisted malformed rows (the known dev row).
