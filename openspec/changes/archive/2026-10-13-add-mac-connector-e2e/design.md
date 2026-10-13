## Context

Two coverage gaps, two different harnesses. Facts that drive the design:

- The app is `MemoryConnector` (`project.yml`), product `Memory.app`, macOS 15, Swift 6, XcodeGen. Its **only** test target is `MemoryConnectorTests` (`bundle.unit-test`, hosted via `TEST_HOST`). There is no `bundle.ui-testing`.
- The app registers a SwiftUI `Window("Memory", id: "main")` scene (`MemoryConnectorApp.swift`) plus an AppKit `NSStatusItem`/`NSPopover` menu bar (`StatusItemController.swift` — deliberately not `MenuBarExtra`).
- Hosted unit tests are detected by `HostedTest.isRunning` via `XCTestConfigurationFilePath`. A UI test launches the app as a **separate process**, so that env var is absent there — UI isolation needs its own signal.
- The remote build path `tools/mac-build.sh --test` runs `xcodebuild ... test` **over ssh**, ad-hoc signed. macOS XCUITest needs a GUI/WindowServer session and a TCC Automation grant, and ad-hoc signing resets grants (`project.yml` signing comment). So UI tests cannot run on the ssh path.
- The engine binary is `apps/connector.linux/cmd/memory-connector` (its own Go module), embedded into the mac app bundle and built `GOOS=darwin` by `Scripts/build-engine.sh`. On darwin `internal/tools` selects the non-Linux provider, and `internal/appletools` supplies the Apple tools.
- The hermetic daemon e2e (`apps/connector.linux/e2e/connector_daemon_e2e_test.go`) already builds the real binary and drives it against an in-process `httptest` fake hub reusing production frame types — but skips everything non-Linux and asserts `linux-host-info`.

## Goals / Non-Goals

**Goals**
- Deterministic, hermetic app-level UI smoke covering window + navigation + chrome.
- Deterministic darwin engine↔hub coverage (register → tool list → status) on a macOS host.
- Both runnable on the self-hosted `[self-hosted, macOS, ARM64, mac]` runner.

**Non-Goals**
- Menu-bar popover automation (not reliably reachable by XCUITest on current macOS).
- OAuth/sign-in web round-trips (stay manual; the UI test uses an isolated, unauthenticated profile).
- Asserting Apple tools that need live TCC/Notes/Reminders data (register/tools-list only; no permissioned calls).
- A full Playwright round-trip against a dev Memory (remains the deferred manual smoke in the backlog task).

## Decisions

### D1 — UI coverage targets the main window, not the menu bar
`XCUIApplication()` drives the app; assert `app.windows.firstMatch` and the `NavigationSplitView` sidebar pages. The `NSStatusItem` is skipped: status-bar extras are owned by Control Center and are not a stable XCUITest element, so any coverage there would be flaky. *Alternative considered:* `XCUIApplication(bundleIdentifier: "com.apple.controlcenter")` status-item hacks — rejected as brittle.

### D2 — Isolation via an explicit `-uiTestMode` launch argument
A UI test spawns the app with `launchArguments = ["-uiTestMode"]` and a unique `MEMORY_UI_TEST_PROFILE` value. `AppEnvironment`/`AppDelegate` then use a temp Application Support directory, skip `importExistingConfigIfNeeded`, do not start the engine or the status poller, and never touch the real Keychain. This mirrors the existing `HostedTest` isolation intent without reusing its detection (which is false in a launched app process). *Alternative:* reuse `HostedTest` — rejected: the app-under-test has no XCTest env vars.

### D3 — Accessibility identifiers as the test contract
Add `.accessibilityIdentifier` to sidebar rows (`sidebar.<page>`), page roots (`page.<name>`), toolbar controls (`toolbar.project`, `toolbar.account`), and the footer (`status.footer`) — the SwiftUI equivalent of the web UI's `data-testid` convention. Views already carry one `.accessibilityLabel` (`IdentifierReveal.swift`), so this is additive.

### D4 — Darwin engine e2e = a runtime-gated sibling of the Linux harness
Add a `darwin` variant test (runtime `GOOS` gate, `-short` skip) that reuses the existing `fakeHub`/`startDaemon`/`runStatusJSON` helpers and asserts `register` + the darwin tool set (Apple tools present) + `status --json` connected. Do **not** parameterize away the Linux test — keep each OS's expectations explicit and local. A tool *call* round-trip stays Linux-only because the permission-free probe tool (`linux-host-info`) has no darwin analogue; darwin asserts the handshake and registration instead.

### D5 — UI suite runs only in a GUI session with stable signing
The suite is invoked on the mac runner from a logged-in session (not the ssh `mac-build.sh` path) and must be signed with the stable identity from `tools/mac-setup-signing.sh` so Automation grants persist across runs. Documented as `mac-build.sh --uitest` + a runner note; CI wiring is follow-up.

## Risks / Trade-offs

- **XCUITest flakiness** → keep the suite to ~4–5 smoke assertions, no animation-dependent waits, generous `waitForExistence` timeouts.
- **TCC grant setup** is a one-time manual step on the mac runner → documented, not automated in this change.
- **Identifiers drift from tests** → keep identifiers adjacent to the view they tag and name them by page, not by index.
- **Darwin tool set changes** → the darwin e2e asserts *presence* of the Apple tool ids it knows; a new tool doesn't break it, a removed one does (desired).

## Migration Plan

Additive only — new target, new files, new identifiers, new test. No behavior change for existing users; `-uiTestMode` is inert unless passed.

## Open Questions

- Should the darwin e2e also assert per-account config isolation (the backlog's "account add/switch" case), or is that better as a Swift-side `AccountStore`/`ProjectStore` test? Leaning: keep it Swift-side (already unit-covered) and leave the Go e2e to the wire handshake.
