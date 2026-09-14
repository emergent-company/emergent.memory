# 10 — API Contracts

The interfaces between components. Shapes are **indicative** (final schemas are owned by
memory's OpenAPI + the Go app's OpenAPI); this pins the *direction* of each call.

## 1. Clients → Go app (gateway)

Auth: `X-API-Key: <per-device key>` on every `/api/*` request, **or** a valid session
cookie when `AUTH_MODE=session` (see §1b). Per-device keys are issued by the QR setup flow
(see §1a); `TOKEN_API_KEY` is an **optional admin key** that also passes. In dev mode there
is no open mode — a missing or unknown key is `401`.

```
GET    /api/agents                     → list agents
POST   /api/agents                     → create agent {name, system_prompt, model, tools, ...}
GET    /api/agents/{id}                → one agent
PUT    /api/agents/{id}                → update
DELETE /api/agents/{id}                → delete
POST   /api/agents/{id}/activate       → enable
POST   /api/agents/{id}/deactivate     → disable

GET    /api/orgs                       → list orgs visible to the session user
POST   /api/orgs                       → create org {name}
GET    /api/projects                   → list projects visible to the session user
POST   /api/projects                   → create project {name, orgId}; becomes active
POST   /api/projects/{id}/activate     → switch the session's active project
GET    /api/members                    → list members of the active project
DELETE /api/members/{userId}           → remove a project member (403 last-admin)
GET    /api/invites                    → list invites sent for the active project
POST   /api/invites                    → invite {orgId, projectId?, email, role}
GET    /api/invites/pending            → pending invites for the signed-in user
POST   /api/invites/accept             → accept {token}
POST   /api/invites/{id}/decline       → decline
DELETE /api/invites/{id}               → revoke/cancel
GET    /api/users/search?email=        → search registered users (≤10)
GET    /api/user/profile               → signed-in user profile
PUT    /api/user/profile               → update profile
PUT    /api/user/avatar                → upload profile photo (multipart `file`, image/* allowlist, ≤512 KiB, ≤4096×4096 px)
GET    /api/user/avatar                → stream the profile photo (404 when none)
DELETE /api/user/avatar                → remove the profile photo

GET    /api/mcp-servers
POST   /api/mcp-servers                → {name, url, transport, headers, allowlist}
GET/PUT/DELETE /api/mcp-servers/{id}

POST   /api/chat        (SSE)          → {agentId, conversationId?, message}
GET    /api/conversations
GET    /api/conversations/{id}/messages
GET    /api/conversations/{id}/history    → typed timeline (messages + runs + tool calls)
GET    /api/conversations/{id}/dump       → formatted session dump (?format=text|json, default text)

POST   /api/token                      → {identity, agent, room?} → {server_url, participant_token}
GET    /api/sessions                   → session log
GET    /api/session?room=…             → session records
GET    /api/memories/capability?agent=…→ {agent, hasMemory}
GET    /api/memories?agent=…&query=…   → memories
```

### 1a. Device setup (QR onboarding, NO auth)

The Project Settings page renders a QR encoding
`{"setupURL": "<base>/api/setup", "token": "<one-time 32-hex>"}`. The current
token is persisted in memory's settings (`ios_setup_token` / `current` /
`{"token","createdAt","used"}`) so it survives gateway restarts (`air` hot-reload),
and is reused across page reloads until consumed or expired (10 minutes), then a
fresh one is minted — single use.

The base URL is `PUBLIC_BASE_URL` (`scheme://host[:port]`, no trailing slash) when
configured — pin it when the gateway sits behind a proxy/hostname so the phone
hits the externally-reachable origin — else derived from the request Host (http).

```
POST /api/setup   (no X-API-Key)       body {"token": "<one-time>"}
→ 200 {"serverURL": "<ws url>", "tokenEndpoint": "<base>/api/token",
        "apiBaseURL": "<base>/", "apiKey": "<per-device key>",
        "ttsStrategy": "client|server"}
→ 400 {"error": "…"}   empty/invalid body
→ 401 {"error": "…"}   unknown / expired / already-used token
```

`ttsStrategy` tells the client whether to play server-side audio (`server`,
`TTS_PROVIDER=cartesia`) or do local TTS from text (`client`, any other value).

