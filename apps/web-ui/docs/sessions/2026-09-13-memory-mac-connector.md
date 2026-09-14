# 2026-09-13 — Memory macOS connector + relay node registry

## Goal
Deliver the Memory macOS connector (`Memory.app`) end-to-end: the Go relay engine
embedded in a SwiftUI menu-bar/Dock app, Zitadel sign-in, per-project tool
configuration + explicit connection, multi-account prod/dev support, dashboard
and agent details; plus gateway support for the UI to show external relay nodes.
Also verify install/run on the `tool` Mac host.

## Outcome
Done (code on master, green; two app installs verified). Remaining are user-gated
checks and one publish blocker:
- **Shipped on master**: connector Go engine (relay, Apple tools, disabled-tools
  filter, derived reminders fix), macOS app (single-account → OIDC sign-in,
  project switcher, per-project profiles, explicit Connect toggle, multi-account
  + Prod/Dev environments, dashboard, agent list/detail with exact tools,
  account switcher, file-backed secrets, "Memory" rename), gateway relay-node
  registry (offline + last-seen + manual remove).
- **Verified live**: hub sessions (app-managed `mcj-mini-connector`), dev project
  `f131d865…`; agents/definitions and projects/orgs APIs; app install/run on the
  `tool` Mac (manual engine removed afterwards).
- **Blocked**: the hosted web UI does not yet show the new offline badges — the
  Build & Publish workflow can't start (GitHub Actions billing/spending limit),
  and deployment is owned by `emergent.memory.infra`.
- **Active change**: `add-relay-node-registry` awaits manual verification + archive.

## Decisions
- **Go core + Swift menu-bar shell** — reuse the proven relay engine; native app
  owns UX/permissions (settled earlier in the session).
- **Engine as a direct `Process` child** — keeps macOS Automation (TCC) grants and
  Keychain identity attached to one stable app (D24-adjacent).
- **File-backed secrets (0600) instead of Keychain** — ad-hoc rebuilds reset
  Keychain access and prompted per item; files under
  `~/.config/memory-connector/accounts/<id>/` persist and need no prompts.
- **Zitadel Native-app PKCE sign-in** (no client secret); prod + dev environments
  are built-in constants (issuers/client ids recorded in `INFRASTRUCTURE.md`).
- **Multi-account with strict isolation** — per-account sessions, profiles,
  project tokens and connected project; switching disconnects the engine.
- **Explicit per-project Connect toggle** (not implicit on selection); tools
  default OFF, per project; a master "All local tools" switch toggles all.
- **Engine lifecycle gated by connection** — `EngineLifecyclePolicy`: runs only
  when a project is connected and its config is valid (no stale config at launch).
- **Agents list = definitions** (not chat-session instances); tool list fetched
  from the full definition; `tools` null/empty means **deny-all** (there is no
  "default toolset") — copy corrected accordingly.
- **Relay-node registry lives in the gateway** (project settings KV) — the Memory
  backend keeps sessions in memory; gateway registry adds offline + last-seen +
  manual removal without backend changes.
- **Lazy reconciliation on page load** (not a background poller) — presence
  accuracy is visit-bounded; TTL ticker deferred (task).
- **Keep bundle id `com.emergent.memory.connector`** while renaming the product to
  `Memory` — preserves TCC/identity; secrets are file-based.

## Changes
- `connector/` (Go engine): config `disabled_tools`, registry filtering/dispatch,
  derived reminders-list scaling (`fix(connector): read batched reminder rows…`),
  README operator docs (Zitadel native app setup).
- `client/macos/MemoryConnector/`: app ~40 Swift files — `EngineManager` +
  `EngineLifecyclePolicy`/`EngineConfigSync`, `AppSecretStore`/`OIDCSessionStore`/
  `ProjectTokenStore`/`ProjectProfileStore`, `OIDCClient`/`PKCE`/`RefreshCoordinator`,
  `AccountStore`+`Account`+`Environment`, `ProjectStore` scopes, `AuthStore`,
  `DashboardStore`/`DashboardPage`, `AgentDetailStore`/`AgentDetailView`,
  `AccountToolbarView`/`AccountRowLabel`/`ProjectSwitcherView`, `StatusItemController`
  (NSStatusItem left-popover/right-menu), XcodeGen project + `tools/mac-build.sh`
  (product `Memory.app`), `tools/mac-setup-signing.sh`/`mac-sign.sh`,
  `tools/make-appicon.sh`, `WebAuthenticator` crash fix.
- `gateway/mcp_nodes_registry.go` (new) + `mcp_nodes.go`/`mcp_nodes.templ`/
  `main.go` — per-project relay node registry, live/offline rows + last-seen,
  snapshot tools, confirmed Remove (`POST /settings/mcp-nodes/remove`, PRG).
- `openspec/`: archived `add-mcp-connector-app`, `add-mac-connector-auth`,
  `add-multi-account-environments` (+ synced main specs); active
  `add-relay-node-registry`.
- `tools/mac-build.sh`, `tools/mac-sign.sh` — product path/install fixes.

## Verification
- `tools/mac-build.sh --test` (xcodebuild on mcj-mini) — **271 tests, 0 failures**
  after the final agent-detail change; earlier runs green at each milestone.
- Gateway: `go build ./...`, `/root/go/bin/templ generate`,
  `/root/go/bin/golangci-lint run ./...` (0 issues), `go test ./... -count=1` — green.
- Live API: `GET /api/projects` (137 projects), `/api/orgs`, `/agent-definitions`;
  hub `/api/mcp-relay/sessions` — dev project showed `mcj-mini-connector` (4 tools)
  and temporarily `tool-connector` (manual engine, since stopped).
- Installs: `~/Applications/Memory.app` running on `mcj-mini-2-1` and on `tool`
  (`CFBundleName/Executable = Memory`); app on `tool` not signed in.
- `openspec validate --specs` — 32/32.

## Open questions / follow-ups
- Deployed UI badges blocked by GitHub Actions billing (`ci-actions-billing-blocker`);
  after the fix, publish the image and have infra deploy, then verify.
- `tool` Mac: install done, but the app is not signed in there (needs GUI sign-in;
  sign-in requires the dev/prod native app to accept the callback scheme).
- Presence accuracy: lazy last-seen on page load vs a TTL/background ticker.
- Automated e2e coverage for the connector (relay register/tools, account isolation).

## Tasks
- [mac-connector-presence-ttl](../tasks/mac-connector-presence-ttl.md) — background ticker for accurate offline/last-seen.
- [verify-relay-node-badges-deploy](../tasks/verify-relay-node-badges-deploy.md) — publish + deploy, then verify badges live.
- [mac-connector-e2e-coverage](../tasks/mac-connector-e2e-coverage.md) — e2e/integration tests for the connector.
- [archive-relay-node-registry](../tasks/archive-relay-node-registry.md) — sync specs + archive the active change.
- [verify-dev-zitadel-native-callback](../tasks/verify-dev-zitadel-native-callback.md) — confirm the dev native app allows the callback scheme.
- [mac-app-smoke-checklist](../tasks/mac-app-smoke-checklist.md) — remaining manual GUI checks.
