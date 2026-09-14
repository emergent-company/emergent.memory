## Context

See `proposal.md — Why`. Current state that shapes the approach:

- `connector/` is a Go module (`internal/config`, `internal/relay`, `internal/toolreg`, `internal/appletools`) exposing `init` / `relay` / `status`. It is already OS-agnostic except for `internal/appletools`, which is Darwin-only via **runtime** detection (`osascript` availability), not build tags.
- `client/macos/` is a SwiftUI menu-bar app. Its non-view logic (`OIDCClient`, `OIDCSessionStore`, `AccountStore`, `ProjectStore`, `AuthStore`, `MemoryAPIClient`, `EngineConfigWriter`, `EngineManager`, `StatusSnapshot`) is the connector "brain" and exists only in Swift. 32 of 52 Swift source files do not import SwiftUI.
- The Swift↔Go boundary is process + files today: the app spawns `memory-connector relay --config <path>`, writes the engine YAML itself, and parses `memory-connector status` **text** output with a hand-written line parser.
- The app already abandoned the Keychain for secrets: they live in 0600 files under `~/.config/memory-connector/` (`AppSecretStore`), which makes the secret layer effectively portable.
- No Linux/systemd precedent exists; the only prior daemonization is the legacy Python wake-word client (launchd plists) being retired.

## Goals / Non-Goals

**Goals:**

- One Go implementation of the connector brain (auth, account/project, config, status, supervision) used by both front-ends.
- A Linux CLI + daemon that achieves parity with the macOS app minus the GUI and minus Apple tools.
- A stable, versioned CLI/JSON contract as the only Swift↔Go coupling.
- Preserve macOS behavior exactly — the Swift adoption is refactor-only.

**Non-Goals:**

- Linux local tool hooks (filesystem/shell/browser); v1 registers no local tools.
- Any Linux GUI or TUI.
- Moving the menu-bar UI, TCC/Automation handling, or SwiftUI views into Go.
- Changing the MCP relay wire protocol or any gateway/server API.
- Linking Go into Swift as a library.

## Decisions

### D1 — Share via a Go binary + JSON CLI, not an in-process library

The shared core is the `memory-connector` binary; front-ends consume it over subcommands with `--json` output. The macOS app keeps spawning the binary and adds `auth`, `projects`, and `status --json` calls.

- **Why:** Swift cannot import Go without a cgo `-buildmode=c-archive` bridge, which is fragile (build complexity, concurrency boundary, no clean types). The process boundary already exists and is the least disruptive.
- **Alternatives considered:** (a) cgo static archive linked into the app — rejected for complexity/fragility; (b) duplicate the brain in Go and Swift — rejected as it defeats the goal.

### D2 — Keep `status --json` as the versioned machine contract

Add `status --json` with an explicit `{instance_id, version, project, tools[], hub_state, ...}` document and an enumerated `hub_state` (connected / not_connected / auth_failed / unreachable / missing_config). Retire the Swift line parser in favor of this. Keep the human-readable text output for terminals.

- **Why:** the current text parser is brittle; two front-ends will depend on the shape; JSON gives a place to version.
- **Alternative considered:** a local HTTP/IPC status endpoint — unnecessary surface area for a single-user CLI.

### D3 — Platform abstraction as small interfaces + build-tagged files

Introduce narrow seams in the core and provide per-OS implementations:

| Concern | macOS | Linux |
|---|---|---|
| Secret/config store | 0600 files (already) | 0600 files, XDG dirs |
| Config/state/log paths | `~/.config`, `~/Library/Logs` | XDG config/state; journald |
| Supervision activation | launchd or in-process child supervision | systemd user unit |
| OIDC authorization | system browser + custom scheme (PKCE) | device authorization flow reused from the SDK (loopback optional) |
| Local tools | Notes/Reminders (osascript) | none in v1 |

`internal/appletools` keeps runtime detection; new platform files use build tags (`_darwin.go`, `_linux.go`) where the behavior genuinely differs.

- **Why:** keeps the shared core free of `runtime.GOOS` branching at call sites and makes each platform testable in isolation.

### D4 — Daemon lifecycle: foreground daemon + systemd owns restart on Linux

`memory-connector daemon` runs in the foreground and exits non-zero on unrecoverable faults; `install` writes a systemd **user** unit (`Restart=on-failure`, journal logging) and enables it; `uninstall` reverses it. The in-process supervision policy (D5) remains for macOS.

