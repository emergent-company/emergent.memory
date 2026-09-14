# Memory — System Specification

Self-hosted, multi-agent voice + chat platform. A thin Go application
over two backends: **Emergent Memory** (the agent brain) and **LiveKit** (voice transport).

This directory is the source of truth for the system design. Read in order.

## Structure

| File | Element | Summary |
|---|---|---|
| [00-vision.md](00-vision.md) | Vision & decisions | Goals, non-goals, principles, decision log, open items |
| [01-architecture.md](01-architecture.md) | Architecture | System diagram, component responsibilities, ownership, legacy→target map |
| [02-memory-backend.md](02-memory-backend.md) | Emergent Memory | Capabilities used, interfaces (Chat/MCP/A2A/ACP), tenancy, models |
| [03-agent-model.md](03-agent-model.md) | Agent model | Agent definition schema, MCP attachment, skills & A2A (deferred) |
| [04-go-application.md](04-go-application.md) | Go application | Gateway API + worker supervisor + web UI |
| [05-voice-bridge.md](05-voice-bridge.md) | Voice bridge | Python LiveKit worker: audio ⇄ memory chat |
| [06-livekit.md](06-livekit.md) | LiveKit | Voice transport, rooms, agent dispatch, token minting |
| [07-clients.md](07-clients.md) | Clients | Web (go-daisy), iOS, Mac wake-word |
| [08-deployment.md](08-deployment.md) | Deployment | Docker Compose, external services, env & secrets |
| [09-security.md](09-security.md) | Security | Trust boundary, auth, secret handling |
| [10-api-contracts.md](10-api-contracts.md) | API contracts | Interfaces between components |
| [11-reuse-from-diane.md](11-reuse-from-diane.md) | Reuse from Diane | What Memory lifts directly from the Diane project |
| [12-operations.md](12-operations.md) | Operations | Observability, testing, migration, rollout |
| [13-roadmap.md](13-roadmap.md) | Roadmap | Phased plan with dependencies and gates |
| [14-assistant-agent.md](14-assistant-agent.md) | General assistant ("operator") agent | One agent that reads/changes all settings via proposal→accept chat; blueprint + UI integration |

## Status

- **State:** Draft — architecture decisions locked (see [00-vision.md](00-vision.md)).
- **Two open decisions** remain (recorded in `00-vision.md`): build strategy, and the
  default brain model.
- **Roadmap** is written after the spec stabilizes.

## One-line mental model

> Memory is the brain. LiveKit is the voice pipe. Go is the thin operator.
> Python is the STT/TTS bridge between voice and brain.
