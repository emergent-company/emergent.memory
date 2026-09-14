# Design — add-mcp-connector-app (macOS menu-bar connector app)

## Context

Engine exists and is proven on the user's Mac (relay stable, Apple tools
return real data; see `docs/sessions/2026-09-09-connector-macos-smoke.md`).
This change wraps it in a native menu-bar app. Reference implementation:
Diane's macOS companion snapshot (`emergent-company/diane@e168c3ed`,
`server/swift/DianeCompanion`, MIT) — Window + MenuBarExtra, engine child
supervision w/ circuit breaker, per-feature permission cards. Repo facts:
iOS project at `client/ios/VoiceAgent` (xcconfig team 74LC88G9SC, empty
entitlements, committed .pbxproj); mac build/deploy = rsync to mcj-mini +
xcodebuild (tools/ios-build-mac.sh pattern); no macOS Swift target exists.

TCC ground truth (researched): a DIRECT `Process` child keeps the app's
responsibility chain → automation prompts name the signed app and each grant
happens once. launchd-spawned copies are their own identity; `open`/
NSWorkspace break the chain. Sandbox OFF + Hardened Runtime + empty
entitlements is the Diane-verified shape (osascript children need no
automation entitlement; in-process NSAppleScript would).

## Goals / Non-Goals

Goals: menu-bar agent app that (1) configures a Memory project with the token
in Keychain, (2) runs the embedded engine as a direct child, (3) shows
engine/hub/tool status, (4) lets the user enable/disable local Apple MCP
tools (engine `disabled_tools`), (5) makes the one-time Notes/Reminders
Automation grants visible and triggerable. Engine change stays minimal.

Non-goals (later): launchd autostart, notarization/DMG/cask, extra Apple
services (Contacts/Calendar/Mail), QR/token-endpoint onboarding, node
lifecycle management in Memory UI, Playwright e2e.

## Decisions

### 1. XcodeGen project (committed project.yml), not hand-authored pbxproj

`client/macos/MemoryConnector/project.yml` + generated
`MemoryConnector.xcodeproj` produced on the Mac during build (xcodegen via
Homebrew — installed on mcj-mini once if missing; check `xcodegen --version`
first). Mirrors Diane's approach and keeps project diffs reviewable.
Alternative — commit a pbxproj — rejected: authored blind on a server,
merge-hostile.

### 2. Signing/build settings (Diane-verified + repo conventions)

- Bundle `com.emergent.memory.connector`; `DEVELOPMENT_TEAM=74LC88G9SC`;
  `CODE_SIGN_STYLE=Automatic`; `ENABLE_HARDENED_RUNTIME=YES`; **App Sandbox
  OFF** (omit `com.apple.security.app-sandbox`); entitlements file EMPTY.
- `Info.plist`: `LSUIElement` true (menu-bar agent), `LSMinimumSystemVersion`
  15.0, CFBundle* names. No privacy usage strings needed for the v1 path
  (osascript-only Apple events; no EventKit/Contacts APIs in the engine).
- The engine is spawned only as a direct child → single TCC identity.

### 3. Engine embedding + supervision (`EngineManager`)

- Run-script build phase (project.yml `preBuildScripts`, declared output):
  `cd "$SRCROOT/../../connector" && CGO_ENABLED=0 GOARCH=arm64 go build -o
  "$TARGET_BUILD_DIR/$UNLOCALIZED_RESOURCES_FOLDER_PATH/memory-connector"
  ./cmd/memory-connector`.
- `EngineManager` (adapted from Diane `APIServerManager` direct-child path):
  locate `Bundle.main` resource (executable check), spawn `Process`, pipe
  stdout/stderr via `readabilityHandler` into
  `~/Library/Logs/memory-connector-app.log`, `terminationHandler` + circuit
  breaker (max 3 restarts / 60s, then wait for user), expose `state`, and
  `stop()` → SIGTERM (engine handles SIGTERM cleanly) on quit.
- No launchd in v1 (identity + lifecycle reasons; revisit for autostart).

### 4. Keychain + engine config materialization

- `KeychainStore`: token only, service `com.emergent.memory.connector`,
  account `project-token` (Security framework). Server URL, instance id,
  disabled tools live in `UserDefaults` (non-secret).
- `EngineConfigWriter`: builds the engine YAML
  (`server_url`, `token`, `instance_id`, optional `disabled_tools`) and writes
  `~/.config/memory-connector.yml` (0600, parent 0700) from the Keychain
  value before every engine (re)start — disk never holds the long-lived
  secret unless the engine is about to read it. Regenerate + restart on Save.
- Instance id default mirrors engine (`<hostname>-connector`).

### 5. Status via the engine's own CLI, not a Swift relay client