- **Why:** on macOS the app must supervise the child directly to keep TCC/Automation grants attributed to the app; on Linux there is no such constraint and systemd is the correct supervisor. Duplicating the circuit breaker under systemd would fight it.
- **Alternative considered:** self-supervising daemon on Linux — rejected (two supervisors, worse logs, no `systemctl status`).

### D5 — Retain the supervision policy as a shared, side-effect-free component

Keep the bounded-restart-with-window / give-up / user-stop-suppressed policy as a pure Go component with unit tests, consumed by the macOS child supervisor; the Linux daemon does not use it for restart (systemd does).

- **Why:** it is already a tested behavior (spec) worth preserving and is front-end-agnostic; making it pure keeps it deterministic to test.

### D6 — Secret store stays file-based

Continue 0600/0700 files, identity-independent, atomic writes; do not adopt libsecret or OS keychains.

- **Why:** the macOS app already proved this removes keychain prompts and survives rebuilds; identical semantics on Linux; one implementation.
- **Trade-off:** secrets are plaintext-on-disk at user-only permissions. Acceptable for a project-scoped `emt_*` token; document it.

### D7 — Module layout inside the existing `connector/` module

```
connector/
  cmd/memory-connector        init · relay · status · auth · projects · daemon · install · uninstall
  internal/engine             relay client + tool registry + dispatch (shared)
  internal/config             shared YAML/config model, XDG-aware path resolution
  internal/account            OIDC (device flow reused + PKCE GUI path) + session/refresh + account store
  internal/memoryapi          thin wrapper over the Memory core Go SDK (projects, tokens)
  internal/secretstore        file-backed store (shared)
  internal/status             structured status model → text and JSON
  internal/supervise          bounded restart policy (pure)
  internal/platform/<os>      paths · log sink · OIDC browser/callback · service install
  internal/tools/<os>         platform tool providers (darwin: appletools; linux: none)
```

- **Why:** keeps the existing module and its import path; the relay/toolreg code is unaffected; new packages map 1:1 to the new capabilities; `internal/account` and `internal/memoryapi` are thin layers over reused upstream components rather than new implementations.
- **Alternative considered:** a separate shared library module — rejected; no second consumer outside this repo.

### D8 — Staged by consumer: Linux daemon first, shared brain second, macOS adoption last

Sequence within this change:

1. **Linux v1** — daemon + systemd `install`/`uninstall` + XDG paths + reconnect/instance guard, using the existing token-only `init`/`relay`. Ships a working Linux connector with **no** auth-brain work and **no** macOS changes.
2. **Shared brain (Go)** — the account/OIDC/project/secret/config/status core, reused/ported from the Memory platform (D11), plus `status --json` and the supervision policy. This is where a second implementation of the brain briefly coexists with Swift.
3. **macOS adoption** — port the SwiftUI app onto the shared JSON/CLI surface and delete the duplicated Swift brain; behavior-preserving and independently revertible.

- **Why:** avoids a speculative "extract core first" refactor before a second consumer exists; lets the Linux consumer shape the core API; front-loads a shippable Linux connector.
- **Alternative considered (my initial plan):** extract the core first, then Linux, then macOS — rejected as speculative design with no consumer to validate the API.
- **Cost:** the Swift brain and the Go brain coexist during phases 2–3, so behavior changes must be kept out of the Swift brain in that window (bugfix-only) to avoid drift.

### D8a — Linux v1 is token-only and deliberately tool-free

Phase 1 ships `daemon` + systemd + XDG with the existing token-only configuration and **no** local tools. Interactive sign-in, project switching, and token minting arrive in phase 2.

- **Why:** a Project-scoped `emt_*` token already works end-to-end with `init`/`relay`; this makes the first Linux release small, useful, and independent of the auth work.
- **Trade-off:** Linux v1 users create a project token in the Memory web UI and paste it. Acceptable for the first release.

### D9 — Headless sign-in: reuse the device flow as the default (loopback optional)

On Linux the default — and only required — sign-in path is the **reused device authorization flow** (no redirect URI), which behaves identically on headless servers and desktops. A loopback redirect may be added later for desktop convenience. Token-only configuration is always available as the non-interactive fallback, overridable with an explicit flow flag.

- **Why:** the device flow already exists in the SDK and needs no redirect registration or new Zitadel app; adding loopback first would mean implementing a second flow for marginal UX gain.
- **Alternative considered:** loopback-first — rejected for v1 (extra code and provider setup); can be revisited if desktop UX warrants it.

### D10 — systemd user scope only in v1

