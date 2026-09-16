## Why

The voice path is still single-owner and single-project even after the web console gained Zitadel session auth. The bridge worker authenticates to Memory with one static `MEMORY_TOKEN` + `MEMORY_PROJECT_ID` injected at process spawn, and no user or project identity flows from a voice call into the worker. Every signed-in user who starts a voice call runs against the same static owner/project, so voice cannot serve multiple users or projects.

## What Changes

- Make `/api/token` session-aware: for a session-authenticated caller, resolve the active project/org and the agent definition in *that* project, mint a short-lived project-scoped Memory token, and carry that binding to the bridge worker through the LiveKit room-dispatch metadata.
- Refactor the bridge worker to read per-session identity (project, org, agent-definition id, Memory token) from the room/job context instead of process-global env.
- Resolve the agent-definition id per session (from the user's active project) instead of once at supervisor spawn from the static project.
- Apply voice settings (STT/TTS provider, voice, language, options) from the session's active project rather than the static supervisor-injected env.
- Keep the existing `X-API-Key` programmatic path working unchanged (single-project, static token).
- **BREAKING (session-mode web voice):** a voice call now operates in the signed-in user's active project; the previous "voice always uses the static project" behavior is retired for session-authenticated calls.

## Capabilities

### New Capabilities
- `voice-session-tenancy`: a voice (LiveKit) session binds to the signed-in user's session and active project, with per-session Memory credentials and agent-definition resolution.

### Modified Capabilities
- `project-voice-settings`: the voice settings the bridge applies resolve from the session's active project/agent, not the static supervisor-injected environment.

## Impact

- `gateway/token.go`: session-aware token mint; resolve project/agent/definition per session; mint a project-scoped token; enrich dispatch metadata.
- `gateway/memory.go`: add a project-scoped token mint call (proxy Memory `POST /api/projects/:id/tokens`) and per-project agent-definition lookup.
- `gateway/supervisor.go`: stop injecting static `AGENT_DEFINITION_ID` / project identity; workers become name-keyed dispatchers.
- `memory_bridge/worker.py`, `config.py`, `memory_chat.py`: read per-session identity from the job context; drop process-global `MEMORY_TOKEN`/`MEMORY_PROJECT_ID`/`AGENT_DEFINITION_ID`.
- `memory_bridge/llm.py`: thread the per-session agent-definition id and conversation binding.
- Specs: new `voice-session-tenancy`; modified `project-voice-settings`.

### Out of scope (separate changes)

- iOS voice multi-user (iOS stays per-device `X-API-Key`, single-project).
- Editing voice settings per-user beyond the existing project-settings surface.
- Supervisor/bridge worker pooling across projects (still one worker per agent name).
