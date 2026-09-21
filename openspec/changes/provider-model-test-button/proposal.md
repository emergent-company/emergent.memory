## Why

Project Settings lets a user pick the project's default generative and embedding models, but gives no way to verify a selection works before agents and extraction depend on it. A wrong or unreachable default fails silently at run time. Memory already exposes a live provider test (generate + embed), but it only exercises the model configured on the provider *credential* — not the explicitly selected default model, which lives in a separate project model config.

## What Changes

- Extend Memory's project provider test endpoint with an optional `{model, modelType}` body so any explicit model can be tested (generative or embedding). Omitting the body keeps today's behaviour exactly.
- Add a **Test** action beside each dropdown in the Settings → **Default models** panel that runs a live generate/embed call for the currently selected model and reports the outcome as a toast — without changing the stored defaults or disturbing the existing save-on-change flow.

## Capabilities

### Modified Capabilities
- `provider-settings`: adds a per-model test action to the Default models panel, backed by an explicit-model provider test API.

## Impact

- **Server** (`apps/server/domain/provider`): the catalog test helpers gain an explicit-model entry point; `TestProjectProvider` accepts an optional body. Backward compatible for existing callers (`POST /projects/:projectId/providers/:provider/test` with no body).
- **Gateway** (`apps/web-ui/gateway`): new `MemoryClient.TestProjectModel`, a new settings route/handler, and Test buttons in `defaultModelsPanel`.
- No database, migration, or schema changes.

## Non-Goals

- No "test every model" bulk action.
- Testing never persists anything — the stored defaults are untouched.
- No change to provider credential config or to the existing per-provider Test action.
