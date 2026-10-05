## Why

Document extraction fails at run time with `no generative model configured for project …` when the project has no resolvable generative model. The failure surfaces only after a job is enqueued, leaving the user with a broken run and no upfront guidance. The gateway already fetches the project's effective model config (`GetEffectiveModelConfig`) for the chat model warnings; it should use the same signal to refuse or disable extraction before a job is ever created.

## What Changes

- The document detail page disables (greys out) the "Extract data" button and shows a warning plus a link to `/settings/providers` when the project has no resolvable generative model.
- The web trigger (`POST /documents/:id/extract`) refuses the request with a redirect carrying the real error, and enqueues no job.
- The JSON API (`POST /api/documents/:id/extract`) refuses with `409 Conflict` and enqueues no job.
- Detection uses exactly the effective generative model: blocked when `GetEffectiveModelConfig` returns a non-nil config whose `GenerativeModel` is empty. A config fetch error fails open (captured, treated as not blocked) so a transient config outage never disables extraction — mirroring `chatModelWarnings`.

## Capabilities

### Modified Capabilities
- `document-browser`: extraction is offered only when a generative model is resolvable; otherwise the control is disabled and guidance links to providers settings; the server refuses the trigger.
- `document-api`: the trigger endpoint refuses extraction when no generative model is configured.

## Impact

- `gateway/documents.go`: `extractionModelMissing` helper + `errNoGenerativeModel`; guard `triggerExtraction`.
- `gateway/ui.go`: compute `blocked` in `uiDocument`, guard `uiTriggerExtraction`.
- `gateway/documents.templ`: `DocumentDetailPage`/`extractionCard` take a `blocked` flag; disabled button, warning alert, and providers link.
- Tests: `documents_test.go` render + route assertions; Playwright `document-extraction-ui.spec.ts` reworked to configure a provider before triggering.

## Scope

- **In scope**: gate the extraction trigger (UI + web + JSON API) on the resolvable generative model.
- **Out of scope**: validating that the configured model actually runs extraction; changing how memory resolves the effective model; gating other model-consuming surfaces.
