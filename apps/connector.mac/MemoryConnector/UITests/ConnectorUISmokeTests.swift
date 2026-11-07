import XCTest

/// App-level UI smoke suite: drives the running SwiftUI app via XCUITest.
///
/// The app is launched as a SEPARATE process in UI-test mode (`-uiTestMode`),
/// which redirects it to an isolated Application Support profile keyed by a
/// fresh `MEMORY_UI_TEST_PROFILE` UUID. It therefore never touches the real
/// config, Keychain, or signed-in accounts, and never starts the engine or the
/// status poller.
///
/// Every assertion keys off a stable `accessibilityIdentifier` — never view
/// hierarchy or localized text — and uses generous `waitForExistence` timeouts
/// (no animation-dependent waits). The menu-bar `NSStatusItem`/popover is
/// deliberately out of scope.
///
/// MUST run in a GUI/WindowServer session on the Mac; XCUITest cannot run over
/// the headless ssh build path (use `Scripts/mac-uitest-agent.sh`). The first
/// run on a host needs a one-time on-screen Automation (TCC) approval; in our
/// test a full rebuild with a fresh ad-hoc signature re-ran without
/// re-prompting (a stable identity remains available via
/// `mac-setup-signing.sh`). See
/// apps/connector.mac/Scripts/mac-build.sh (`--uitest`).
///
/// `@MainActor` on the class: the XCUIElement APIs are main-actor-isolated in
/// the macOS 27 SDK. XCTest invokes test methods on the main thread, so test
/// methods and private helpers may be main-actor-isolated — but the
/// `XCTestCase` `setUp`/`tearDown` overrides are NOT (they are declared
/// `nonisolated` on the superclass, and an override cannot add isolation), so
/// this suite has none: each test launches its own app via `launchApp()`.
/// XCTest terminates apps launched during a test when that test ends.
@MainActor
final class ConnectorUISmokeTests: XCTestCase {
    /// Generous, animation-independent wait. The main window and each page root
    /// exist as soon as the view is built; nothing here depends on a transition
    /// or animation completing.
    private static let launchTimeout: TimeInterval = 30
    private static let elementTimeout: TimeInterval = 15

    /// Sidebar rows to click, paired with the page root each must reveal, in
    /// the sidebar's own INFORMATION → SETTINGS → About order (every
    /// `SidebarItem` case).
    private static let pages: [(sidebar: String, page: String)] = [
        ("sidebar.dashboard", "page.dashboard"),
        ("sidebar.project", "page.project"),
        ("sidebar.tools", "page.tools"),
        ("sidebar.mcpServers", "page.mcpServers"),
        ("sidebar.permissions", "page.permissions"),
        ("sidebar.connection", "page.connection"),
        ("sidebar.about", "page.about"),
    ]

    /// Chrome controls that must be present on the main window. The UI-test
    /// profile is signed out, so the project control is deliberately absent:
    /// it would repeat the account control's "Sign in" button (see
    /// `assertChrome`).
    private static let chromeIdentifiers = [
        "toolbar.account",
        "status.footer",
    ]

    // MARK: - Scenarios

    func testMainWindowOpens() {
        let app = launchApp()
        assertMainWindowOpens(app)
    }

    func testEverySidebarPageRenders() {
        let app = launchApp()
        assertMainWindowOpens(app)
        for entry in Self.pages {
            let row = element(app, entry.sidebar)
            XCTAssertTrue(
                row.waitForExistence(timeout: Self.elementTimeout),
                "missing sidebar row \(entry.sidebar)"
            )
            row.click()
            let page = element(app, entry.page)
            XCTAssertTrue(
                page.waitForExistence(timeout: Self.elementTimeout),
                "page root missing for \(entry.page)"
            )
        }
    }

    func testChromeControlsPresent() {
        let app = launchApp()
        assertMainWindowOpens(app)
        for identifier in Self.chromeIdentifiers {
            XCTAssertTrue(
                element(app, identifier).waitForExistence(timeout: Self.elementTimeout),
                "missing chrome control \(identifier)"
            )
        }
        // Signed out, the account control is the only sign-in affordance. A
        // project control here would render a second "Sign in" label beside it.
        XCTAssertFalse(
            element(app, "toolbar.project").exists,
            "the project control must not be installed while signed out"
        )
    }

    // MARK: - Helpers

    /// Launches the app in UI-test mode against a fresh isolated profile.
    private func launchApp() -> XCUIApplication {
        continueAfterFailure = false
        let app = XCUIApplication()
        app.launchArguments = ["-uiTestMode"]
        app.launchEnvironment["MEMORY_UI_TEST_PROFILE"] = UUID().uuidString
        app.launch()
        return app
    }

    private func assertMainWindowOpens(_ app: XCUIApplication) {
        XCTAssertTrue(
            app.windows.firstMatch.waitForExistence(timeout: Self.launchTimeout),
            "main window did not open"
        )
    }

    /// Resolves an identifier against any descendant element type, so the suite
    /// never depends on whether a sidebar row/page root/chrome control surfaces
    /// as a button, static text, group, or otherwise.
    private func element(_ app: XCUIApplication, _ identifier: String) -> XCUIElement {
        app.descendants(matching: .any)[identifier].firstMatch
    }
}
