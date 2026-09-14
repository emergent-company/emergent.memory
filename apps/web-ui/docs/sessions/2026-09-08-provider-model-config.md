# 2026-09-08 — Provider/model config: credential-prefixed selectors, fallback config, real error surfacing

## Goal

Diagnose "memory service unavailable" on chat start, then make the model-selection UI
correct and complete: credential-prefixed model dropdowns, configurable provider fallback
models, and real memory errors surfaced to the client.

## Outcome

**Done.** Four commits on `master` plus one memory-repo PR:

- `5c91256` — surface real memory errors in chat; credential-prefix default-model dropdown.
- `ba1a96c` — lint cleanup (De Morgan in `auth_ui_test.go`).
- `9f20bca` — expose provider fallback generative/embedding model fields (text inputs).
- `f74de1f` — prefixed provider-grouped dropdowns for the fallback fields.
- [emergent.memory PR #383](https://github.com/emergent-company/emergent.memory/pull/383) —
  strip routing prefix from resolved provider model names (open, not yet merged).

Also fixed a live outage with a **config change** (no code): the project default generative
model was `deepseek/deepseek-v4-flash`, but deepseek models are served through the `openai`
credential provider (LiteLLM proxy); memory resolves the `provider/` prefix as the credential
provider, so `deepseek/…` failed with "no deepseek provider config found" (masked by the
gateway as "memory service unavailable"). Changed the model-config to
`openai/deepseek-v4-flash`; chat stream verified returning HTTP 200.

## Decisions

- **Default-model dropdown uses the credential provider prefix** (`openai/deepseek-v4-flash`),
  not the catalog's routing provider (`deepseek/…`) — memory's `CreateModelWithName` resolves
  the prefix against configured credential providers; a routing prefix breaks at runtime.
- **Provider fallback fields are prefixed dropdowns** (same convention as the default-model
  panel) — user requested "particular model from particular provider"; memory strips the prefix
  when resolving, so the prefix is routing-only.
- **Fix the prefix strip in the memory backend (PR #383), not the gateway** — the resolved
  credential builders (`decryptProjectConfig`, `buildTempResolvedCred`) kept the prefix raw,
  which broke the standalone test endpoint and the upsert-time embedding test; centralizing the
  strip in the resolved credential fixes every consumer.
- **Surgical `git add -p` commits** — the shared `/root/alfred` working tree carried active
  parallel-session WIP entangled in the same files; staged only own hunks.

## Changes

- `gateway/memory.go` — `parseMemoryError` helper (parses `{"error":{"code","message"}}` /
  `{"error":"…"}`); `doH` and `ChatStream` both use it.
- `gateway/handlers.go` — `chat` returns `err.Error()` (real memory message) instead of the
  generic `"memory service unavailable"`.
- `gateway/settings_providers.go` — `defaultModelCatalog` (credential-prefixed model options);
  `providerModelOptions`, `providerFallbackCurrent`, `prefixedModelInCatalog`; provider prefill
  helpers (`providerConfigGenerativeModel`/`EmbeddingModel`).
- `gateway/project_settings.templ` — `defaultModelSelect` uses `defaultModelCatalog`; new
  `providerModelSelect` component (grouped dropdown + `(current)` fallback for legacy bare
  values); provider form fallback fields switched from text inputs to dropdowns.
- `gateway/settings_handlers.go` — `providerConfigPageData` carries `GenerativeModels` /
  `EmbeddingModels`; populated in the add/edit/error form handlers.
- `gateway/settings_providers_test.go`, `gateway/handlers_test.go` — new tests (fallback
  dropdown render + current-value handling; chat error surfacing) + updated persist test to
  prefixed values.
- `gateway/auth_ui_test.go` — QF1001 De Morgan simplification.

Memory repo (PR #383): `apps/server/domain/provider/service.go` — `stripModelPrefix` helper
applied in `decryptProjectConfig` + `buildTempResolvedCred`, redundant inline strip removed;
`service_test.go` — `TestStripModelPrefix` + prefixed `TestDecryptProjectConfig`.

## Verification

Run from `/root/alfred/gateway` (module now `github.com/emergent-company/memory.web-ui`):

- `PATH="/root/go/bin:$PATH" templ generate` — ok.
- `go build ./...` — ok.
- `go test ./...` — ok (both packages).
- `PATH="/root/go/bin:$PATH" golangci-lint run --new-from-rev HEAD ./...` — 0 issues.
- Live check: `POST /api/chat/stream` against `memory.emergent-company.ai` returned HTTP 200
  with a real reply after the model-config fix.

Memory repo (PR #383), run from the clone `apps/server`:

- `go test ./apps/server/domain/provider/...` — ok.
- `go build ./apps/server/...` — ok.

## Open questions / follow-ups

- **Memory PR #383 awaiting merge** — the gateway's prefixed fallback values depend on it; until
  it lands, configuring a *prefixed* embedding model fails the upsert test (generative is already
  stripped). See `docs/tasks/merge-memory-strip-model-prefix.md`.
- **Embedding model migration** — model-config `google/gemini-embedding-001` (safe until 2028) vs
  provider fallback `gemini-embedding-2-preview` (preview, past earliest shutdown). Migrating to
  stable `gemini-embedding-2` requires re-indexing (incompatible embedding spaces). See
  `docs/tasks/migrate-embedding-model-gemini-2.md`.
- **Repos renamed mid-session** — `alfred` → `memory.web-ui` (remote now
  `emergent-company/memory.web-ui.git`, module `github.com/emergent-company/memory.web-ui`),
  and `alfred_bridge` → `memory_bridge`. Done by a parallel lane, not this session.

## Tasks

- [merge-memory-strip-model-prefix](../tasks/merge-memory-strip-model-prefix.md) — merge memory PR #383 + re-verify fallback.
- [migrate-embedding-model-gemini-2](../tasks/migrate-embedding-model-gemini-2.md) — migrate embedding model to stable gemini-embedding-2 (re-index).
