# Design — add-mcp-connector

## Context

See proposal.md. Phase 1 (`add-external-mcp-management`, shipped) made connected
relay nodes visible/manageable in Memory's UI; the hub side (`mcprelay` in
emergent.memory, unlicensed — used as a service, never copied) is stable.
This change ships the missing client. Reference implementation to study, NOT to
copy verbatim: `emergent-company/diane` (MIT) `server/cmd/diane/mcp_relay.go`
and `server/mcp/tools/apple/`. Wire contract ground truth for implementation is
the live mcprelay source in `.slim/clonedeps/repos/emergent-company__emergent.memory/apps/server/domain/mcprelay/`
(handler.go/service.go) plus `docs/site/developer-guide/mcp-relay.md`.

## Goals / Non-Goals

Goals: a small, headless, single-process Go CLI that (1) reads a per-project
config, (2) holds one outbound relay WS with register/re-register + resilience,
(3) serves registered Apple Notes/Reminders tools via AppleScript, (4) exposes
`init`/`status` subcommands. Deterministic tests run against an in-memory fake
hub (httptest), no live Memory required.

Non-goals: Swift menu-bar shell (2b), brew/systemd/launchd packaging, Linux
tool set (notes/reminders are macOS-only in v1), server-pushed config sync
(Diane's graph `MCPProxyConfig`), multi-project daemon, tool sandboxing,
Playwright e2e (deferred to 2b when a real connector becomes the fixture).

## Decisions

### 1. New module `connector/`, binary `memory-connector`

Root-level directory beside `gateway/`, `memory_bridge/`, `client/`: one
self-contained Go module (`connector/go.mod`), a `cmd/memory-connector` main,
`internal/{config,relay,toolreg,appletools}`. Not under `gateway/` (separate
binary + dependency set, no coupling to the web UI module). Linux and macOS
share this module; Apple tool files carry `//go:build darwin`-style platform
gating via a small registry, not build tags in the tool registry itself
(registry is platform-neutral; providers register conditionally at runtime so
`status` can explain why Apple tools are absent on Linux).

Rationale: mirrors repo layout (`memory_bridge/` precedent), keeps the future
Linux port inside the same change stream, and avoids gateway module churn.
Alternative — new repo — rejected: not enough independent lifecycle yet.

### 2. Single process: relay client + tool registry in one binary

Diane splits relay parent + `diane mcp serve` stdio subprocess (needed there
for server-pushed graph config). V1 has no config push, so no subprocess:
one process owns the WS, a `toolreg.Registry` (name → handler) registers the
static tool list at startup, and a per-connection dispatcher routes `tools/call`
requests to handlers. Handlers return `(result map[string]any, err error)`;
the dispatcher serializes the MCP-shaped response.

Rationale: fewer moving parts, easier to test, no stdio IPC bugs. Revisit when
tool isolation/restart-by-tool becomes a need.

### 3. Wire fidelity: frames + correlation follow the hub, verified by a fake hub

Frames over WS: `register` (first), then the hub sends call requests and the
connector replies; keepalive via WS control pings plus app-level pong handling
mirrors the hub's expectations (server pings ~45s, pong wait 60s, idle reap 90s
in mcprelay). Register payload = `{type, instance_id, version, tools:{tools:[
{name, description, inputSchema}]}}` (nested `tools` shape — the exact shape
`GET /sessions/:id/tools` re-serves and the UI renders). Registration happens
after every (re)connect; a new registration replaces a stale session for the
same instance id (hub behavior — no deregister dance needed).

Request correlation: each hub call frame carries an id; the connector answers
with a response frame carrying the same id. Implementation ports the id/pending
semantics from the Diane client but validates them against the mcprelay
handler/service in the .slim clone at implementation time — the spec-level
contract is "correct request/response pairing", not exact field names.

**Verification anchor:** tests drive a fake hub (httptest + gorilla/websocket)
that implements the mcprelay handler semantics (register store, forward call,
timeout, drop), so protocol drift is caught in CI without emergent.memory.

### 4. Resilience

