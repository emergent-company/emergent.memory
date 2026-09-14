# Memory should always report the agent's resolved generative model

**Status:** done
**Created:** 2026-09-08
**Source:** [2026-09-08-agent-model-default-display](sessions/2026-09-08-agent-model-default-display.md)

## Done (2026-09-10)

- **#403 merged + deployed**: GET and LIST agent-definitions report
  `effectiveModel` (project config → provider-credential generative model);
  per-agent overrides now reported unconditionally.
- Gateway follow-up landed: **memory.web-ui #19** removed the agents-page
  per-agent `GetAgentDefinition` N+1 (list carries the model) and added
  `EffectiveModel` to `AgentDefinitionSummary`.

## Implementation (2026-09-09)

Implemented on **emergent.memory branch `feat/agent-effective-model`** (pushed to
origin; awaiting review/merge):

1. `modelconfig.Service.ResolveGenerativeModel` now falls back past an empty
   project config to the project's **provider-credential generative model**
   (new `CredentialService.DefaultGenerativeModel`, project-explicit so
   reporting can resolve for a project different from the session's), surfaced
   as new `ModelSourceProvider`. Env-var models are deliberately NOT consulted
   (test-only path — production always wires the resolver), so run outcomes
   are unchanged.
2. GET `/agent-definitions/:id` reports the per-agent override
   **unconditionally** (previously suppressed when no project default was
   set), else the resolved default.
3. LIST `/agent-definitions` adds `effectiveModel` to the summary DTO; default
   resolved **once per project**, override wins per agent — removes the
   gateway N+1 need.
4. `pkg/adk` internal fallback kept as a defensive copy w/ canonical-source
   note; drift comments + swagger `ModelSource` enum hand-patched.

Note: the "What" wording (org config → runtime/env default) predates design
review — org-level model config is deprecated-only and env models are never
used on the wired production path; the implemented chain is project config →
provider credential → none.

Follow-ups after merge/deploy: gateway agents-table N+1 removal (stop the one
GET per agent once the list carries `effectiveModel`) and a live check that
the UI shows the resolved model for an auto agent without a project default.

## What
In the **emergent.memory** repo (`/root/alfred/.slim/clonedeps/repos/emergent-company__emergent.memory`), make the memory API report the model an agent definition will actually run with, even when no project default is configured:

1. `ResolveGenerativeModel` is currently **project-config-only** (`domain/modelconfig/service.go` — empty config → `ModelSourceNone`). Extend the resolution that feeds `AgentDefinitionDTO.EffectiveModel` so an auto agent still resolves a real default (org config → runtime/env default) instead of returning empty. Today the GET handler only sets `effectiveModel` when the resolver returns non-empty, so an agent on a project without a default comes back `effectiveModel: null`.
2. Include the resolved `model` / `effectiveModel` on the **list** endpoint (`GET /api/projects/:projectId/agent-definitions`) — the gateway's agents table currently does one GET per agent (N+1) because summaries omit the model.

## Why
The gateway UI can only show the exact model an agent uses ("which model is being used?") when the value is resolvable. With no project default set, both the gateway's project-default read and memory's `effectiveModel` are empty, so the agent page falls back to the opaque "auto — default model" (plus a warning). The runtime/env default exists on the memory side but is never surfaced.

## Depends on
- none (gateway already decodes `EffectiveModel` and renders it when present — commit `11183b0`)

## Notes
- Confirmed live: api.dev memory returns `effectiveModel: null` for an auto agent on a project with empty model-config and zero configured providers.
- Deployment to api.dev / prod memory is a separate concern (this is a cross-repo PR in emergent.memory).
- Record the resolved decision in `docs/spec/00-vision.md` (D26) and note it in `docs/spec/04-go-application.md` once those files are free of concurrent-session WIP.
