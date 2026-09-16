## Context

The server exposes a project-admin, agent-owned MCP endpoint
(`openspec/specs/agent-mcp-keys`, `openspec/specs/agent-mcp-sessions`):

| Method | Path |
|---|---|
| `POST` | `/api/projects/:projectId/agents/:agentId/mcp-endpoint` |
| `GET` | `/api/projects/:projectId/agents/:agentId/mcp-endpoint` |
| `DELETE` | `/api/projects/:projectId/agent-mcp-endpoints/:id` |
| `POST` | `/api/projects/:projectId/agent-mcp-endpoints/:id/keys` |
| `GET` | `/api/projects/:projectId/agent-mcp-endpoints/:id/keys` |
| `DELETE` | `/api/projects/:projectId/agent-mcp-keys/:id` |
| `POST` | `/api/projects/:projectId/agent-mcp-keys/:id/rotate` |
| `GET` | `/api/projects/:projectId/agent-mcp-endpoints/:id/sessions` |

Every route requires a project admin (the server guards with
`RequireAPITokenScopes("admin")`). The client-facing URL for an agent is
`/api/mcp/agents/:agentId` and exposes five tools.

The CLI already has an `agents` group with subcommands `list`, `get`, `create`,
`update`, `delete`, `trigger`, `runs`, `get-run`, `questions`, `hooks`, plus the
`mcp-servers` subgroup. Agent commands resolve the project through
`resolveProjectContext` and the agent through `resolveAgentArgOrPick`.

## Goals / Non-Goals

**Goals:**

- Drive the whole agent MCP endpoint lifecycle from `memory agents mcp-endpoint`.
- Resolve an agent by id, name, or slug — no UUID pasting.
- Reveal a key secret exactly once, with the MCP URL and a copy-paste client
  config, on both `create` and `rotate`.
- Mirror the CLI's existing conventions for `--json`, `--project`, destructive
  confirmation, and error mapping.

**Non-Goals:**

- Any server-domain, route, migration, or API-contract change.
- Any web-UI change.
- Changing the five-tool surface or session semantics.
- Managing the MCP registry (`agents mcp-servers`) — unrelated to this endpoint.
- `--output csv` and other formats for these commands; `--json` is the
  machine-readable mode.

## Decisions

### Command tree: one `mcp-endpoint` subgroup under `agents`

```
memory agents mcp-endpoint show [agent] [--json]
memory agents mcp-endpoint create [agent]
memory agents mcp-endpoint revoke [agent] --yes
memory agents mcp-endpoint keys create [agent] --label <label> [--expires-at <RFC3339>]
memory agents mcp-endpoint keys list [agent] [--json]
memory agents mcp-endpoint keys revoke <key-id> --yes
memory agents mcp-endpoint keys rotate <key-id> --yes
memory agents mcp-endpoint sessions [agent] [--status <status>] [--json]
```

Rationale:

- **Under `agents`, not `mcp-servers`.** The endpoint is agent-owned; the
  existing `agents mcp-servers` subgroup manages the project's MCP *registry*
  (external servers the agent can call). Folding this in there would conflate two
  opposite directions of MCP traffic. A distinct `mcp-endpoint` subgroup keeps
  the registry untouched.
- **A `keys` subgroup.** Keys are many per endpoint and get their own lifecycle;
  `keys create|list|revoke|rotate` reads as clearly as the server routes.
- **`sessions` is a sibling, not a key subcommand.** Sessions are endpoint-wide
  and the API lists them by endpoint id; nesting under `keys` would misstate the
  ownership.
- **Key revoke/rotate take a key id, not an agent.** The routes are key-scoped
  and key ids are unique; forcing an agent argument would add a lookup and a
  failure mode for no benefit. `--project` is still resolved because the route
  is project-scoped.

Alternatives considered: flat `agents mcp-endpoint-*` commands (noisy group,
no help grouping); a top-level `memory mcp-endpoint` (breaks the agent-owned
model); reusing `agents mcp-servers` (conflates registry and endpoint).

### Agent resolution: id → runtime name → definition name/slug

`resolveAgentRef` resolves in order:

1. A UUID is passed through unchanged.
2. A case-insensitive exact match on a runtime agent's `name`.
3. A case-insensitive match on an agent definition's `name`, or on its ACP slug
   (derived from the name: lowercase, non-alphanumerics → hyphens, collapsed and
   trimmed, ≤63 chars) — the server accepts a definition id wherever it accepts
   a runtime agent id for endpoint creation.
4. Otherwise a not-found error that lists the available agent names; an
   ambiguous match is also an error.

The slug normalization is reimplemented in the CLI (a handful of regex
replacements) rather than imported from the server module, because `apps/cli`
depends only on the nested `.../pkg/sdk` module, not on the server module root.

When no agent argument is given and stdin is a terminal, the existing
`resolveAgentArgOrPick` picker is reused.

### Secret revealed exactly once, by a single tested renderer

`renderAgentMCPKeySecret(w, resp)` is the only place a raw token is printed. It
prints the label, key id, the MCP URL, an unmissable "shown once" warning, and
instructions to send the secret in the `X-API-Key` header. The secret is
deliberately printed **once** and not duplicated inside a config snippet — a test
asserts the token occurs exactly once in the rendered output. `create` and
`rotate` both call it; `list` and `show` decode a DTO that has no token field at
all, so a secret cannot leak from a read path.

### Destructive operations

`revoke`, `keys revoke`, and `keys rotate` are destructive (rotate invalidates
the old secret). Each prompts `[y/N]` on an interactive terminal and requires
`--yes` otherwise; the non-interactive path errors rather than proceeding
silently, so scripted use is explicit. This matches `team remove` (`--yes`) and
`tokens cleanup` (`--force`) conventions.

### JSON output

`show`, `keys list`, and `sessions` accept `--json` and emit the server DTOs
verbatim (no CLI-side reshaping), matching `agents list --json`. Create/rotate
always render the human secret block; they do not take `--json` because the
point of the command is the one-time reveal.

### SDK placement

The methods live in the existing `apps/server/pkg/sdk/mcp` package, not a new
sub-client, because that package already owns MCP credential minting
(`Client.Share`). Methods take `projectID` explicitly, matching `Share`.
`Client.SetContext` already carries the project for other MCP calls; explicit
project keeps these methods usable and testable without context mutation.

## Risks / Trade-offs

- [Risk] `mcp-endpoint` vs `mcp-servers` confusion remains → Mitigation: help
  text on both groups names the other and says which direction of MCP traffic it
  manages (registry = agent calls out; endpoint = clients call the agent).
- [Risk] Name→id resolution hides which agent was chosen → Mitigation: resolve
  commands echo the resolved agent id in their output; on ambiguity the error
  lists matches.
- [Risk] A server older than the endpoint API returns 404 → Mitigation: error
  mapping surfaces the server message unchanged, so the cause is visible.
- [Trade-off] Reimplementing slug normalization duplicates ~5 lines from
  `pkg/acpslug` → acceptable given the module boundary; a test pins parity for
  common inputs.
