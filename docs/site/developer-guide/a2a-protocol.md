# A2A and A2UI

Memory exposes an [Agent2Agent (A2A)](https://a2a-protocol.org/) agent interface
that other agents and agent platforms can discover and call, and it emits
[A2UI](https://a2ui.org/) surfaces so those callers can render agent output as
UI rather than prose.

This page is the compatibility contract: what version of each protocol the
server implements, which transports and catalogs it advertises, and what it does
*not* support. Read it before pointing a registry, a host, or another agent at
this server.

## Compatibility matrix

| Surface | Implemented | Notes |
|---|---|---|
| **A2A protocol** | **v1.0** only | All other versions are rejected with `VERSION_NOT_SUPPORTED` |
| **A2A transport** | `HTTP+JSON` | `application/a2a+json`; no gRPC, no JSON-RPC |
| **A2A streaming** | Yes | `POST /message:stream` (SSE), `POST /tasks/{id}:subscribe` |
| **A2A push notifications** | No | Every push-notification route answers `PUSH_NOTIFICATION_NOT_SUPPORTED` |
| **A2A extended card** | Yes | Per-project skills; requires `agents:read` |
| **A2UI protocol** | **v0.9.1** | Encoded in the extension URI and stamped on each surface envelope that carries a `version` (the `deleteSurface` teardown frame omits it) |
| **A2UI catalogs** | `memory-basic` | Custom catalog; see [A2UI catalogs](#a2ui-catalogs) |
| **A2UI integration** | `A2UI-over-A2A` card extension | Surfaces stream as `application/a2ui+json` data parts |
| **Auth** | Bearer project API token (`emt_…`) | The card declares this bearer scheme only — no OAuth2/OIDC scheme; the OAuth flow issues no `agents:*` scope |
| **Tenant selection** | `X-Project-ID` header, or a project-scoped token | Never path-scoped |

## Discovery

The A2A routes are served by the **API host** (`APP_URL`), which is a different
host from the web UI (`WEB_UI_URL`). Card-documented asset URLs therefore point at
the web UI; card-documented endpoints point here.

### Global agent card

```bash
curl https://api.memory.emergent-company.ai/.well-known/agent-card.json
```

Unauthenticated. Returns the static, tenant-blind platform card: name,
description, provider, the advertised interface, the A2UI and agent-metadata
extensions, and a static skills directory describing what the platform does. It
never contains project, org, or tenant identifiers.

### Extended agent card

```bash
curl https://api.memory.emergent-company.ai/extendedAgentCard \
  -H "Authorization: Bearer $MEMORY_TOKEN" \
  -H "X-Project-ID: $PROJECT_ID"
```

Requires `agents:read`. Returns the same card with `skills` derived from the
project's **external-visibility agent definitions**, one skill per agent, plus
the bearer security declaration. This is the card to use when you need to know
which agents a caller can actually invoke.

## Authentication and tenancy

Every route except the global card requires a Bearer token. This page documents
the **project API token** (`emt_…`), which is the supported integration
credential; the scope below is the token's, checked against the `scopes` list the
token was minted with:

| Route | Scope |
|---|---|
| `POST /message:send` | `agents:write` |
| `POST /message:stream` | `agents:write` |
| `GET /tasks`, `GET /tasks/{id}` | `agents:read` |
| `POST /tasks/{id}:cancel` | `agents:write` |
| `POST /tasks/{id}:subscribe` | `agents:read` |
| `GET /extendedAgentCard` | `agents:read` |

The card declares a single bearer security scheme, for `emt_…` tokens, and no
OAuth2 or OIDC scheme. The server does run an OAuth 2.1 authorization server
(`/.well-known/oauth-authorization-server`, authorization-code with PKCE), but its
scope allowlist — `graph:read`, `graph:write`, `schema:read`, `projects:read`,
`chat:use` — does not include `agents:read` or `agents:write`, so a token obtained
through it is refused here with an insufficient-scope error. Mint a project API
token carrying the scope you need (see [Not supported](#not-supported)).

The project comes from the `X-Project-ID` header, or from the token itself when
the token is project-scoped (a project-scoped token cannot be redirected to a
different project by the header). A request with neither is rejected with
`PROJECT_REQUIRED`.

## Message flow

`POST /message:send` runs an agent and returns a buffered envelope.
`POST /message:stream` runs the same agent and streams SSE events. Both accept an
A2A `Message` carrying text parts and, optionally, an `a2uiAction` metadata entry
(see [Responding to a surface](#responding-to-a-surface)).

## A2UI

### Advertised extension

The card advertises the A2UI-over-A2A binding:

```json
{
  "capabilities": {
    "extensions": [
      {
        "uri": "https://a2ui.org/a2a-extension/a2ui/v0.9.1",
        "params": { "supportedCatalogIds": ["memory-basic"] }
      }
    ]
  }
}
```

`supportedCatalogIds` is the list of catalog ids this server can resolve for
`createSurface.catalogId`.

### Streamed surfaces

A UI message is emitted as an A2A data part whose `metadata.mimeType` is
`application/a2ui+json`. The `data` member is an ordered array of A2UI envelope
messages (`createSurface`, `updateComponents`, `updateDataModel`,
`deleteSurface`). A2UI-incapable clients ignore the part and read the text parts
instead, so one stream serves both renderer classes.

### A2UI catalogs

Memory ships one catalog, `memory-basic`, with nine components tuned for agent
approval and review flows rather than for general layout:

| Component | Purpose |
|---|---|
| `proposal` | Review, accept or reject a proposed change |
| `approval` | Gate a tool call behind an explicit decision |
| `question` | Ask the user a question and resume the run with the answer |
| `code` | Show a code block with language metadata |
| `entity` | Preview a knowledge-graph entity |
| `object-form` | Edit a graph object's fields |
| `todo` | Show task progress |
| `result` | Show a structured operation result |
| `sources` | Cite the documents and chunks a claim came from |

The catalog is closed: agent output is validated against it, and output that
falls outside it is repaired by re-prompting the model. Surface messages naming
any other `catalogId` are rejected with an `INVALID_ARGUMENT` issue list.

### Responding to a surface

When the user acts on a surface, send the action back on the next message as
message metadata:

```json
{
  "role": "ROLE_USER",
  "parts": [{ "text": "Approved." }],
  "metadata": {
    "a2uiAction": { "surfaceId": "surface-1", "action": { "componentId": "approve", "response": "accept" } }
  }
}
```

The action is folded into the user message that resumes the run, so the agent
sees both the decision and the accompanying text.

## Not supported

Be explicit about these before integrating:

- **A2A v0.3 and earlier.** The server implements v1.0 and rejects any other
  `A2A-Version` with `VERSION_NOT_SUPPORTED`. Registries and hosts that speak
  only v0.3 (Gemini Enterprise among them) need a v0.3 compatibility shim in
  front of this server.
- **Path-scoped project addressing.** The project is a header or a token claim,
  never a path segment, so a host that cannot send custom headers cannot select
  a project.
- **OAuth for agent calls.** The card declares a bearer `emt_…` project API token
  scheme and no OAuth2 or OIDC scheme. The deployment's OAuth 2.1 authorization
  server issues tokens scoped to `graph:read`, `graph:write`, `schema:read`,
  `projects:read` and `chat:use` — not `agents:read` or `agents:write` — so a
  token a third-party app obtains through that flow cannot call these routes.
  Mint a project API token with the scope you need instead.
- **Standard A2UI catalogs.** Only `memory-basic` is served. A renderer that
  implements just the standard A2UI component set cannot draw these surfaces.
- **Push notifications.** Not implemented. The six routes exist only to answer
  `PUSH_NOTIFICATION_NOT_SUPPORTED`: `GET /tasks/{id}/pushNotificationConfigs`,
  `GET /tasks/{id}/pushNotificationConfigs/{configId}`,
  `POST /tasks/{id}/pushNotificationConfigs`,
  `PUT /tasks/{id}/pushNotificationConfigs/{configId}`,
  `DELETE /tasks/{id}/pushNotificationConfigs`,
  `DELETE /tasks/{id}/pushNotificationConfigs/{configId}`.
- **Agent-to-agent delegation over A2A.** A caller invokes one of the project's
  agent definitions; the server does not itself fan out to other A2A agents.

## Registry metadata

The card carries the `agent_metadata/v1` extension so agent galleries can show an
author and a contact address:

```json
{
  "uri": "https://www.googleapis.com/gemini-enterprise/a2a/extensions/agent_metadata/v1",
  "params": {
    "authorName": "Emergent Company",
    "contactEmail": "support@emergent-company.ai"
  }
}
```

This is forward-looking: Gemini Enterprise is the reference implementation of the
extension, but it requires A2A v0.3 and cannot register this server yet (see
[Not supported](#not-supported)). The fields are advertised because any registry
that consumes the extension gets a correct author and contact for free, and
because adding it later means re-publishing the card.

The `iconUrl` field is absolute and points at the web UI's 512 px icon. The
advertised origin is the frontend origin — `WEB_UI_URL`, or `APP_URL` when that is
unset — because the gateway serves `/assets/*`, not the API host. If neither is
configured, it falls back to the interface origin so the value is never empty.