`install` manages a systemd **user** unit only. A system-wide (root) unit is out of scope for this change.

- **Why:** the connector relays a per-user configuration and secrets; user scope needs no root and matches how the macOS app runs per-user. A system unit would need its own user/secret story for little benefit.

### D11 — Reuse the Memory core Go SDK's public packages; upstream only a scope fix

Verification of the SDK (`apps/server/pkg/sdk`, inspected in the local clone) shows it already exposes — publicly and importably — what the brain needs:

| Need | SDK package | Key API |
|---|---|---|
| OIDC discovery | `sdk/auth` | `DiscoverOIDC` |
| Device flow + polling | `sdk/auth` | `OAuthProvider.InitiateDeviceFlow` / `PollForToken` |
| Rotating refresh (serialized) | `sdk/auth` | `OAuthProvider.Refresh` (mutex-guarded, persists creds) |
| Credential store (0600) | `sdk/auth` | `LoadCredentials` / `SaveCredentials` |
| Auth seam | `sdk/auth` | `Provider` interface; `APIKeyProvider`, `APITokenProvider` |
| Project token lifecycle | `sdk/apitokens` | `Create` / `List` / `Get` / `Revoke` / `UpdateScopes` |
| Project list / current | `sdk/projects` | `List` / `GetCurrent` |

The Memory CLI's `tools/cli/internal/auth` is a **near-duplicate** of `sdk/auth` (same `Credentials` JSON shape, same 5-minute refresh buffer, same 0600 writes). So **no extraction is needed** — the connector imports the SDK packages directly.

Gaps found, and their treatment:

- **Device flow omits `offline_access`.** `InitiateDeviceFlow` hardcodes scope `openid profile email`, so no refresh token is returned — fatal for a long-running daemon. **Preferred:** a one-line upstream change adding `offline_access` (or making scopes configurable). **Fallback:** the connector requests the device code itself with the extra scope. This is the only upstream ask.
- **`sdk.NewWithDeviceFlow` is not embeddable.** It prints instructions to stdout, blocks, and calls `DiscoverOIDC(cfg.ServerURL)` (treats the server URL as the issuer). The connector instead composes the public pieces: `auth.NewOAuthProvider` + `InitiateDeviceFlow`/`PollForToken`, then `sdk.New` with an `apitoken` provider carrying the access token; the issuer is resolved via `/api/auth/issuer` first.
- **No PKCE browser flow in the SDK.** macOS keeps its PKCE custom-scheme flow; connector-core carries the PKCE step (ported from the Swift client) behind the same `Provider`-style seam. Device flow is the Linux path.
- **Reuse the OAuth client id** the Memory CLI already registers, so Linux needs no new Zitadel application and no redirect URI.

- **Why:** removes an entire class of new, security-sensitive code and its tests; the SDK already tracks server API/request shapes.
- **Alternative considered:** fork `tools/cli/internal/auth` — rejected; it duplicates a public SDK package and would need ongoing sync.
- **Dependency risk:** the SDK device-flow scope gap must be closed upstream or worked around locally (fallback above); either way the connector stays unblocked.

#### Proposed upstream SDK change

See [`reference/upstream-sdk-patch.md`](reference/upstream-sdk-patch.md) for the ready-to-apply patch, unit tests, PR text, and the fallback. In short: make `auth.OAuthProvider` device-flow scopes explicit, defaulting to `openid profile email offline_access`, with an optional `Scopes` pass-through on `sdk.AuthConfig` — additive and backward compatible. Target repo `emergent-company/emergent.memory` (local checkout `/root/emergent.memory`).

### D12 — Optional interop with the Memory CLI credential store

The SDK's `Credentials` JSON format is identical to the CLI's `~/.memory/credentials.json`, so importing a CLI session is just pointing `LoadCredentials` at that path when the connector has no session of its own. The connector SHALL NOT write into the CLI's store.

- **Why:** a single `memory login` can serve both tools, while keeping the stores decoupled (the CLI session is account-scoped; the connector also holds project-scoped tokens).
- **Alternative considered:** share one store outright — rejected; couples two tools' lifecycles and mixes credential scopes.

## Risks / Trade-offs

