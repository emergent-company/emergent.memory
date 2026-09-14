## Why

The Go engine (`connector/memory-connector`) is proven — relay + register
stable, Apple Notes/Reminders tools return real data on the user's Mac — but
the headless CLI is a poor day-to-day surface:

- Token sits in a 0600 YAML; project config is manual.
- AppleScript Automation prompts fire from arbitrary ssh/terminal contexts:
  flaky to surface, occasionally auto-denied, and each launch context can get
  its own TCC record.
- Permission state is invisible; the user cannot see what the connector may
  access and must re-trigger grants blindly.
- No visual supervision of the relay process or its hub connection.

A small native menu-bar app embedding the Go engine fixes all of it at once:
the engine runs as a DIRECT child of the signed app, so every TCC grant
(Notes/Reminders Automation) is attributed to one stable app identity and
requested exactly once; the app surfaces each permission's state and lets the
user grant/see them in-app + in System Settings; MCP/local configuration moves
out of YAML editing into a form; and relay status is always visible.

Phase 2b v1 scope (per product direction): the MENU-BAR Mac app focused on
**MCP configuration and the local integration** — Memory project setup,
which local Apple MCP tools are enabled, one-time permissions, and
connection/tool status. Modeled on Diane's macOS companion (MIT, snapshot
`emergent-company/diane@e168c3ed`, `server/swift/DianeCompanion`): window +
MenuBarExtra, engine child supervision with circuit breaker, per-feature
permission cards with probe-to-trigger, sandbox OFF + Hardened Runtime,
empty entitlements, team 74LC88G9SC.

## What Changes

- **New native macOS app** `client/macos/MemoryConnector`: SwiftUI
  menu-bar agent app (`MenuBarExtra` window style + Settings scene,
  LSUIElement), no third-party deps, committed XcodeGen `project.yml`
  (no hand-authored .pbxproj), xcconfig signing mirroring iOS conventions.
- **Engine embedded + supervised**: Xcode run-script phase builds the Go
  engine (darwin/arm64) into `Contents/Resources/memory-connector`; the app
  spawns it as a direct `Process` child (never launchd/`open`), pipes logs to
  `~/Library/Logs/`, circuit-breaker restart (Diane pattern), SIGTERM clean
  stop on quit. Direct-child ⇒ TCC responsible process = the app ⇒ each
  Automation grant happens once for the app identity.
- **Connection settings**: server URL + project API token + instance id;
  token stored in the **Keychain** (source of truth); the app materializes
  `~/.config/memory-connector.yml` (0600) from the Keychain value before
  spawning the engine. Token entry is manual paste from Memory
  Settings → API Tokens in v1.
- **Menu bar**: status icon (engine running? node connected to hub?) +
  quick summary menu (status, tool count, open Settings, Quit).
- **Main window: Diane-style sidebar navigation** (`NavigationSplitView` +
  `SidebarItem` enum) splitting INFORMATION from SETTINGS into pages:
  Overview (engine/hub status + local tools served), Project & Account
  (connected project name/id/org + user display name, email, avatar-initials),
  MCP Tools (enable/disable), Permissions, Connection (server/token/instance),
  About. Project/identity data is fetched over REST with the same
  project-scoped token: `GET /api/auth/me` (user+project one-shot),
  `GET /api/user/profile` (name/email), `GET /api/projects/current` (project).
  Avatar falls back to initials (Memory exposes only an avatar object key).
- **Settings → Local MCP tools**: lists the engine's Apple tools
  (`notes_search`, `notes_create`, `reminders_list`, `reminders_add`) with
  per-tool enable toggles persisted as engine `disabled_tools`; note that
  enabled tools appear in Memory's `/settings/mcp-nodes` + agent picker.
- **Settings → Permissions**: per-service Automation cards (Notes,
  Reminders) — probe with a harmless osascript to trigger the one-time
  prompt, show granted/denied/unknown from probe results, deep-link to
  System Settings → Privacy & Security → Automation. Explains grant-once
  semantics (no per-terminal grants).
- **Engine extension** (`connector/`): optional `disabled_tools` in config;
  relay registers + `status` reports only enabled tools; dispatch rejects
  disabled tools. Small, unit-tested.
- **Build/deploy**: `tools/mac-build.sh` mirroring `tools/ios-build-mac.sh`
  (rsync → mcj-mini, xcodebuild macOS scheme, automatic signing w/
  DEVELOPMENT_TEAM) so the app builds and runs on the user's Mac.
- No launchd autostart, no notarization pipeline, no extra Apple services
  (Contacts/Calendar/Mail), no memory-node lifecycle UI beyond connect state —
  all later phases.

## Capabilities

### New Capabilities

- `mac-connector-app`: A menu-bar macOS app embedding the Memory connector
  engine — Memory project configuration (Keychain-stored token), direct-child
  engine supervision, connection/tool status, local MCP tool enablement, and
  one-time Automation permissions with visible state.

### Modified Capabilities

- `mcp-connector`: the engine SHALL support config-driven `disabled_tools` so
  the app can choose which local MCP tools are registered and served.

## Impact

- `client/macos/MemoryConnector/` (new): project.yml (XcodeGen), xcconfig,
  Swift sources (app, engine supervisor, keychain/config, settings views,
  permission probes), asset catalogs, Info.plist keys (LSUIElement etc.).
- `connector/`: config schema + relay registration/status/dispatch filter for
  `disabled_tools` (+ tests).
- `tools/mac-build.sh` (new, mirrors ios-build-mac.sh).
- No gateway / backend / emergent.memory changes.
- Deployment: dev-signed build on mcj-mini via the new script; install + run
  on the user's Mac; grant Notes/Reminders once; verify node on hub.
