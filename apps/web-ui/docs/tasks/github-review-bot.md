# Memory GitHub review bot — e2e wiring (schedule-first)

**Status:** working e2e (dev / api.dev)
**Created:** 2026-09-11

## What

A scheduled Memory agent reviews GitHub pull requests and posts real reviews as the
`emergent-code-reviewer` GitHub App identity. Built without Flow, reusing Flow's concepts
(webhook verify, App installation tokens, marker comments) and Memory's existing primitives.

## Working setup (api.dev, project `bf10f0e4-3b2c-41a1-99bf-ee9c8f6b46c8`)

| Piece | Value |
|---|---|
| Test repo | `emergent-company/pr-review-sandbox` (private) |
| GitHub App | `emergent-code-reviewer` id `4884315`, installation `160306576`, org-wide (`all` repos) |
| App perms | `pull_requests:write`, `contents:write`, `statuses:write`, `issues:write`, `metadata:read` |
| MCP server | `github` (http) → `https://api.githubcopilot.com/mcp/`, id `350d3488-77fa-41b4-93cf-618305479db2`, 41 tools |
| Agent definition | `pr-reviewer`, id `0b255ac8-0f1e-4a72-b933-14210941318d`, model `openai/deepseek-v4-flash`, no sandbox |
| Runtime schedule | `pr-review-sweep`, id `fea99c57-e323-4487-adb8-8e3468f618c7`, cron `0 * * * *` |
| LLM provider | project provider `openai` → baseUrl `http://litellm:4000/v1`, generative `deepseek-v4-flash` |

Key design choices:
- **MCP-only agent, no sandbox.** Tool resolution is independent of workspace provisioning
  (`executor.go:1330` early-returns when `SandboxConfig` is empty). The reviewer needs no checkout —
  it reads diffs via GitHub MCP tools.
- **Remote MCP over stdio.** stdio MCP runs inside the memory-server container
  (`mcpregistry/proxy.go`, mcp-go `exec.CommandContext`), so a host binary/docker is not usable;
  the remote server accepts a `ghs_` installation token as a bearer.
- **Agent tool whitelist is explicit** (`github_pull_request_read`, `github_pull_request_review_write`,
  `github_list_pull_requests`, `github_search_pull_requests`, `github_add_comment_to_pending_review`,
  `github_get_file_contents`, `github_run_secret_scanning`). Empty `tools` = deny all.

## Repro

```bash
# 1. mint App installation token (1h TTL)
TOKEN="$(tools/gh-app-token.sh)"

# 2. register MCP server (memory admin API; project token has admin scope)
curl -X POST "$MEMORY_URL/api/admin/mcp-servers" \
  -H "Authorization: Bearer $MEMORY_TOKEN" -H "X-Project-ID: $PROJECT_ID" \
  -H 'Content-Type: application/json' \
  -d "{\"name\":\"github\",\"type\":\"http\",\"url\":\"https://api.githubcopilot.com/mcp/\",
       \"headers\":{\"Authorization\":\"Bearer $TOKEN\"},\"enabled\":true}"

# 3. sync tools (create does NOT discover)
curl -X POST "$MEMORY_URL/api/admin/mcp-servers/<serverId>/sync" \
  -H "Authorization: Bearer $MEMORY_TOKEN" -H "X-Project-ID: $PROJECT_ID"

# 4. configure an LLM provider (see known issue #2: use provider "openai", not "deepseek")
curl -X PUT "$MEMORY_URL/api/v1/projects/$PROJECT_ID/providers/openai" \
  -H "Authorization: Bearer $MEMORY_TOKEN" -H "X-Project-ID: $PROJECT_ID" \
  -H 'Content-Type: application/json' \
  -d '{"apiKey":"<LLM_API_KEY>","baseUrl":"http://litellm:4000/v1","generativeModel":"deepseek-v4-flash"}'

# 5. create agent definition (no workspaceConfig => no sandbox), then runtime schedule
#    POST /api/projects/$PROJECT_ID/agent-definitions
#    POST /api/projects/$PROJECT_ID/agents   (triggerType:"schedule", cronSchedule, agentDefinitionId)
```

## E2E evidence (2026-09-11)