- **[macOS regression during the phase-3 port]** → Keep the CLI text surface and config file format backward compatible throughout; the Swift port stays behind the existing tests (`StatusSnapshotParserTests`, `OIDCClientTests`, `EngineConfigWriterTests`, etc.) which must remain green; land the port incrementally and revert to the in-tree Swift brain if needed.
- **[Two live brain implementations drift during phases 2–3]** → Freeze the Swift brain to bugfix-only once phase 2 starts; keep the overlap window short.
- **[SDK device flow lacks `offline_access`]** → One-line upstream scope fix (preferred), or the connector requests the device code itself with the extra scope (fallback); either keeps the daemon's refresh working.
- **[Headless OIDC on Linux is genuinely harder than macOS]** → Reuse the SDK device flow (no redirect URI) as the primary path; token-only always works; do not block v1 on a provider capability.
- **[Two supervisors could double-restart on Linux]** → Linux daemon does not self-restart; `install` configures `Restart=on-failure`; the daemon exits non-zero only for real faults.
- **[Token plaintext on disk]** → 0600/0700, project-scoped least-privilege tokens, documented; no wider secret exposure than today's macOS app.
- **[Swift↔Go contract drift]** → `status --json` is versioned and validated by a golden test in Go; the Swift decoder fails loudly on unknown `hub_state` values.
- **[Zero-tool Linux connector is of limited value]** → Explicitly acknowledged; v1 delivers the daemon/auth/project plumbing so tool hooks can land in a follow-up without new infrastructure.

## Migration Plan

1. **Phase 1 — Linux v1:** add `daemon`, `install`, `uninstall`, the systemd user unit template, XDG paths, reconnect, and the instance guard; token-only `init`/`relay` unchanged; add Linux build/release wiring. Ships a working Linux connector.
2. **Phase 2 — shared brain:** adopt the SDK `auth`/`apitokens`/`projects` packages (closing the `offline_access` scope gap) and add the account/project store, token mint/reuse, secret store, `status --json`, and supervision policy with Go tests.
3. **Phase 3 — macOS adoption:** port the app onto the JSON/CLI surface and delete the Swift brain; keep views and menu-bar handling.
   - **Rollback:** each phase is independently revertible. Phase 3 reverts to the in-tree Swift brain with no data migration because on-disk secret/config formats are unchanged.

## Release Boundaries

Each phase is **independently shippable**; no phase depends on a later one.

**Linux v1 (Phase 1)** — ships without the brain:

- In: `daemon`, `install` / `uninstall` (systemd user unit), XDG paths + journald, reconnect, single-instance guard, empty tool set with explanatory status, `linux/amd64` + `linux/arm64` binaries.
- Config: token-only via the existing `init`; `~/.config/memory-connector.yml` unchanged.
- `status` stays text (no `--json`); macOS is untouched.
- MUST NOT depend on: the Memory SDK, OIDC, credential store, `status --json`, or any macOS change.
- Exit criteria: a user can `init` with an `emt_*` token, `install`, and see the node connected in the hub, with a healthy `systemctl --user status`.

**Shared brain (Phase 2)** — additive to the same binary:

- In: SDK `auth` / `apitokens` / `projects` adoption, device-flow `auth login` / `auth logout`, `projects list` / `projects use`, token mint/reuse/revoke, credential store (+ optional CLI-session import), config materialization, `status --json`, supervision policy.
- Prerequisite: the upstream SDK scope change is merged **or** the local fallback ships (D11).
- Backward compatible: token-only configs keep working; `init` / `relay` / `status` text output unchanged.

**macOS adoption (Phase 3)** — separate, behavior-preserving:

- In: the Swift app consumes the JSON/CLI surface; the duplicated Swift brain is deleted.
- Guard: existing Swift tests stay green; revert to the in-tree Swift brain on regression.

**Versioning and publishing:**

- Publish Linux binaries as release artifacts (goreleaser) for `linux/amd64` and `linux/arm64`; version reported by no-args / `--version`.
- Phase 1 and Phase 2 may ship as separate versions; `status --json` is introduced in Phase 2 and versioned from the start.
- No data migration between phases: config format and paths are unchanged; Phase 2 only adds a credential store alongside the existing config.

## Port Plan: Swift brain → connector-core

Reuse-versus-port split, verified against the Swift sources (`client/macos/MemoryConnector/Sources`) and the SDK:

