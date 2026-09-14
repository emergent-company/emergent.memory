# Tasks — add-mcp-connector-app

TDD where executable (engine Go tests; Swift hosted unit tests for pure
helpers run via xcodebuild test on mcj-mini). Implementation in a worktree
lane (shared checkout has an unrelated unresolved merge; lane keeps commits
unblocked). Coordinate: the connector `disabled_tools` engine work and the
Swift app are separable; engine tasks first (testable on this server).

## 1. Engine: disabled_tools (connector/)

- [x] 1.1 Add optional `DisabledTools []string` (`disabled_tools`) to
      `internal/config` Config (yaml), validated (unknown tool names warned,
      not fatal; empty list = no filtering). Verify: config unit tests.
- [x] 1.2 Apply filter where tools are registered: `relay` subcommand builds
      the tool set, filters out disabled names, and registers the remainder.
      Verify: fake-hub test — tools listed exclude disabled ones.
- [x] 1.3 Dispatch rejects disabled/unknown tools with a clear error naming
      the tool. Verify: fake-hub test — call to disabled tool returns error.
- [x] 1.4 `status` reflects the filtered tool list. Verify: unit test —
      status output excludes disabled tools (matches `tools (n): …` shape).
- [x] 1.5 Gate: `go build ./...`, `go vet ./...`, `go test ./... -count=1`,
      `golangci-lint run ./...` all pass in `connector/`.

## 2. macOS app scaffold (client/macos/MemoryConnector)

- [x] 2.1 `project.yml` (XcodeGen): app target `MemoryConnector`, bundle
      `com.emergent.memory.connector`, macOS 15.0 deployment, LSUIElement in
      Info.plist, empty entitlements, ENABLE_HARDENED_RUNTIME=YES,
      CODE_SIGN_STYLE Automatic, DEVELOPMENT_TEAM 74LC88G9SC, macOS scheme.
      Verify: `xcodegen generate` on mcj-mini produces a project (install
      xcodegen via brew first if missing — note in script output).
- [x] 2.2 SwiftUI app entry: `MenuBarExtra` (window style) + Settings scene;
      status icon asset set (connected / disconnected / connecting / error).
      Verify: app builds + launches headless on mcj-mini (process appears).
- [x] 2.3 `tools/mac-build.sh` mirroring ios-build-mac.sh: rsync → mcj-mini,
      xcodegen generate + xcodebuild macOS scheme, flags `--install`,
      `--test`, `--no-sync`. Verify: script builds on mcj-mini.

## 3. Engine embedding + supervision

- [x] 3.1 Run-script build phase: `go build` engine (darwin/arm64) into
      Contents/Resources/memory-connector, declared output file. Verify:
      built .app contains executable resource.
- [x] 3.2 `EngineManager`: locate resource, spawn direct Process child,
      pipe stdout/stderr to `~/Library/Logs/memory-connector-app.log`,
      terminationHandler + circuit breaker (3 restarts / 60s), expose state,
      `stop()` SIGTERM. Verify: Swift unit test (hosted) for state/restart
      bookkeeping; manual launch on mcj-mini shows logs + clean stop.
- [ ] 3.3 App quit stops the engine cleanly. Verify: launch app, quit, engine
      process gone; hub session disappears.

## 4. Configuration, Keychain, connection settings

- [x] 4.1 `KeychainStore` (Security framework): set/get/delete token for
      service `com.emergent.memory.connector`. Verify: hosted unit test
      round-trip (keychain available in hosted test run on Mac).
- [x] 4.2 `EngineConfigWriter`: materialize `~/.config/memory-connector.yml`
      (0600, parent 0700) from Keychain token + UserDefaults (server URL,
      instance id, disabled tools) before engine start/restart. Verify:
      unit test — yaml content, permissions, disabled_tools omitted when
      empty.
- [ ] 4.3 Settings → Connection view: server URL, token (paste,
      SecureField), instance id (default `<hostname>-connector`); Save →
      Keychain + config write + engine (re)start. Verify: manual smoke on
      mcj-mini (config lands, engine registers with hub).
- [ ] 4.4 Editing config while engine runs restarts it with new values.
      Verify: manual smoke — change instance id → hub shows new id.

## 5. Menu bar + status

- [ ] 5.1 Menu-bar item + compact menu: status summary, registered tool
      count, Open Settings, Quit; icon reflects state. Verify: manual smoke.
- [x] 5.2 `StatusMonitor`: poll engine `status --config` (5s; 2s×20 burst at
      launch), parse stdout (instance/tools(n)/hub line) → state. Verify:
      hosted unit test parses fixture outputs (connected/not
      connected/auth-failed); manual smoke shows connected state while relay
      runs.

## 6. Local MCP tools + Permissions UI

- [ ] 6.1 Settings → Local MCP tools: list engine Apple tools with
      descriptions + enable toggles; persist disabled set → regenerate config
      → restart engine. Verify: manual smoke — disabling `reminders_list`
      drops it from hub tools; call returns disabled error.
