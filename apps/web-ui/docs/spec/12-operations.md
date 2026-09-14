# 12 — Operations

Cross-cutting concerns the roadmap depends on: observability, testing, migration, rollout.

## Observability

| Layer | What | Where |
|---|---|---|
| Memory (brain) | conversation messages, LLM call logs, usage | memory `chat_messages`, `llm_call_logs`, usage service — **durable** |
| Bridge (transport) | STT/TTS latency, audio events, turn timing | structured logs to supervisor (stdout/stderr → container log driver) |
| Supervisor (lifecycle) | worker spawn/stop/restart, crash backoff, reconcile diffs | Go structured logs |
| Tracing | optional OpenTelemetry | memory has Tempo integration; Memory can opt in later |

### Error tracking, tracing & session replay (Sentry)

Optional crash/error reporting, performance tracing, and session replay for the gateway and
the web UI. Both report to the same Sentry project:

| Env var | Meaning | Default |
|---|---|---|
| `SENTRY_DSN` | Sentry project DSN | empty → tracking disabled |
| `SENTRY_ENVIRONMENT` | environment tag on every event/transaction | `development` |
| `SENTRY_TRACES_SAMPLE_RATE` | trace sampling (0–1) | `1.0` in dev, `0.2` otherwise |
| `SENTRY_REPLAY_SESSION_SAMPLE_RATE` | session-replay sampling (0–1) | `1.0` in dev, `0.1` otherwise |
| `SENTRY_REPLAY_ON_ERROR_SAMPLE_RATE` | error-session replay sampling (0–1) | `1.0` |

The Go backend (`sentry-go`) reports recovered panics, 5xx handler errors, and memory REST
API failures (tagged with method/path/status), and runs a per-request transaction middleware
(continuing inbound `sentry-trace`/`baggage` headers) plus an `http.client` span per memory
call. The browser loads a pinned Sentry bundle (`bundle.tracing.replay.feedback.min.js`
v10.73.0) enabling `browserTracingIntegration` (page-load/navigation/fetch + Core Web Vitals)
and `replayIntegration` (text/inputs/media masked). Values flow to the browser via `<meta>`
tags (templ renders `<script>` verbatim). Everything is a no-op when `SENTRY_DSN` is empty.

Memory adds **no new durable log store** — the session JSONL (`/tmp/memory-sessions.jsonl`)
and the old admin sessions viewer retire. Conversation history lives in memory; the bridge
logs only transport-level timing for latency debugging.

## Testing

Per project convention (openspec `config.yaml`): **test-driven, unit tests minimum**.

- **Go gateway** — unit tests: route handlers (fake memory SDK), supervisor reconcile loop
  (fake process spawner + fake agent list), token mint (fake LiveKit API), auth middleware.
- **Bridge (Python)** — unit tests: audio→text→chat→TTS pipeline with **mocked** Deepgram,
  Cartesia, and memory SSE; turn-taking/away/exit-keyword logic against a fake chat stream.
- **Contract tests** — against a real (or test) memory instance: chat stream shape, agent
  create→chat round-trip, MCP registration.
- **E2E (stretch)** — web text chat smoke; voice smoke (iOS/Mac → LiveKit → bridge → memory).
  E2E is complementary, never a substitute for unit coverage.

## E2E (Playwright)

Browser tests in `tests/e2e/` run **against the live dev gateway** (tailnet
`alfred-dev:8095`/`8443`) on a real memory tenant. Four projects in
`playwright.config.ts`: `setup` (serial auth + tenant bootstrap, reuses the saved
session when present), `chromium` (read surface, parallel), `mutations`
(UI-mutating specs, single worker, self-cleaning), and `scenarios` (full-journey
scenarios that call a live LLM; single worker, `setup`-dependent, env-gated —
they skip fast when `E2E_SCENARIO_LLM_API_KEY` is unset). Read/mutation specs are
grouped into surface folders under `tests/e2e/specs/` (organizations, projects,
agents, skills-schedules, objects, schema, documents, sessions, settings, account,
shell); multi-flow files wrap their tests in `test.describe`. Scenario specs live
at the top level under `tests/e2e/scenarios/` and compose the shared UI-step
helpers in `tests/e2e/helpers/`.

Policy: **mutations run through the UI** — a spec whose title names a
create/edit/delete drives the real form. `page.request` is reserved for SEED
(fixtures/prerequisites), CLEANUP (`finally` teardown), and VERIFY (GET assertions
on server state). UI elements carry `data-testid` via go-daisy's `Attrs` mechanism
where IDs won't do: `stat-agents` · `stat-with-tools` · `model-select`.

## Linting & hooks

Adopt the go-daisy / emergent.memory tooling:

- **golangci-lint** — `.golangci.yml` + `task lint`.
- **lefthook pre-commit** — `gofmt` (staged files) + `go vet` + `go build`; the `lint` job
  runs `gofmt` + `go vet` + `golangci-lint`.
- `templ generate` before build when `.templ` files change.

## Migration (existing → new)

Two agents exist today and must move into memory as agent definitions:

| Today | Becomes (in memory) |
|---|---|
| `alfred-google-rt` (PL, Gemini realtime, HA tools) | `AgentDefinition` "alfred": PL system prompt, model (O2), HA MCP attachment |
| `diane` (EN, openai_compat, memory tools) | `AgentDefinition` "diane": EN system prompt, model, memory MCP attachment |
| HA function tools + catalog (`ha_tools.py`) | ha-mcp MCP server in the registry |
| env-file credentials | memory MCP registry secrets + provider/model config |

Exact create/seed calls depend on memory's contract (see verification in progress). A seed
script or blueprint provisions the two agents idempotently (mirroring the old `seed.py`).

## Rollout & verification

1. Provision memory: org/project, model config, the two agent definitions, MCP servers.
2. Build + unit-test the Go gateway + supervisor (fake backends).
3. Build + unit-test the bridge (mocked STT/TTS/chat).
4. Contract-test against memory.
5. Wire web UI → gateway → memory chat (text path end-to-end first).
6. Wire voice bridge (LiveKit) → cut over iOS/Mac.
7. Retire legacy entrypoints + admin.py + FastAPI control plane + SQLite store.

**Gate each step on:** `go build ./...` / `go vet` + unit tests (Go); pytest + lint (bridge);
server restart + manual chat smoke after UI/bridge changes.