Run `5eb5a64e-cfb8-42c5-8eae-88de34b71eb4` completed (6 steps):

| PR | Fixture | Bot verdict |
|---|---|---|
| #1 `add-truncate` | buggy helper (ellipsis appended unconditionally) | `CHANGES_REQUESTED` — cites `app/strings.py:15` |
| #2 `add-reverse` | clean helper + unit test | `APPROVED` |

Both reviews authored by `emergent-code-reviewer` — a distinct identity, so the review is a real gate.

## Known issues / promotion path

1. **Token TTL — RESOLVED via native App auth (see update below).** Original remote-http MCP pinned a
   1h `ghs_` token in a static header, which broke after expiry. Now the MCP server is stdio with native
   GitHub App auth and refreshes tokens itself; no timer.
2. **Memory bug — custom base URL ignored for provider `deepseek`.** Both `buildTempResolvedCred`
   (`service.go:656`) and `decryptProjectConfig` (`service.go:167`) hardcode `https://api.deepseek.com/v1`,
   so a LiteLLM-backed deepseek config cannot be validated or used. Use provider `openai` (honors
   `baseUrl`), or fix deepseek to honor `BaseURL`. Worth an upstream fix in `emergent.memory`.
3. **Webhook trigger — BUILT (PR #98).** Gateway `POST /webhooks/github` verifies `X-Hub-Signature-256`
   (HMAC-SHA256) and triggers the Memory agent via `MemoryClient.TriggerAgent` with `{prompt, context}`.
   Routes are exempted in `publicAuthPath`.    Verified end-to-end (2026-09-11) by signed local deliveries:
   valid `pull_request.opened` → 202 → run `f5e65288` (7 steps) → PR #4 `CHANGES_REQUESTED` at
   `app/strings.py:16`; bad/missing signature → 401; non-allowlisted repo, `push`, and filtered actions → 204.
   Note: the gateway's `canonicalHostRedirect` 302s any request whose `Host` != `PUBLIC_BASE_URL` host, so a
   real delivery must arrive on the canonical host (tests spoofed it).
4. **Gateway schedule path** can create a schedule with a prompt, but `TriggerScheduledAgent` sends an
   empty body (`gateway/memory_schedules.go:127`) — no per-run prompt/context.
5. **No merge action.** The reviewer only reviews; merge remains a separate policy step.

## Update — native App auth, no timer (2026-09-11)

Replaced the remote-http MCP server (static `ghs_` header) with **stdio `github-mcp-server` using native
GitHub App auth**, so the server mints and refreshes installation tokens itself — matching how Flow does it
(`ghinstallation` transport, token minted on demand, never stored; see `flow/internal/server/git.go:103`,
`flow/internal/github/client.go:56`).

Host setup (dev, `emergent-dev`):

```bash
# 1. stage the static binary + App key
gh release download v1.12.1 --repo github/github-mcp-server \
  --pattern 'github-mcp-server_Linux_x86_64.tar.gz'   # extract -> github-mcp-server
scp github-mcp-server emergent-dev:/opt/emergent-dev/secrets/github-mcp-server
scp "/root/Emergent Code Reviewer Private Key Sept 9 2026.pem" \
  emergent-dev:/opt/emergent-dev/secrets/github-app.pem

# 2. mount both into memory-server (docker-compose.yml) and recreate
#    - .../secrets/github-mcp-server:/usr/local/bin/github-mcp-server:ro
#    - .../secrets/github-app.pem:/secrets/github-app.pem:ro
docker compose up -d memory-server
```

Memory MCP server row is now `type:"stdio"`:

```
command: /usr/local/bin/github-mcp-server
args:    stdio --app-id 4884315 --app-installation-id 160306576 \
         --app-private-key-path /secrets/github-app.pem --toolsets all
```

89 tools synced; e2e re-verified with a fresh PR #3 → `CHANGES_REQUESTED` at `app/strings.py:17/19`, no
static token involved.

**Production hardening:** bake the binary into the memory-server image
(`deploy/self-hosted/Dockerfile.server`) and mount only the PEM, instead of bind-mounting the binary from
the host. The bind mount is a dev convenience.