- [ ] 6.2 Settings → Permissions: Notes + Reminders cards — probe
      (harmless osascript, 5s timeout), state granted/denied/unknown cached,
      Authorize button, System Settings Automation deep link, grant-once
      copy. Verify: manual smoke on mcj-mini — first Authorize prompts once
      (user clicks Allow), card shows granted; repeat shows no prompt.

## 7. Verification gate + macOS smoke

- [x] 7.1 Engine gate rerun (build/vet/test/lint in connector/) green.
- [x] 7.2 `tools/mac-build.sh` build green on mcj-mini.
- [ ] 7.3 End-to-end smoke on the user's Mac: install app, configure project
      (reuse existing token/server from the earlier CLI smoke), engine
      registers, `/settings/mcp-nodes` shows node + 4 tools; grant Notes +
      Reminders once; live `notes_search` + `reminders_list` return real
      data; disable one tool → disappears + call error; record in a session
      doc.

## 8. Sidebar navigation + project/user identity (real-Mac feedback)

- [x] 8.1 `MemoryAPIClient` (Sources/): URLSession client using server URL +
  Keychain token; fetch `GET /api/auth/me`, `GET /api/user/profile`,
  `GET /api/projects/current`; decodable models; errors surfaced (auth failed
  / unreachable / no identity). Hosted tests with fixture JSON (connected,
  identity-missing, 401, malformed). Verify: hosted tests green.
- [ ] 8.2 Sidebar navigation: replace the single Settings view with a
  `NavigationSplitView` (sidebar `List` with two `Section`s: information +
  settings) driven by a `SidebarItem` enum (label + SF Symbol) and an
  `AppState`-style selection observable; detail switched via an
  `AnyView`-returning function. Verify: build + manual smoke (pages switch).
- [ ] 8.3 Pages split: Overview (engine/hub status + tools count), Project &
  Account (project name/id/org + user display name/email/avatar-initials,
  graceful "identity unavailable"), MCP Tools, Permissions, Connection, About.
  Existing sections move into their pages unchanged. Verify: manual smoke.
- [ ] 8.4 Project/Account data wiring: load identity on window appear + on
  token/connection change; loading/error states; refresh control. Verify:
  manual smoke + hosted parsing tests.
- [ ] 8.5 Gate: `tools/mac-build.sh --test` green; rebuild + install; user
  verifies sidebar, project card, user card.

## 10. Fallback token removal + account control (feedback)

- [x] 10.1 Remove fallback token: a "Remove token" affordance in Connection →
  Advanced clears the Keychain token, updates state (no token configured),
  writes/stops the engine appropriately, and leaves sign-in/OIDC untouched.
  Verify: hosted test for token removal + state; manual smoke.
- [x] 10.2 Header account control (top right): replace the unstyled sign-in
  button with a properly padded, right-aligned control (`.primaryAction`
  toolbar item). Signed out → padded "Sign in" button. Signed in → initials
  avatar; clicking opens a menu (name/email, "Profile", "Sign out").
  Verify: build + manual smoke.
- [x] 10.3 Avatar menu "Profile" navigates to the Project & Account page
  (like the web account menu). Verify: manual smoke.

- [x] 8.6 Resolve display names: extend `MemoryAPIClient`/models with
  `GET /api/orgs` (RequireAuth, no scope) and resolution of the organisation
  name for the current `org_id` (and keep project name from
  `/api/projects/current` / `/api/auth/me`); `IdentitySnapshot` exposes
  `projectName`/`organizationName`; hosted tests (org list matched by id,
  missing org → nil, fixtures). Verify: hosted tests green.
- [x] 8.7 UI: Project & Account shows **names** (project, organisation);
  raw IDs hidden behind a copy-icon control (reveal on hover, click copies to
  clipboard with a brief "copied" state); same treatment for instance id where
  shown. Verify: manual smoke + design review.

## 11. Dashboard rethink + avatar image + switcher styling (feedback)

- [x] 11.1 `MemoryAPIClient.avatarData(projectID:)`: GET `/api/user/avatar`
  with the user JWT (+ `X-Project-ID`); 200 → bytes, 404 → nil, 401 → auth
  error. Hosted tests (bytes/404/401/header assertions).
- [x] 11.2 Avatar view: load + cache the real picture (circular); fall back to
  an initials circle using Memory's account-avatar styling (brass primary bg,
  primary-content text) — used in the toolbar account control and the account
  card.
- [x] 11.3 Dashboard/Overview rethink: keep what the user needs (engine/hub
  status, node instance id behind the copy control, enabled tools), remove
  irrelevant/duplicated rows, tidy hierarchy/spacing.
- [x] 11.4 Project switcher control styling: proper padding/border so the label
  no longer looks forced into the toolbar frame (consistent with the account
  control).