- Reconnect: capped exponential backoff, first retry ~30s growing to a 5 min
  cap (Diane's numbers; Traefik-safe), infinite retries while the process runs.
- Keepalive: WS control ping ~25s; read deadline tolerant of hub pings.
- Re-register immediately after every successful reconnect; no periodic
  re-register needed in v1 (static tool list per run) — revisit if tools become
  dynamic.
- Shutdown: SIGTERM/SIGINT → close WS gracefully, exit 0. Run as a foreground
  process (supervision/launchd belongs to 2b).

### 5. Config + onboarding

YAML at `~/.config/memory-connector.yml`, written `0600`:

```yaml
server_url: https://memory.emergent-company.ai
token: emt_<project-scoped>
project_id: <uuid>          # optional if token is project-scoped
instance_id: <stable-id>    # default: <hostname>-connector
```

`init` prompts/accepts flags, writes the file, then probes connectivity by
calling `GET /api/mcp-relay/sessions` with the token (project context from the
token) — 2xx = config usable, 401/403 = report auth failure, no file written
on failure. `status` reads the same endpoint list to report registered tool
count parity (best effort) plus local connection state. `--config` overrides
the default path (testability).

Rationale: Diane's multi-project YAML is more than v1 needs; single-profile
keeps `status`/`init` trivial. Token in a 0600 file, `wss://` recommended;
`http://`/`ws://` allowed for local dev with a warning.

### 6. Apple tools via AppleScript only (no brew deps)

Both target apps are AppleScript-scriptable, so `osascript` alone covers v1 —
unlike Diane's `remindctl` (brew) + embedded-Swift split. Handlers exec
`/usr/bin/osascript` with a bounded timeout (default ~15s), parse stdout as
JSON where the script emits it. First run triggers the macOS Automation
permission prompt for the host terminal/process — expected UX; `init`/`status`
output mentions "grant Automation permission for <host> in System Settings →
Privacy & Security" when a script errors with -1743 (not authorized).

v1 tool set (names/inputs decided at implementation, four expected):
- Notes: search notes (query, folder?, limit), create note (title, body?).
- Reminders: list reminders (list?, include_completed? → default false), add
  reminder (title, due_date? RFC3339, notes?).
- Timeouts and JSON output formatting tested via a script-runner seam
  (interface) so unit tests inject a fake runner instead of invoking osascript.

### 7. Testing

- Unit: frame codec, config load/validate, registry, handler arg validation,
  runner seam, resilience timers (shortened for tests).
- Integration: fake-hub tests (register→call→response round trip, unknown
  tool, handler error, reconnect + re-register, drop detection) using
  `httptest.NewServer` + gorilla/websocket dial. Deterministic, no sleeps where
  injectable.
- Platform: Apple provider registration unit tests run on all platforms
  (registry lists tools; runner seam stubbed); the real osascript path is
  exercised manually on macOS at the 2b gate.
- Go toolchain: match gateway's `go.mod` version. Dep: `gorilla/websocket`
  (same as the hub; battle-tested). `gopkg.in/yaml.v3` for config.

## Risks / Trade-offs

- [Protocol drift between Diane client and current mcprelay hub] → Conformance
  verified against the .slim mcprelay source at implementation time + fake-hub
  tests encode the contract.
- [AppleScript brittleness: locale, app not running, permission prompts] →
  Scripts kept minimal; runner seam + error mapping (-1743 guidance); tools
  return descriptive errors instead of failing registration.
- [Secret token on disk] → 0600 file, docs warning, `wss://` default guidance;
  Keychain handling deferred to the 2b shell.
- [Instance-id collisions across machines] → default `<hostname>-connector`;
  `init` warns when sessions already show the chosen id.
- [Long-lived idle sessions reaped by hub] → keepalive cadence chosen below the
  hub's 90s reap.

## Migration Plan

Greenfield module; nothing to migrate. Backend/gateway untouched. Rollback =
remove connector binary/config.

## Open Questions

- gorilla/websocket vs coder/websocket (both fine; gorilla preferred, matches
  hub) — no spec impact.
- Exact module path + four tool signatures — settled at implementation.
