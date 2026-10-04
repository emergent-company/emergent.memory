## Why

The newest OpenAI models reject a request that combines `tools` with a graded reasoning effort on `POST /v1/chat/completions`, and some effort levels (e.g. `max`) exist only on the Responses API. The server currently speaks Chat Completions for every OpenAI-compatible vendor, so those models fail at the wire level. The fix must not regress the many relay/proxy OpenAI-compatible endpoints (OpenRouter, LiteLLM, local servers) that only implement Chat Completions.

## What Changes

- Add `openai-responses` as a first-class wire protocol (`ProtocolOpenAIResponses`) alongside `openai-chat`.
- Add a Responses adapter (`pkg/adk/openai_responses_model.go`) that POSTs to `{base}/responses` with the Responses request/response shape (`input` items, `instructions`, flat function tools, `output` items) and emits the same ADK `model.LLM` surface as the Chat adapter.
- Select Responses **data-driven by host**: a vendor definition declares `ResponsesHosts`; `ProviderDefinition.ProtocolForBaseURL` promotes an `openai-chat` vendor to `openai-responses` only when the resolved base URL's host matches. `api.openai.com` is declared for the OpenAI vendor, so first-party OpenAI uses Responses while any relay/proxy host keeps Chat Completions.
- Thread the selection through credential resolution (`applyDefinition`), the ADK model factory (DB and env-var paths), and the catalog test-connection generate path.
- Support a graded reasoning effort (`reasoning.effort`) on the Responses adapter, sent in the same request as `tools`.
- No database migration.

## Capabilities

### New Capabilities

<!-- none -->

### Modified Capabilities

- `openai-compatible-llm`: adds the Responses wire protocol, host-based protocol selection, and the tools-plus-reasoning-effort request shape.

## Impact

- `apps/server/domain/provider/protocol.go` — new `ProtocolOpenAIResponses` constant.
- `apps/server/domain/provider/registry.go` — `ProviderDefinition.ResponsesHosts`.
- `apps/server/domain/provider/protocol_select.go` — `ProtocolForBaseURL`.
- `apps/server/domain/provider/builtin.go` — OpenAI declares `ResponsesHosts`.
- `apps/server/domain/provider/service.go` — `applyDefinition` resolves the protocol base-URL-aware.
- `apps/server/domain/provider/catalog.go` — generate/embed dispatch handles `openai-responses`.
- `apps/server/pkg/adk/openai_responses_model.go` — the Responses adapter and shared host-matching helpers.
- `apps/server/pkg/adk/model.go` — dispatch for `openai-responses` on the DB and env-var paths.
- Tests: adapter wire-shape/parsing, selection (first-party vs relay), tools+reasoning-effort, orphan tool pairing.
