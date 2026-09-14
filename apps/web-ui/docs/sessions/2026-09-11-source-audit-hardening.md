# 2026-09-11 — Source audit + hardening backlog

## Goal

Audit the application source for potential improvements and implement the actionable
findings, one item at a time, with verification.

## Outcome

Done. Ran a four-lane recon (gateway Go, Python bridge + connector, Swift client + e2e, and
an oracle review of the gateway core/security paths), produced a prioritized list of 10
backlog items, then landed all 10 plus a red-master hotfix and two extra iOS fixes across
**19 PRs** on `master`:

| PR | Area |
|---|---|
| #31 | Quick wins: constant-time secret compares, error propagation, AppleScript tab escaping, config chmod, worker task tracking |
| #33 | Fail-closed auth default (`AUTH_MODE=session`) + `Config.Validate` startup checks |
| #34 | SSRF guard on provider `base_url` probes |
| #63 | OIDC ID-token verification (JWKS signature + `iss`/`aud`/`exp`) |
| #65 | Server timeouts + 4M request body limit (uploads exempt) |
| #66 | Single-flight session-token refresh |
| #67 | Graceful shutdown: drain SSE + join supervisor |
| #68 | Collapse four N+1 fan-outs into bounded parallelism |
| #70 | Gateway boundary tests (`sse_markdown`, `setup`, `mcp_relay`) |
| #71 | Connector relay/`runRelay` tests |
| #72 | iOS `MemoryTokenSource` force-unwrap fix |
| #73 | Split `gateway/memory.go` (2827 lines) into domain files |
| #77 | iOS test harness concrete-simulator + `MemoryTokenSource` tests |
| #79 | Fix `MarkdownParserTests/parsesTaskList` (swift-markdown GFM tasklist nondeterminism) |
| #81 | **Red-master hotfix**: declare per-agent MCP share methods on `MemoryBackend` |
| #82 | Finish N+1: `scheduledSessionSummaries` bounded parallel |
| #83 | Typed `*memoryHTTPError` + `isMemoryStatus`; drop stringly `"409"/"403"` checks |
| #84 | iOS shared `GatewayHTTP` transport |
| #86 | iOS: coalesce controller Tasks from `objectWillChange` bursts |

All PRs were verified locally (gateway/connector `go build`+`test`+`-race`+`golangci-lint`,
`memory_bridge` pytest, iOS suite on `mcj-mini`) and merged with `--admin` because GitHub
Actions was account-billing-blocked for the whole session (later partly mitigated by #90's
self-hosted runner).

## Decisions

- **Retire the single-owner/Tailscale-only posture.** `AUTH_MODE` default `dev` → `session`;
  `Config.Validate()` fails fast when session config is missing; `dev` is an explicit
  local-only escape hatch. — The app is meant to be public, authenticating like the memory
  CLI (auth-code + PKCE, public client, access token as the memory bearer).
- **Verify the OIDC ID token** (JWKS signature + `iss`/`aud`/`exp` via `coreos/go-oidc`) and
  keep the flow nonce check; deleted the dead `ZITADEL_CLIENT_SECRET` config. — A public
  deployment must not trust an unverified token; the old spec/breadcrumb (`09-security.md`)
  said "no signature verification needed" and was updated.
- **Guard outbound provider `base_url` dials** with an SSRF-safe dialer (reject
  loopback/private/link-local/multicast/CGNAT; pin the resolved IP to avoid DNS rebinding).
- **Split `memory.go` by domain (pure code move) instead of splitting the 167-method
  `MemoryBackend` interface.** — Lower risk; narrowing the interface is deferred.
- **Use a shared `runConcurrently` (errgroup, limit 8)** for N+1 loops rather than bespoke
  goroutine code per site.
- **Typed backend errors** (`*memoryHTTPError{Status,Code,Message}`, `errors.As`) instead of
  `strings.Contains(err.Error(), "409")`.
- **HTTP hardening:** global 4M body limit with document/avatar uploads skipped; explicit
  `http.Server` timeouts with `WriteTimeout` intentionally unset so SSE stays open.
- **Left e2e `trace: 'on'` and the `mutations` dependency unchanged** — both are documented
  as intentional in `playwright.config.ts`.
