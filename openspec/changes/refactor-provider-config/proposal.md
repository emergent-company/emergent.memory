## Why

The current provider configuration is split across three tables (`organization_provider_credentials`, `organization_provider_model_selections`, `project_provider_policies`) with a three-value policy enum, a silent env-var fallback, and a bug where the DB-stored generative model is ignored at call sites. This creates accidental complexity: credentials and model selection require two separate writes, the resolution path has four possible outcomes instead of two, and misconfigured projects fail silently rather than loudly.

## What Changes

- **BREAKING**: Drop `kb.organization_provider_credentials`, `kb.organization_provider_model_selections`, and `kb.project_provider_policies`. Replace with two tables of identical shape: `kb.org_provider_configs` (FK → `kb.orgs`) and `kb.project_provider_configs` (FK → `kb.projects`). Each row holds credentials + model selection together. One row per provider per scope, enforced by unique constraint.
- **BREAKING**: Remove the `kb.provider_policy` enum and the `none`/`organization`/`project` policy abstraction. Resolution is now purely structural: project row exists → use it; no project row → check org row; no org row → hard error. No silent env-var fallback in request context.
- **BREAKING**: `POST .../providers/:provider/credentials` and `PUT .../providers/:provider/models` collapse into a single `PUT /api/v1/organizations/:orgId/providers/:provider` endpoint that accepts credentials + optional model names in one request.
- On credential save: test live, sync model catalog, auto-select default generative and embedding models if not supplied.
- `cred.GenerativeModel` from the resolved config is now the authoritative model name at call sites; the caller's `modelName` parameter becomes a fallback only when the DB value is empty.
- CLI: `provider set-credentials` and `provider set-models` collapse into `provider configure <provider>`. `provider set-project-policy` becomes `provider configure-project <provider>`. Old commands removed.

## Capabilities

### New Capabilities
- `provider-config`: Unified provider configuration — one table per scope (org/project), credentials and model selection in a single write, live test on save, auto-default models. Replaces `llm-provider-config`, `provider-model-selection`, and `project-provider-policy`.

### Modified Capabilities
- `llm-provider-config`: Resolution hierarchy changes — project row present → use it, else org row, else hard error. Env-var fallback removed from request-context resolution path. **Replaces existing spec.**
- `project-provider-policy`: Policy enum removed. Project override is now represented by the presence/absence of a `project_provider_configs` row. **Replaces existing spec.**
- `provider-model-selection`: Model selection merged into credential save. Separate model selection table and endpoint removed. **Replaces existing spec.**
- `provider-cli`: `set-credentials` and `set-models` collapse into `configure`. `set-project-policy` becomes `configure-project`. **Replaces existing spec.**

## Impact

- **DB migrations**: 00042 drops three old tables, creates two new ones
- **Go**: `domain/provider/` — entity, repository, service, handler, routes all rewritten; `pkg/adk/model.go` — model name precedence fix
- **CLI**: `tools/emergent-cli/internal/cmd/provider.go` — commands reshaped
- **Skills**: `emergent-providers` and `emergent-onboard` SKILL.md updated
- **No UI changes** (UI for provider config does not exist yet)
- **No external API changes** beyond the provider config endpoints themselves