`StatusMonitor` polls (5s, 2s×20s burst at launch like Diane) by running the
embedded engine's `status --config` subprocess (short-lived Process, 10s
timeout) and parses stdout: instance, `tools (n): …`, `hub: …` line. No
duplicated relay/WS logic in Swift; hub presence uses the exact engine
code path. Menu-bar icon state: connecting / connected / disconnected / error.

### 6. Tool enablement drives engine config

App-known tool ids (`notes_search`, `notes_create`, `reminders_list`,
`reminders_add`) with descriptions; toggles persist the disabled subset →
config `disabled_tools` → engine (re)start. Engine change (Section "Impact",
tasks 1.x) filters registration, `status`, and dispatch for that list.

### 7. Permission cards: probe-to-trigger + stored state

Per service (Notes, Reminders): run a harmless probe
(`tell application "Notes" to get name` / `… "Reminders" to get name of every
list`) through a short-lived osascript `Process` (5s timeout). Outcome:
exit 0 → granted; stderr -1743 → denied; timeout/hang → unknown (prompt may
be pending). Cache last result + a `requested` flag in UserDefaults; card
renders Granted / Denied / Not granted yet and offers Authorize (runs probe —
first run triggers the one-time prompt attributed to the app) and a deep link
`x-apple.systempreferences:com.apple.preference.security?Privacy_Automation`.
Copy explains grants are per-app and persist (no per-terminal prompts).
Note: macOS exposes no programmatic Automation-status API; probe is the
truth.

### 8. Build/deploy: `tools/mac-build.sh`

Mirror `tools/ios-build-mac.sh`: rsync repo → mcj-mini (`~/code/alftred`,
same excludes), remote `xcodegen generate` (idempotent) + `xcodebuild
-project client/macos/MemoryConnector -scheme MemoryConnector -destination
'platform=macOS' -derivedDataPath build/DerivedData
CODE_SIGNING_ALLOWED=YES -allowProvisioningUpdates`, optional `--install`
(copy `.app` to `~/Applications`, `open`) and `--test` (xcodebuild test,
hosted unit tests). Manual smoke runs the app on the user's Mac (GUI —
requires the user for the one-time grants).

## Risks / Trade-offs

- [Automation permission state has no API] → probe + cached state + pane
  deep link; document denied/cached behavior (System Settings shows rows;
  card may say unknown until probed).
- [First launch of a dev-signed app → Gatekeeper] → built with team signing
  on the user's own Mac (no quarantine); if copied, `xattr -dr
  com.apple.quarantine` note in script output.
- [Engine dies with app quit] → intended v1; launchd autostart = follow-up.
- [go build inside Xcode run-script slows builds] → cached by go; acceptable
  for a dev tool.
- [XcodeGen absent on mcj-mini] → install once via brew (checked in build
  script with clear error + install hint).
- [Reminders list perf fixed but uncommitted in shared tree]
  (sibling merge conflict blocks commits) → commit first chance index is
  clean; implementation rides a worktree lane anyway.

## Migration Plan

Greenfield app + one additive engine feature. No backend/gateway changes.
Rollback = remove app + config file; engine untouched when `disabled_tools`
absent.

## Open Questions

- Exact AppleScript probe strings for Reminders (list query) — resolved
  during implementation; no spec impact.
- Token provisioning UX beyond manual paste (QR later) — later phase.

## Addendum — Sidebar navigation + project/user identity

Decision (after real-Mac feedback): the window becomes a Diane-style
`NavigationSplitView` (sidebar List with `Section`s + detail switch), splitting
INFORMATION from SETTINGS:

- Information: **Overview** (engine/hub status, tools count), **Project &
  Account** (project + user card).
- Settings: **MCP Tools**, **Permissions**, **Connection**, **About**.

Identity data (same project-scoped token; no extra scopes needed):
- `GET /api/auth/me` → `{user_id,email,scopes,project_id,project_name,org_id}`
  (best one-shot; `RequireAuth` only).
- `GET /api/user/profile` → `{firstName,lastName,displayName,email,...}`
  (`RequireAuth` only).
- `GET /api/projects/current` → `ProjectDTO{id,name,orgId,...}` (`RequireAuth`
  only). Full `GET /api/projects/:id`/members/orgs need `projects:read` —
  optional, not required for the card.
- Avatar: Memory exposes only an avatar object key; the gateway composes
  `/api/user/avatar?v=<key>` (upstream memory route not located). v1 renders
  **initials** from display name; image URL support is a follow-up.
- Degradation: sandbox tokens (user_id NULL) or unreachable server → the card
  shows "identity unavailable"; the rest of the app works.

Implementation split: `MemoryAPIClient` (URLSession, token from Keychain,
models, fixture-tested parsing) as a headless unit; sidebar/pages as the
design-owned UI layer consuming it. `SidebarItem` enum mirrors Diane
(rawValue label + system icon, `CaseIterable`, selection in an
`AppState`-style observable), and content is switched through an
`AnyView`-returning function (Diane idiom, avoids generic metadata blowups).

