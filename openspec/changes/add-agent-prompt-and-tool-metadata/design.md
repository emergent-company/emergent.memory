## Context

The transcript timeline is assembled by `agents.Repository.GetConversationFullHistory` from three tables: `kb.agent_runs` (run_start/run_end), `kb.agent_run_messages` (messages), and `kb.agent_run_tool_calls` (tool calls). The gateway renders the same timeline two ways — server-side templ for `/sessions/:id` and client-side JS for the live chat — and both consume the gateway's normalized item JSON.

Today the composed system instruction (`resolveInstruction` + workspace augmentation in `executor.runPipeline`) is only handed to the LLM; it is never persisted. The raw definition prompt is not equivalent: `resolveInstruction` appends the skills block, `SystemPromptAppendix`, and the tool-policy guidance, and `augmentInstructionWithWorkspace` may append workspace context.

## Goals / Non-Goals

- **Goals**: make the composed instruction auditable in the transcript; surface tool-call id + duration in both surfaces; keep one timeline vocabulary so both renderers stay in sync.
- **Non-Goals**: exposing the prompt through the A2A/`call_agent` reply path (already role-filtered out); changing what the LLM receives; back-filling prompts for historical runs (only new runs carry one).

## Decisions

### Persist the composed instruction as a `system` run-message

`runPipeline` computes `instruction` at ~line 1867 and finalizes it (workspace augmentation) before the run executes. Persist it once per run with `persistMessage(dbCtx, run.ID, "system", instruction, initialSteps)` immediately before the user message is persisted. Rationale:

- Reuses the existing `kb.agent_run_messages` table and jsonb content — no migration.
- It flows automatically through `GetConversationFullHistory` as a `message`/`system` record, so both surfaces and the existing history endpoints receive it without new plumbing.
- Role-filtering already treats `system` as non-reply everywhere it matters: `isAgentReplyRole` excludes it (A2A/`call_agent`), and the ADK session — not `agent_run_messages` — is the LLM's conversation memory, so persisting it does not feed it back to the model.

Alternative — a new `kb.agent_runs.system_instruction` column — was rejected: it needs a migration and a second plumbing path (run DTO + history DTO) for the same data the message timeline already transports.

### One prompt card at the top, deduped by content

The instruction is stable across a conversation's runs by design (that is what enables Gemini implicit prompt caching), so rendering every run's `system` record would duplicate identical cards. Both renderers show the first `system` message once at the very start of the conversation and skip later `system` records. `SystemPromptAppendix` is empty for normal chat, so normal conversations are unaffected by the dedupe.

### Id + duration as additive record fields

`ConversationHistoryItem` gains `id` (tool-call id) and already has `duration_ms`; populate `id` from `tc.ID`. The gateway mirrors both on `TimelineItem` and on its `AgentRunToolCall` DTO (duration already exists server-side in `AgentRunToolCallDTO`). Fields are omitempty so older payloads are unchanged.

## Risks / Trade-offs

- **Prompt visibility**: the composed instruction may include workspace/skills context. It is now recorded in the transcript and therefore visible wherever transcripts are (session viewer, live chat, session dumps). This is the requested behavior; the prompt is not a secret (it is sent to third-party model providers) and is not included in A2A replies.
- **Runs with no persisted run-messages**: older runs and any path that does not go through `runPipeline` simply have no `system` record; renderers omit the card.

## Migration Plan

No schema migration. New runs begin carrying the `system` record immediately; old sessions render exactly as before.

## Verification

- `go build ./...` + `go test ./...` in `apps/server` and `apps/web-ui/gateway`.
- `templ generate` after templ edits; `task lint`.
- Unit tests: history item carries tool id + duration; run message set includes a `system` row; gateway parse/render of a `system` item; JS renderer is covered by the existing Playwright e2e suite where applicable.