Device keys are stored in memory's project settings as a single registry:
category `ios_device_keys`, key `registry`, value
`{"devices": {"<key-hex>": {"createdAt": "<RFC3339>"}}}` (memory has no
list-by-category route, so per-key settings could not be enumerated). The
Project Settings page lists the registry (keys masked) and offers a revoke
form: `POST /settings/devices/:key/revoke` (PRG → 303 back to `/settings`).

### 1b. Browser session (Zitadel OIDC, `AUTH_MODE=session`, opt-in)

Auth routes are public (no key/session required in session mode; `/auth/start` and
`/auth/callback` are the OIDC endpoints):

```
GET  /auth/login        → render the sign-in page (its action targets /auth/start)
GET  /auth/start        → 302 to Zitadel authorize (authorization-code + PKCE S256)
GET  /auth/callback     → validate state, exchange code, set the session cookie → 302 /
POST /auth/logout       → clear the session cookie → 302 /
```

The session cookie is stateless and HMAC-signed (`SESSION_SECRET`), carrying
`{access_token, refresh_token, active_project_id, org_id, name, email, picture, avatar_override_url, exp}`
(`avatar_override_url` is the uploaded photo's gateway-relative path, empty when not overridden).
UI page routes require a valid session (else 302 → `/auth/login`); `/api/*` accepts a
session **or** a key. Session-token calls to memory add `X-Project-ID` (and `X-Org-ID`
when known); the shared `emt_*` API-key path adds no header (the token is already
project-bound). Access tokens are refreshed server-side (`grant_type=refresh_token`,
5-min grace window, expired-cookie recovery). The cookie's browser lifetime is
`SESSION_MAX_AGE` (default 30 days), independent of the access-token `exp`, so the
refresh token survives access-token expiry and renews it silently.

Auth/session cookies carry the `Secure` flag per `cookieSecure()`: an explicit
`PUBLIC_BASE_URL` pins it (`http://…` disables), and an empty value derives it from the
request scheme (`X-Forwarded-Proto`/TLS) so plain-HTTP dev hosts drop `Secure` while TLS
proxies keep it.

Host-only cookies (the OAuth state cookie and the session cookie) are scoped to the
request `Host`, so a `Host` that differs from the pinned `PUBLIC_BASE_URL`/`ZITADEL_REDIRECT_URI`
(e.g. the short Tailscale name `alfred-dev` vs `alfred-dev.tail0358fa.ts.net`) silently
drops them — surfacing as "missing oauth state". A canonical-host middleware 302s any
browser request on a non-canonical `Host` to `PUBLIC_BASE_URL` (path + query preserved),
skipping `/api/*`, `/internal/*`, `/static/*`, and `/assets/*` so non-browser clients are
never redirected.

## 2. Go app → Memory (verified from source)

Auth: `emt_*` token (Bearer). Project is **derived from the token** — no `X-Project-ID`
header. Scopes: `agents:read agents:write chat:use data:read data:write schema:read
schema:write projects:read projects:write admin`.

### Agent definition CRUD
`POST /api/projects/:projectId/agent-definitions` (scope `agents:write`):
```json
{ "name": "my-agent", "systemPrompt": "…",
  "model": {"name": "deepseek/deepseek-v4-flash", "temperature": 0.7, "maxTokens": 2048},
  "tools": ["search-knowledge"], "bannedTools": [], "skills": [], "autoLoadSkills": false,
  "flowType": "single", "visibility": "project", "dispatchMode": "sync" }
```
- `name` required; `model.name` must contain a `/` provider prefix; duplicate name → 409.
- A definition is chat-capable **immediately** — no runtime `Agent`, no enable step.

### Model config (only if `model` omitted on the definition)
`PUT /api/v1/projects/:projectId/model-config`
`{ "generativeModel": "…", "embeddingModel": "…" }` — note the `/api/v1/` prefix, no scope check.

### Provider credentials (org-level, encrypted at rest)
Must exist or chat returns `503 no_provider`. Set via
`memory provider configure <google|vertex|openai|deepseek>`.

### Chat relay
`POST /api/chat/stream` (scope `chat:use`):
```json
{ "conversationId": "uuid?", "message": "… (required)", "agentDefinitionId": "uuid?" }
```
- `agentDefinitionId` is honored **only on a new conversation** — bind it at conversation creation.
- SSE is **data-only frames** (no `event:` name); dispatch on the `type` field inside `data`.

### MCP server registration
`POST /api/admin/mcp-servers` (scope `admin:write`):
```json
{ "name": "foo", "type": "http", "url": "…/mcp", "headers": {"Authorization": "Bearer …"}, "enabled": true }
```
`type` ∈ `stdio | sse | http`. Then `POST /api/admin/mcp-servers/:id/sync` to fetch tools.

### Conversation CRUD
`GET/POST /api/chat/conversations` · `GET /api/chat/:id` · `GET /api/chat/:id/history`
(typed timeline) · `PATCH/DELETE /api/chat/:id` · `POST /api/chat/:id/messages`.
Resume = `POST /api/chat/stream` with `conversationId` (last-10-messages history).

### Skills (deferred — D10)
`GET/POST/PATCH/DELETE /api/projects/:projectId/skills`, body `{name, description, content(markdown)}`.

### Project transfer between orgs (org tenancy)
`POST /api/projects/{id}/transfer`, body `{"orgId": "<destination-org-id>"}` — reparents the
project to another org the caller belongs to; identity/history/settings preserved (no
delete+recreate), only the owning org changes. Authz enforced service-side: `org_admin` of
the project's current org **and** member of the destination org. Gateway entry point is the
org-view Transfer action (`POST /projects/transfer`); spec: `openspec` change
`transfer-project` → `specs/project-transfer/spec.md`.

## 3. Bridge → Memory (chat streaming)

The bridge speaks the **same** Chat API as the Go app (a first-class memory client):

```
POST /api/chat/stream  { conversationId, agentDefinitionId, message: <STT text> }
→ data: {"type":"meta","conversationId":…}
→ data: {"type":"thinking","id":"…","role":"operator","text":"…","done":false}  ── agent reasoning (streamed deltas, requested)
→ data: {"type":"token","token":"…"}    ── token deltas → TTS starts early
→ data: {"type":"mcp_tool","tool":"…","status":"started|completed|error",…}  ── agent tool call
→ data: {"type":"done","runId":?}
```

SSE frames are **data-only** (no `event:` names) — the bridge dispatches on `type`.

### Thinking event (requested — pending memory implementation)

Memory surfaces the agent's reasoning as a streamed, collapsible "Thinking" block.
Today memory **strips** thinking from the chat stream (it is persisted post-hoc as
`operator` messages carrying `content.function_calls`, but never emitted live), so the
stream must gain a distinct event:

```
data: {"type":"thinking","id":"<stable segment id>","role":"operator|reasoning|planning","text":"<delta>","done":<bool>}
```

- `id` — stable per reasoning segment; a new id starts a new block.
- `role` — `operator` (planning monologue before tool calls, the value persisted in
  history), `reasoning` (raw chain-of-thought / `reasoning_content`), or `planning`.
  Default `operator`.
- `text` — incremental UTF-8 delta to append to the segment.
- `done` — true when the segment is complete; the final answer still streams via `token`.

The Go gateway passes `thinking` through verbatim (`rewriteChatStream` default branch);
`chat.js` renders it as a live collapsible block keyed by `id`. Until memory emits it,
thinking is visible only in history (operator messages with `function_calls`).

## 4. LiveKit dispatch & token

- Room config dispatch: `RoomAgentDispatch(agent_name=<agent.name>)` embedded in the room
  JWT (`roomConfig.agents`).
- Bridge registers `rtc_session(agent_name=<AGENT_NAME>)`; the name is the 3-way identity
  (agent name in memory == LiveKit dispatch name == worker env).

## 5. Bridge signal topics (client-facing)

| Topic | Payload | Meaning |
|---|---|---|
| `lk.agent.ready` | `"ready"` | mic subscribed — play cue-to-speak chime |
| `lk.agent.events` | `user_state_changed` | away → end session |
| `lk.transcription` | user text | (optional) client-side exit-keyword scan |

## 6. External MCP servers (agent tools)

Registered in memory; the agent's chat loop calls them via MCP (streamable_http / sse).
No Memory component touches them directly.

## Contract stability notes

- Memory's API is the **source of truth** for agent/chat/MCP — the Go app is a thin adapter,
  so memory API changes surface as Go adapter changes (not data-model churn in Memory).
- The Go app's client API (`/api/agents`, `/api/chat`, …) is Memory's **stable public
  surface** — clients depend on it, not on memory directly.
- SSE event names are fixed by memory (`TokenEvent`, `MCPToolEvent`, `DoneEvent`); the Go
  app passes them through and the bridge consumes them directly.
