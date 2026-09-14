# 2026-09-10 — MCP servers UI + agent tool calls

> Work performed 2026-09-09 (all commits that day); this log written at handoff on 09-10.

## Goal

Deliver in-browser management of project MCP servers in the gateway (register/connect
external servers, sync/inspect tools, per-tool enable, delete), then prove the full value
loop with e2e tests: register an external MCP server → attach its tools to an agent → have
the agent actually call one in a live chat turn.

## Outcome

**Done** — feature shipped, plus two upstream Emergent Memory fixes it exposed.

1. **Gateway UI delivered** (`openspec/changes/archive/2026-09-09-add-mcp-servers-ui/`):
   Settings → MCP Servers page (`/settings/mcp-servers[/new|/:id/edit]`) with list/register/
   edit/delete across `stdio`/`sse`/`http`, per-server sync (`POST /:id/sync`) + inspect
   (`POST /:id/inspect`), per-tool enable toggle (`PATCH /:id/tools/:toolId`), cached-tool
   `<details>` groups, read-only builtins, delete confirm dialog. Agent tool-picker
   empty-state CTA now links to the page. Gateway gained the four missing client methods
   proxying memory's admin API.
2. **E2E**: `tests/e2e/specs/settings/mcp-servers-create-ui.spec.ts` (mutations) —
   register → row → sync (live Exa) → tool toggle → edit → delete + self-cleanup guard,
   green live. `tests/e2e/scenarios/mcp-servers-tool-call.spec.ts` (scenarios) — full loop
   incl. chat tool-call signal (mcp_tool SSE + tool chip), env-gated.
3. **Memory backend fixes** (PRs merged to `emergent-company/emergent.memory`):
   - **#406** `c8bf8656` — agent tool whitelists store **bare** tool names while pool keys
     were `ServerName_ToolName` → whitelist miss → only `set_session_title` reached the
     model. Added bare→prefixed alias resolution. **Deployed to api.dev (v0.75.0).**
   - **#410** `687c86da` — slugified external pool keys (`<slugified server>_<tool>`, valid
     LLM function names, ≤64 chars) + slug-aware call routing back to raw server names.
     **Not yet deployed** (needs next release).
4. **Dev deployment test MCPs** registered on the dev project: `exa` (keyless, 2 tools),
   `firecrawl` (keyless list/sync, 3 tools). `memory-self` self-loop dropped (unreachable
   from cloud dev).

**Not done / blocked:** end-to-end tool-call scenario still **skips** — api.dev runs
v0.75.0 (has #406 only); #410 lands in the next release. Re-run the scenario after that
deploy (see task `mcp-servers-tool-call-deploy-verify`).

## Decisions

- **Manage MCP servers as a standalone Settings-group page** (API Tokens precedent), not a
  hub-rail section or top-level nav — matches the registry/CRUD concern and reuses the
  `api_tokens` file-trio pattern.
- **Whitelist stores bare tool names** (what memory's tools API returns); resolution to the
  namespaced pool key is memory-side — UI stays simple and server-name changes don't churn
  agent definitions.
- **Slugify the server-name prefix in pool keys** (memory) so any user-entered server name
  yields a valid LLM function name, with slug-aware call routing to recover the raw server.
- **Test via public keyless remote MCPs** (Exa, Firecrawl) since cloud dev memory can only
  reach public internet — tailnet/self-hosted targets don't work there.
- **Admin-merge upstream PRs** (#406/#410) — the repo requires an approving review that no
  bot provides; #405/#391 sit `REVIEW_REQUIRED` post-merge, so admin squash is the
  established path.
- **Isolate memory-repo writer lanes in git worktrees** (`omos/<slug>` branches) — the main
  checkout had parallel WIP on another branch.

## Changes

Gateway (`/root/alfred`):
- `gateway/mcp_servers_client.go` (new) — sync/inspect/tools/toggle client methods;
  `gateway/backend.go` interface + fake; `gateway/mcp_servers_client_test.go`.
- `gateway/mcp_servers.go` / `gateway/mcp_servers.templ` / `gateway/mcp_servers_handlers.go`
  (new) — page data, PRG handlers, transport form, list/rows/JS actions, JSON routes.
- `gateway/main.go`, `gateway/ui.go` — routes + sidebar entry; `gateway/agent.templ` —
  empty-state CTA link.
- `gateway/mcp_servers_handlers_test.go`, `gateway/mcp_servers_ui_test.go` (new).
- `openspec/changes/add-mcp-servers-ui/**` → archived; `openspec/specs/mcp-servers-ui/spec.md`.
- `tests/e2e/specs/settings/mcp-servers-create-ui.spec.ts` (new);
  `tests/e2e/scenarios/mcp-servers-tool-call.spec.ts` (new); `tests/e2e/.env.e2e.example`
  (+`E2E_MCP_EXAMPLE_URL`/`HEADER_*`).

Emergent Memory (`/root/emergent.memory`, worktree branches, both merged):
- `apps/server/domain/agents/toolpool.go` — bare-name alias + `externalToolKey` slugify.
- `apps/server/domain/mcpregistry/names.go` (new, canonical slug) + `proxy.go`
  (slug-aware `CallTool`), `proxy_test.go`, `toolpool_test.go`, `pkg/adk/openai_model_test.go`.

## Verification

- `templ generate` + `cd gateway && go build ./...` + `go test ./...` + `golangci-lint run
  ./...` (0 issues) + gofmt — all green (gateway).
- `npx playwright test specs/settings/mcp-servers-create-ui.spec.ts --project=mutations`
  — **3 passed (18.5s)** live (setup + flow + cleanup guard), Exa sync branch exercised.
- `npx playwright test scenarios/mcp-servers-tool-call.spec.ts --project=scenarios` —
  setup passes, scenario **skipped** (backend/build limitation, not a spec failure).
- Memory PRs: `go build` + targeted `go test ./domain/agents/... ./domain/mcpregistry/...
  ./pkg/adk/...` green; upstream CI (Lint/Build/Unit Tests) green on both PRs.
- Deploy probe: api.dev `/health` → `version: v0.75.0` (contains #406); fix #410 pending
  next release.

## Open questions / follow-ups

- Tool-call scenario flips green only after the next memory release (containing #410) is
  deployed to api.dev; needs a re-run + `kb.agent_runs.tools` cross-check.
- `ParsePrefixedToolName` is still naive for its remaining caller (manual-sync validation,
  `service.go:544`) and the >64-char truncated-key edge in slug resolution is accepted v1.
- Firecrawl full toolset/tool-calls need a free API key added as a server header.
- Dev provider key in `E2E Main` still rejected by the live provider save (setup logs
  "provider config skipped") — unrelated to this feature, but makes scenario setup noisy.

## Tasks

- [mcp-servers-tool-call-deploy-verify](../tasks/mcp-servers-tool-call-deploy-verify.md) — re-run the scenario after the #410 release deploys.
- [dev-firecrawl-mcp-key](../tasks/dev-firecrawl-mcp-key.md) — add a free key for full Firecrawl tools/calls.
- [memory-self-local-test-mcp](../tasks/memory-self-local-test-mcp.md) — self-loop MCP only viable in local-docker.
- [memory-tool-prefix-hardening](../tasks/memory-tool-prefix-hardening.md) — remaining prefix-parse/truncation edges.
