## Context

See `proposal.md` — Why. The bridge worker (`memory_bridge/worker.py`) is a separate process spawned by the Go supervisor, keyed by agent name via LiveKit's `rtc_session(agent_name=…)` dispatch. It authenticates to Memory with process-global env (`MEMORY_TOKEN`, `MEMORY_PROJECT_ID`, `AGENT_DEFINITION_ID`) inherited at spawn, and holds no per-session identity. The gateway already resolves per-request credentials for the web path via `sessionContext` (`tokenFor`/`projectIDFor`/`sessionHeaders` in `gateway/memory.go`); the voice path bypasses that entirely and hits Memory directly with the static token.

Key constraints that shape the approach:

- The worker's `@server.rtc_session(agent_name=config.AGENT_NAME)` decorator is evaluated at import, so a worker process is statically bound to one agent name (one worker per name, serving many rooms).
- The LiveKit join token minted by `gateway/token.go` embeds the `RoomAgentDispatch` (name + `Metadata`) **in the client's JWT**, which the client can decode — so the dispatch metadata must never carry a secret.
- Memory exposes `POST /api/projects/:projectId/tokens` (project-scoped API token, full value returned once) reachable with a Zitadel user token — the source for per-session project credentials.

## Goals / Non-Goals

**Goals:**

- Bind each voice session to the signed-in user's active project and org.
- Give the worker a per-session project-scoped credential and per-project agent-definition id + voice settings, without exposing the user's session token.
- Keep the `X-API-Key` programmatic path working unchanged (static project/token).

**Non-Goals:**

- iOS multi-user (iOS stays per-device `X-API-Key`, single-project).
- Supervisor/bridge worker pooling or per-project worker enumeration beyond the canonical-name assumption.
- Per-user voice-settings editing beyond the existing project-settings surface.
- Server-side LiveKit room lifecycle rework (moving room config out of the join JWT).

## Decisions

### D1 — Resolve binding at token mint, deliver server-side

`/api/token` resolves, for a session-authenticated caller, the active project/org, the requested agent's definition id *in that project*, the agent's voice language, and a project-scoped credential. This binding is delivered to the worker over a server-side channel — **not** in the LiveKit join JWT (whose room config is client-decodable).

- **Why:** the dispatch metadata already rides in the client's JWT (see Context); putting a credential there leaks it to the browser/app.
- **Alternative rejected:** embed `{project_id, token, …}` in `RoomAgentDispatch.Metadata` — client-readable, unacceptable.

### D2 — Per-session project-scoped Memory token

For a session-authenticated voice call, the gateway mints a short-lived project-scoped API token via Memory's `POST /api/projects/:projectId/tokens`, scoped to what the worker needs (`chat:use` plus the minimum for agent-question respond/cancel — verify exact scopes against Memory's route guards at implementation). The worker uses `Bearer <project-token>` directly against Memory (project-bound, so no `X-Project-ID` header needed).

- **Why:** least privilege, short-lived, revocable, project-bound, and the user's Zitadel token never leaves the gateway.
- **Alternative rejected:** pass the user's Zitadel access token + `X-Project-ID` to the worker — long-lived, needs client-secret refresh the worker can't do, and floats the user's real credential.

### D3 — Binding delivery channel: gateway-internal loopback endpoint

The gateway stores the resolved binding in a short-TTL server-side map keyed by room name, and exposes an internal endpoint (`/internal/voice-binding?room=…`) reachable only from the worker process (loopback + a shared `WORKER_INTERNAL_KEY` injected into the worker env at spawn, like `AGENT_NAME` today). The worker fetches and consumes its binding once at `entrypoint` start.

- **Why:** keeps the credential on the server (gateway → worker, both server processes); the room name alone is not a secret because the internal endpoint requires the shared worker key the client never sees.
- **Alternative rejected:** server-side room config via LiveKit `RoomServiceClient.CreateRoom` (clean, but a larger room-lifecycle change — deferred).

### D4 — Worker reads per-job binding, drops process-global creds

Refactor `worker.py` so `entrypoint(ctx)` parses the job metadata (`conversation_id`) and fetches the binding by `ctx.room.name`; `_build_session`/`_attach_chat_io` receive an explicit `binding` struct instead of reading `config.MEMORY_TOKEN`/`MEMORY_PROJECT_ID`/`AGENT_DEFINITION_ID`. `config.AGENT_NAME` stays as the dispatch key; language comes from the binding (with the existing global fallback when absent).

- **Why:** one worker process serves many rooms; identity becomes a per-job value, not process state.

### D5 — name → definition-id resolution moves from supervisor to token mint

The supervisor stops resolving `AGENT_DEFINITION_ID`/`AGENT_LANGUAGE` from the static project. It keeps spawning one worker per agent *name* (the LiveKit dispatch key), enumerated from the canonical/static project as today. The definition id + language are resolved at token-mint time in the *session's* project and delivered via the binding.

- **Why:** the definition id is project-scoped; only the mint path knows the caller's project.
- **Assumption:** agent names are canonical across projects (the "memory" family exists in every project). See Open Questions.

### D6 — Two-mode token endpoint

`/api/token` keeps one worker code path by always producing a binding, but sources it differently:

- **Session mode** (`AUTH_MODE=session` + session attached): per-session project/org, minted project token, per-project definition id + language.
- **Key mode** (`X-API-Key`): static `MEMORY_PROJECT_ID` + static `MEMORY_TOKEN` + definition id resolved from the static project — today's behavior, unchanged.

### D7 — Voice settings resolve per session from the active project

The binding carries the voice language (from the active project's agent definition `Config`) and, where the project voice settings surface provides them, the per-project voice options. STT/TTS API keys (Cartesia/Deepgram) and the transport/endpointing defaults remain server-side global env — they are provider secrets, not per-project values today.

## Risks / Trade-offs

- **[Credential leak via client JWT]** → never place credentials in room config/metadata; deliver via D3's server-side channel only.
- **[Binding map orphan/leak]** → short TTL, one-time consume, delete on worker fetch; keyed by room and gated by the shared worker secret.
- **[Project token lifecycle]** → short-lived + scoped; mint per call or cache per-project with a short TTL (see Open Questions).
- **[Agent names not canonical across projects]** → a project with a novel agent name has no running worker and dispatch fails. Mitigation: document; future work enumerates the union of names across projects.
- **[Worker hits Memory directly, bypassing gateway session logic]** → acceptable: the project token is project-bound and scoped, so no session header logic is needed on the worker path.
- **[X-API-Key path regression]** → key mode keeps the static binding; covered by unit tests on both modes.

## Migration Plan

1. Extend the token endpoint to resolve/mint and store the binding (session mode), and add the internal binding endpoint; the worker continues to read env (backward-compatible).
2. Add worker-side binding fetch; flip `_build_session`/`_attach_chat_io` to use it when present, falling back to env.
3. Remove env fallback and the supervisor's `AGENT_DEFINITION_ID`/`AGENT_LANGUAGE` injection once the worker path is stable.
4. Keep `X-API-Key` mode on the static path throughout.
5. Rollback: re-enable env fallback (supervisor still injects the static values until the final cleanup task).

## Open Questions

- Exact Memory scopes required for the project token (`chat:use` + agent-question respond/cancel) — verify against `emergent.memory` route guards at implementation.
- Whether agent names are canonical across projects; if not, how to enumerate the worker set (separate follow-up).
- Project-token mint-per-call vs short-TTL cache per project (performance trade-off).
