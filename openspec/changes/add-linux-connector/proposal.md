## Why

The connector's account/project "brain" — sign-in, session refresh, secret storage, project selection, token minting, and config materialization — currently lives only in the macOS SwiftUI app, while the headless Go engine (`connector/`) is already cross-platform. A Linux user therefore gets only the bare `init`/`relay`/`status` CLI, and any new platform would have to re-implement the brain. The Memory platform already solves much of this: the Memory CLI (`apps/cli`) ships an OIDC device-authorization flow, token refresh/revocation, and a 0600 credential store, and it consumes the public Memory core Go SDK. Building a Linux CLI + daemon first and only then consolidating the shared brain onto that reused foundation avoids speculatively refactoring before a second consumer exists.

## What Changes

- **Sequence: Linux daemon first, shared brain second, macOS adoption last.**
  1. **Linux v1** — `memory-connector daemon` (foreground, supervisor-friendly) plus `install` / `uninstall` managing a **systemd user unit**, reusing the existing token-only `init`/`relay`. Ships a working Linux connector without touching the macOS app.
  2. **Shared brain (Go)** — OIDC sign-in/session/refresh, account/project management, project-scoped token mint/reuse/revoke, secret store, config materialization, `status --json`, and supervision policy.
  3. **macOS adoption** — port the SwiftUI app onto the shared core and delete the duplicated Swift brain; behavior unchanged.
- **Reuse the Memory core Go SDK's public components instead of reimplementing them**: the SDK (`github.com/emergent-company/emergent.memory/apps/server/pkg/sdk`) already ships a public `auth` package (OIDC discovery, OAuth device flow with polling, serialized refresh, 0600 credential store, and pluggable `apikey`/`apitoken`/`oauth` providers) plus `apitokens` (mint/list/get/revoke/scopes) and `projects` (list/current) clients. The connector reuses those. The Memory CLI's `apps/cli/internal/auth` is a near-duplicate of the SDK package; only one small upstream change is needed — the SDK device flow must request `offline_access` (or expose configurable scopes) so the connector receives a refresh token.
- **Build the connector brain in Go** from those reused pieces plus the behaviors the macOS app has today (PKCE for the GUI path, per-account sessions, project selection, token mint/reuse, config materialization, structured status, restart/supervision policy).
- **Add core subcommands** to `memory-connector`: `auth login` / `auth logout`, `projects list` / `projects use`, and `status --json`; keep the existing `init` / `relay` / `status` text surface working.
- **Introduce a platform abstraction** so the shared core is OS-agnostic: secret store (0600 files, XDG-aware paths), log sink, supervision activation (launchd/systemd), and the OIDC authorization seam (macOS custom scheme + PKCE; Linux device flow, loopback optional).
- **Adopt the shared core in the macOS app**: replace the Swift OIDC/account/project store, API client, config writer, status parser, and restart policy with calls into the Go CLI's JSON surface. SwiftUI views and the menu bar stay native; no user-visible behavior change.
- **Linux v1 registers no local platform tools** (matching the current roadmap note that Linux tool hooks arrive later); the daemon authenticates, connects, and serves an empty-but-valid tool set, and `status` explains the absence.
- **Add `status --json`** as the stable machine-readable contract consumed by the macOS GUI and Linux tooling.

## Capabilities

### New Capabilities
- `connector-core`: the OS-agnostic connector core and its CLI surface — OIDC sign-in/session/refresh, account and project management with token mint/reuse, secret storage, config materialization, structured status, and the supervision contract — shared by the macOS app and the Linux CLI/daemon, and built by reusing the Memory CLI's authentication components and the Memory core Go SDK.
- `linux-connector`: Linux-specific packaging and runtime behavior — the `daemon` subcommand, systemd user-unit install/uninstall, XDG paths, and the platform note for the absent local tool set.

### Modified Capabilities
<!-- None: the macOS app's behavior is unchanged (adopting the shared core is an implementation move), and no existing capability's requirements change. -->

## Impact

- **`connector/` Go module**: new packages for account/OIDC, projects, secret store, status model, and platform glue; new subcommands; XDG-aware config/log paths; platform-specific files; new dependency on the Memory core Go SDK.
- **Memory core repo (`emergent-company/emergent.memory`)**: depend on the published SDK (`.../apps/server/pkg/sdk`), including its public `auth`, `apitokens`, and `projects` packages. One small upstream SDK change is preferred: device-flow scopes must include `offline_access` (or be configurable) so the connector gets a refresh token. The CLI's `internal/auth` duplicate can later be deduped onto the SDK, but that is not a prerequisite.
- **`client/macos/`**: the Swift brain (`OIDCClient`, `OIDCSessionStore`, `AccountStore`, `ProjectStore`, `AuthStore`, `MemoryAPIClient`, `EngineConfigWriter`, `EngineManager` policy, `StatusSnapshot`) is replaced by calls to the Go CLI; views, menu-bar item, and Automation/TCC handling remain native.
- **Packaging / CI**: systemd user-unit template, Linux release artifacts (e.g. goreleaser), and build wiring.
- **No gateway/server changes**: uses existing `/api/auth/*`, `/api/projects*`, `/api/user/*`, and `/api/mcp-relay/*` endpoints.
- **Out of scope**: Linux local tool hooks (filesystem/shell/browser), any GUI for Linux, and changes to the MCP relay wire protocol.