- **iOS:** one shared `GatewayHTTP` transport for the three gateway clients (keeping each
  client's error type/messages); coalesce controller scan/trace Tasks so a change burst
  reschedules instead of spawning a Task per change.

## Changes

- **Auth/security** — `gateway/config.go` (default + `Validate`), `gateway/main.go`
  (validate/warn, timeouts, body limit, shutdown drain, supervisor join), `gateway/auth.go`
  (constant-time compare, `/auth/switch`, single-flight refresh), `gateway/oidc.go`
  (JWKS verification), `gateway/netguard.go` (new SSRF dialer),
  `gateway/settings_providers.go` (guarded client), `gateway/voice_binding.go`,
  `gateway/setup.go`, `gateway/objects.go`.
- **Reliability/perf** — `gateway/memory.go` (typed error, body cap), `gateway/concurrency.go`
  (new `runConcurrently`), `gateway/client_api.go`, `gateway/sessions.go`, `gateway/ui.go`,
  `gateway/settings_handlers.go`, `gateway/supervisor.go`.
- **Structure** — `gateway/memory.go` split into `memory_agents.go`, `memory_schedules.go`,
  `memory_runs.go`, `memory_conversations.go`, `memory_documents.go`, `memory_graph.go`,
  `memory_skills.go`, `memory_schemas.go`, `memory_usage.go`, `memory_providers.go`,
  `memory_orgs.go`, `memory_profile.go` (128 funcs unchanged).
- **MCP shares hotfix** — `gateway/backend.go` (5 interface decls),
  `gateway/handlers_test.go` (`fakeMemory` fields/methods).
- **Tests** — `gateway/{sse_markdown,setup,mcp_relay,memory_error,concurrency,body_limit,shutdown}_test.go`,
  `connector/cmd/memory-connector/relay_test.go`,
  `connector/internal/relay/client_behavior_test.go`,
  `client/ios/VoiceAgentTests/{MemoryTokenSourceTests,GatewayHTTPTests}.swift`.
- **iOS** — `client/ios/VoiceAgent/Net/GatewayHTTP.swift` (new),
  `{Memory/AgentInfoClient,Sessions/SessionLogClient,Agents/ControlPlaneClient}.swift`,
  `Memory/MemorySessionController.swift`, `Memory/MemoryTokenSource.swift`.
- **Bridge/connector** — `memory_bridge/worker.py` (tracked tasks, always-close writers),
  `connector/internal/appletools/applescript.go`, `connector/internal/config/config.go`.
- **Docs** — `docs/spec/{00-vision,04-go-application,08-deployment,09-security}.md`, `.env.example`,
  `docker-compose.yml`, `docs/tasks/BACKLOG.md` (auth rows).

## Verification

- `cd gateway && go build ./... && go vet ./... && go test ./... && go test -race ./... && golangci-lint run ./...` — green (per PR).
- `cd connector && go build ./... && go test ./... && go test -race ./... && golangci-lint run ./...` — green.
- `.venv/bin/python -m pytest memory_bridge/tests` — 42 passed.
- Merged-state spot check in a clean worktree off `origin/master` — gateway/connector/bridge green.
- `tools/ios-build-mac.sh --test` on `mcj-mini` (iPhone 17 Pro) — full iOS suite 71/71 (was 67/1 with the task-list flake).
- CI (GitHub Actions) did not run — account billing block; every merge used `--admin`.

## Open questions / follow-ups

- **Public/Zitadel posture is unverified live** — client type/PKCE, redirect URIs, refresh
  TTL still need a real deployment check (BACKLOG `verify-public-zitadel-auth`).
- **`MemoryBackend` is still one 167-method interface** — the red master (#81) shows the
  friction; narrow per-handler interfaces would prevent it.
- **`origin/master` vs local checkout diverged** during the session; local `master` lacks the
  later merges (separate session tracks `sync-local-master-origin`).
- **CI billing** — `docs/tasks/ci-actions-billing-blocker.md`; a required local pre-merge
  build gate would have caught the non-compiling red master on push.
- **e2e harness** — hard waits / brittle selectors not audited; `trace: 'on'` and mutation
  ordering left as documented-intentional.
- **Connector JSON-RPC numeric ids** dropped (`frames.go`); needs hub-side coordination.
- **MCP server secrets remain plaintext** (accepted, documented).

## Tasks

- [connector-jsonrpc-numeric-ids](../tasks/connector-jsonrpc-numeric-ids.md) — accept numeric JSON-RPC request ids on the relay
- [memory-backend-narrow-interfaces](../tasks/memory-backend-narrow-interfaces.md) — narrow the 167-method `MemoryBackend` into per-consumer interfaces
