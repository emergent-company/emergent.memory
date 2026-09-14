# 08 — Deployment

## Compose boundary (D3)

The Memory Docker Compose packages **only the Memory app**: the Go binary + the Python
bridge runtime. External services are connected, not contained.

```
docker-compose.yml
┌─────────────────────────────────────────────────┐
│  memory  (one container)                         │
│   ├ /usr/local/bin/memory   (Go: gateway+supervisor+UI) │
│   └ /app/bridge             (Python: livekit-agents + deps) │
│        └ supervisor spawns: python -m memory_bridge --agent <name> │
└─────────────────────────────────────────────────┘
         │ connects to (external)
   ┌─────┼──────────┬───────────────┬─────────────┐
 LiveKit      Memory         LiteLLM        Deepgram/Cartesia
(home2 LXC)   (:5300)        (:4000)        (cloud APIs)
```

- **One container** with both Go and Python runtimes (the supervisor spawns Python child
  processes directly — D2).
- Worker processes are **children of the Go supervisor**, not separate compose services.

## As-deployed (home2 LXC 112)

Current production runs **directly in the LiveKit LXC** (not yet Docker):

| Item | Value |
|---|---|
| Host | `home2` LXC 112 (`livekit`) |
| App dir | `/opt/alfred` (Go binary + `memory_bridge/`) |
| Service | `systemd memory.service` (`EnvironmentFile=/opt/alfred/.env`, `Restart=always`) |
| Port | `:8082` (old `admin.py` still holds `:8080`; gateway moves there at cut-over) |
| Bridge Python | reuses `/root/alfred/.venv` (livekit-agents 1.6.9 + deepgram/cartesia/silero) |

The Dockerfile/compose above is the target packaging (go-daisy must be vendored first).

## Image

- Base: a Go + Python image (e.g. `golang` + `python:3.11-slim` layered, or a single
  multi-stage build).
- Python deps: `livekit-agents`, `livekit-plugins-deepgram`, `livekit-plugins-cartesia`,
  `livekit-plugins-silero`, `httpx`, `python-dotenv`.
- Go deps: `go-daisy`, `a-h/templ`, `labstack/echo`, memory Go SDK.

## External services (pre-existing, not managed by Memory)

| Service | Location | Purpose |
|---|---|---|
| LiveKit | home2 LXC 112, ws `:7880` | voice transport |
| Memory | `:5300` (this host) | brain + store |
| LiteLLM | `:4000` | LLM gateway (DeepSeek, etc.) for memory |
| Deepgram / Cartesia | cloud | STT / TTS |
| ha-mcp / user MCP servers | per-owner | agent tools |

## Env & secrets

Secrets are injected via env (compose `.env`), never baked into the image. The Go app is the
sole holder of `MEMORY_TOKEN` and LiveKit server keys; bridge workers get their env from the
supervisor (templated).

| Secret | Holder | Notes |
|---|---|---|
| `MEMORY_TOKEN` (`emt_*`) | Go app + bridge (injected) | scoped memory token |
| `LIVEKIT_API_KEY/SECRET` | Go app + bridge | server key for token mint + worker join |
| `TOKEN_API_KEY` | Go app | **optional admin** client `X-API-Key`; per-device keys issued via QR setup flow |
| `AUTH_MODE` | Go app | browser-auth posture: `session` (**default**) requires Zitadel; `dev` = explicit unauthenticated local dev |
| `SESSION_SECRET` | Go app | HMAC key for the session cookie (required in session mode) |
| `ZITADEL_ISSUER`, `ZITADEL_CLIENT_ID`, `ZITADEL_REDIRECT_URI` | Go app | OIDC issuer + public client (authorization-code + PKCE; no client secret) |
| `PUBLIC_BASE_URL` | Go app | externally-reachable base URL; must be `https://` in production (drives cookie `Secure` + canonical-host redirect) |
| `DEEPGRAM_API_KEY`, `CARTESIA_API_KEY` | bridge (injected) | STT/TTS |
| `LLM_API_KEY` | memory (its own config) | not held by Memory |
| `SENTRY_DSN`, `SENTRY_ENVIRONMENT` | Go app | observability (empty DSN = disabled) |
| `SENTRY_*_SAMPLE_RATE` | Go app | trace/replay sampling (see `12-operations.md`) |

## Sentry & CI/CD deploy secrets

Runtime Sentry vars (`SENTRY_DSN`/`SENTRY_ENVIRONMENT`/`SENTRY_*_SAMPLE_RATE`) are forwarded
from compose `.env` into the container. The GitHub Actions deploy job (`build-publish.yml`)
also needs these **repo secrets** (GitHub → Settings → Secrets & variables → Actions), which
mirror `emergent.memory.infra`'s secrets:

| Secret | Purpose |
|---|---|
| `TS_OAUTH_CLIENT_ID` / `TS_OAUTH_CLIENT_SECRET` | Tailscale OAuth client for the CI runner |
| `PROD_SSH_KEY` / `PROD_SSH_USER` / `PROD_TAILSCALE_HOST` | SSH to the prod host |
| `GHCR_PAT` / `GHCR_USER` | GHCR login on the prod host for `docker pull` |

Deploy fails at the Tailscale step ("OAuth identity empty") when any of these are unset.
GitHub never exposes secret values (write-only), so they must be copied manually from the
infra repo or promoted to org-level secrets.

## Persistence

Memory holds **no volume**. All durable state is in memory's Postgres. A restart of the
Memory container loses only in-memory worker PIDs (reconciled on startup).

## Networking

- Memory → memory, LiteLLM: HTTP on the tailnet/LAN.
- Memory → LiveKit: ws `:7880` (tailnet).
- Clients → Memory: HTTPS `:8080` (tailnet).
- Clients ↔ LiveKit: WebRTC (UDP media range, tailnet).

## Startup sequence

1. Compose starts the `memory` container.
2. Go app boots: loads config, connects to memory (health), starts HTTP + web UI.
3. Supervisor reconciles: lists enabled agents from memory → spawns a bridge worker per
   agent.
4. Ready: web UI serves; iOS/Mac can join LiveKit rooms and be dispatched to workers.
