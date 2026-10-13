# mac-connector-e2e Specification

## Purpose
Defines end-to-end coverage for the macOS connector: an app-level UI smoke suite that drives the running SwiftUI app, and a hermetic darwin engine↔hub test. It also fixes the launch-isolation and accessibility-identifier contracts those tests depend on.

## Requirements

### Requirement: App-level UI smoke coverage

The macOS connector SHALL have a `bundle.ui-testing` target (`MemoryConnectorUITests`) that drives the running app via XCUITest and is added to the `MemoryConnector` scheme's test action alongside the hosted unit tests. The suite SHALL remain small, deterministic, and free of animation-dependent waits.

#### Scenario: Main window opens
- **WHEN** the UI test launches the app in UI-test mode
- **THEN** the main window exists within the launch timeout

#### Scenario: Every sidebar page renders
- **WHEN** the test clicks each sidebar row
- **THEN** the corresponding page root element exists, for each of the dashboard, project, tools, mcpServers, permissions, connection, and about pages (the `SidebarItem` cases)

#### Scenario: Chrome controls are present
- **WHEN** the main window is shown
- **THEN** the project and account toolbar controls and the status footer are present

#### Scenario: Menu bar is out of scope
- **WHEN** automated UI coverage is written
- **THEN** it SHALL NOT depend on the menu-bar `NSStatusItem`/popover, which is not a stable XCUITest element

### Requirement: UI-test launch isolation

The app SHALL detect a UI-test launch (a `-uiTestMode` launch argument) and, in that mode, run against an isolated profile: a temporary Application Support directory, no legacy-config import, no engine start, no status polling, and no access to the real Keychain or signed-in accounts. The mode SHALL be inert when the argument is absent.

#### Scenario: Isolated profile
- **WHEN** the app is launched with `-uiTestMode`
- **THEN** it does not read or write the real user config, Keychain, or account state

#### Scenario: Engine not started
- **WHEN** the app is launched with `-uiTestMode`
- **THEN** the relay engine and status poller do not start

#### Scenario: Inert without the flag
- **WHEN** the app is launched without `-uiTestMode`
- **THEN** behaviour is unchanged from today

#### Scenario: Distinct from hosted unit tests
- **WHEN** UI-test mode is detected
- **THEN** detection SHALL be based on the launch argument, not on the hosted-unit-test environment check (which is false in a launched app process)

### Requirement: Accessibility identifiers for testability

Sidebar rows, page roots, toolbar controls, and the status footer SHALL carry stable `accessibilityIdentifier`s named by page/role (not by index), so the UI suite does not depend on view hierarchy or localized text.

#### Scenario: Identifiers are page-named
- **WHEN** a sidebar row or page root is tagged
- **THEN** its identifier encodes the page name (e.g. `sidebar.tools`, `page.tools`)

#### Scenario: Toolbar and footer are reachable
- **WHEN** the UI suite asserts chrome
- **THEN** the project/account toolbar controls and the status footer each expose a stable identifier

### Requirement: Darwin engine relay end-to-end coverage

The connector SHALL have a hermetic end-to-end test that builds the real `memory-connector` binary and runs it as a subprocess against an in-process fake relay hub, reusing the production frame types, asserting the daemon reaches the hub and reports connected. The darwin variant SHALL run on macOS and SHALL skip under `-short` and on non-darwin hosts.

#### Scenario: Register and tool list on darwin
- **WHEN** the darwin e2e starts the daemon against the fake hub
- **THEN** the hub receives a `register` frame whose tool list includes the Apple tool ids the mac app registers

#### Scenario: Status reports connected
- **WHEN** the daemon is registered
- **THEN** `status --config <cfg> --json` reports the matching `instance_id`, `hub_state == "connected"`, and `schema_version == 1`

#### Scenario: Skips off-platform and in short mode
- **WHEN** the test runs on a non-darwin host, or with `-short`
- **THEN** it skips instead of failing

#### Scenario: Linux coverage is unchanged
- **WHEN** the darwin variant is added
- **THEN** the existing Linux daemon e2e still asserts the Linux tool set and is not weakened

### Requirement: UI suite runner contract

The UI suite SHALL be runnable only in a GUI/WindowServer session and SHALL be documented as such, because macOS XCUITest cannot run on the headless ssh build path. The first run on a host requires a one-time on-screen Automation (TCC) approval.

#### Scenario: Documented runner
- **WHEN** a developer or CI wants to run the UI suite
- **THEN** there is a documented command/mode for a GUI-session run on the mac runner, and a note that the ssh `mac-build.sh --test` path cannot run it