| Swift | Destination | Mode |
|---|---|---|
| `OIDCClient.discovery` | SDK `auth.DiscoverOIDC` | reuse |
| `OIDCClient.authorizeURL` (PKCE S256) | core `internal/account/pkce.go` | port |
| `OIDCClient.exchangeCode` (auth-code grant) | core | port |
| `OIDCClient.refresh` | SDK `auth.OAuthProvider.Refresh` | reuse |
| `OIDCClient.revoke` | core (RFC 7009) | port — SDK lacks it |
| `OIDCClient.endSessionURL` | core | port |
| `OIDCClient.userinfo` | core → `/api/auth/me` | port / replace |
| `OIDCSessionStore` (0600 `session.json`, legacy Keychain migration) | SDK `auth.Credentials` + `internal/secretstore`; one-time Keychain migration | port |
| `AccountStore`, `ProjectStore`, `MemoryAPIClient` | `internal/account`, `internal/memoryapi` (SDK `projects`/`apitokens`) | port → reuse |
| `EngineConfigWriter` | `internal/config` materialization | port |
| `StatusSnapshot` parser | `internal/status` JSON | replace |
| `EngineManager` restart policy | `internal/supervise` | port |

Two distinct seams:

- **Request-time auth** — the SDK `auth.Provider` interface (`Authenticate` / `Refresh`). Unchanged; used for all API calls.
- **Login-time flow** — a new core `AuthFlow` seam, because the SDK only implements device flow:

```go
type AuthFlow interface {
	Name() string
	Run(ctx context.Context, oidc *auth.OIDCConfig) (*auth.Credentials, error)
}
```

Implementations: `DeviceFlow` (wraps the SDK `OAuthProvider`; the Linux default), `PKCESchemeFlow` (macOS custom scheme), and optionally `PKCELoopbackFlow` later.

macOS custom-scheme bridging — keeps the callback in the app but the logic in the core:

1. `memory-connector auth start --flow pkce --redirect-uri <scheme> --json` → `{login_id, authorize_url, state}`; the core persists a one-shot pending-login record (state, `code_verifier`, redirect URI, issuer, client id) at 0600.
2. The GUI opens `authorize_url` in the system browser.
3. The provider redirects to the registered custom scheme; the GUI reads `code` and `state`.
4. `memory-connector auth complete --login-id <id> --code <code> --state <state> --json` → the core validates state (maps Swift `stateMismatch`), exchanges code + verifier, refreshes, persists credentials, and returns identity.
5. Pending-login records are one-shot with a TTL; stale or unknown ids fail cleanly.

This preserves the macOS PKCE custom-scheme behavior (so no `mac-connector-auth` delta) while keeping PKCE, token exchange, and storage inside the shared core.

- **Known constraint:** SDK `DiscoverOIDC` requires `device_authorization_endpoint` and `userinfo_endpoint`. Zitadel provides both; if a future provider lacks the device endpoint, the core's PKCE path performs its own discovery. Deferred unless it bites.

### macOS bridging IPC contract (Phase 3)

The GUI never performs OIDC itself; it calls the two-step core surface. Commands and payloads:

`auth start` →

```
memory-connector auth start --flow pkce --redirect-uri <scheme> [--server <url>] --json
{ "login_id": "...", "authorize_url": "...", "state": "...", "expires_at": "..." }
```

- Persists a pending-login record (0600, state dir): `login_id`, `state`, `code_verifier`, `redirect_uri`, issuer, client id, `created_at`, `expires_at`.
- `authorize_url` contains `code_challenge` (S256) only; the `code_verifier` is never emitted.

`auth complete` →

```
memory-connector auth complete --login-id <id> --code <code> --state <state> --json
{ "account": { "email": "...", "issuer": "..." }, "expires_at": "..." }
```

- Validates `login_id` exists and is unexpired, then that `state` matches the stored value; on success exchanges code + verifier, refreshes, persists credentials, deletes the pending record (one-shot), and returns identity.

`auth cancel --login-id <id>` clears a pending record (best-effort). `auth status --json` reports signed-in state, account, and expiry.

Error contract (stable machine codes, `--json`):

| Code | Cause |
|---|---|
| `unknown_login` | no such pending `login_id` |
| `expired_login` | pending record past `expires_at` |
| `state_mismatch` | returned `state` does not match (maps Swift `stateMismatch`) |
| `exchange_failed` | token exchange rejected or unreachable |
| `provider_unusable` | issuer/discovery failure |

Semantics:

- Multiple concurrent `auth start` calls are allowed and isolated by `login_id`.
- Pending records have a TTL (aligned with the provider's authorization-code lifetime) and are pruned on access.
- The GUI opens `authorize_url` with the system browser, receives the custom-scheme redirect, then calls `auth complete`. A stale record after an app restart simply fails and the user restarts sign-in.

## Open Questions

- The exact Linux tool set for the follow-up change (filesystem, shell, browser) is intentionally deferred; v1 registers no local tools.
