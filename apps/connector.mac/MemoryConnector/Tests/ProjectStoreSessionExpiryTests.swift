@testable import MemoryConnector
import XCTest

/// `ProjectStore.reassertConnection()` when the connector CLI session expires.
///
/// A project is connected and its engine config (written by the CLI on a
/// long-lived project token) binds that project, but the OIDC access token
/// backing `projects use` has expired/cleared. The launch scope-settle must
/// keep the connected intent and leave the running engine alone — only a
/// non-auth failure tears the connection (and engine) down.
///
/// Kept in its own file so `ProjectStoreTests.swift` stays within its SwiftLint
/// file-length baseline; see `apps/connector.mac/Scripts/lint-swift.sh`.
final class ProjectStoreSessionExpiryTests: XCTestCase {
    private var suiteName = ""
    private var defaults = UserDefaults(suiteName: "") ?? .standard
    private var root = FileManager.default.temporaryDirectory
    private var configURL = FileManager.default.temporaryDirectory.appendingPathComponent("config.yml")
    private var cli = StubCLI()

    private let signedInStatus = #"{"schema_version":1,"server":"https://api.example.test","signed_in":true}"#
    private let signedOutStatus = #"{"schema_version":1,"server":"https://api.example.test","signed_in":false}"#

    override func setUpWithError() throws {
        suiteName = "mc-projsession-\(UUID().uuidString)"
        defaults = UserDefaults(suiteName: suiteName) ?? .standard
        root = FileManager.default.temporaryDirectory
            .appendingPathComponent("mc-projsession-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
        configURL = root.appendingPathComponent("memory-connector.yml")
        cli = StubCLI()
        StubURLProtocol.registry.reset()
        ConnectorKeychainCleanup.clearAll(defaults: defaults)
    }

    override func tearDownWithError() throws {
        if !suiteName.isEmpty {
            defaults.removePersistentDomain(forName: suiteName)
        }
        try? FileManager.default.removeItem(at: root)
        StubURLProtocol.registry.reset()
        ConnectorKeychainCleanup.clearAll(defaults: defaults)
    }

    @MainActor
    private func makeStore(stop: CallCounter) -> ProjectStore {
        let settings = ConnectorSettings(defaults: defaults)
        settings.serverURL = "https://api.example.test"
        settings.instanceID = "host-connector"
        settings.updateDisabledTools([])
        let store = ProjectStore(settings: settings,
                                 defaults: defaults,
                                 restart: { _, _, _ in },
                                 stopProject: { _ in },
                                 stop: { stop.increment() })
        store.connectorCLI = cli.cli
        store.cliConfigPath = configURL.path
        return store
    }

    /// Seeds a persisted connection for `p1` so the store reconstructs
    /// `connectedProjectIDs` on init, mirroring a launch with a configured
    /// project already on disk.
    private func seedConnectedProject(_ id: String = "p1") {
        ProjectProfileStore(defaults: defaults).setConnected(true, for: id)
    }

    private func useCalls() -> [[String]] {
        cli.calls.filter { Array($0.prefix(2)).joined(separator: " ") == "projects use" }
    }

    @MainActor
    func testReassertConnectionAuthFailureKeepsConnectionAndEngine() async {
        seedConnectedProject("p1")
        cli.fail("projects use", message: "not signed in — run 'memory-connector auth login' first")
        cli.on("auth status", json: signedOutStatus)
        let stop = CallCounter()
        let store = makeStore(stop: stop)
        XCTAssertEqual(store.connectedProjectIDs, ["p1"])

        let result = await store.reassertConnection()

        XCTAssertFalse(result)
        XCTAssertEqual(store.state, .signedOut)
        XCTAssertEqual(store.connectedProjectIDs, ["p1"],
                       "auth failure must keep the connected intent")
        XCTAssertTrue(store.hasConnectedProject)
        XCTAssertEqual(stop.count, 0, "auth failure must not stop the engine")
        XCTAssertEqual(useCalls().count, 1, "projects use is still attempted exactly once")
    }

    @MainActor
    func testReassertConnectionTextFallbackKeepsConnectionAndEngine() async {
        // No `auth status` stub: classification falls back to the failure text.
        seedConnectedProject("p1")
        cli.fail("projects use", message: "invalid_token")
        let stop = CallCounter()
        let store = makeStore(stop: stop)

        let result = await store.reassertConnection()

        XCTAssertFalse(result)
        XCTAssertEqual(store.state, .signedOut)
        XCTAssertEqual(store.connectedProjectIDs, ["p1"])
        XCTAssertEqual(stop.count, 0)
    }

    @MainActor
    func testReassertConnectionNonAuthFailureClearsConnectionAndStops() async {
        seedConnectedProject("p1")
        cli.fail("projects use", message: "boom")
        cli.on("auth status", json: signedInStatus)
        let stop = CallCounter()
        let store = makeStore(stop: stop)
        XCTAssertEqual(store.connectedProjectIDs, ["p1"])

        let result = await store.reassertConnection()

        XCTAssertFalse(result)
        guard case .error = store.state else { return XCTFail("expected error state") }
        XCTAssertTrue(store.connectedProjectIDs.isEmpty, "non-auth failure must clear the connection")
        XCTAssertFalse(store.hasConnectedProject)
        XCTAssertEqual(stop.count, 1, "non-auth failure must stop the engine")
    }

    @MainActor
    func testReassertConnectionAuthFailureStillAllowsEngineToRun() async throws {
        // A config binding p1 already exists on disk; after an auth failure the
        // connected intent survives and the lifecycle policy still says run.
        try EngineConfigWriter.write(configURL: configURL, content: """
        server_url: https://api.example.test
        token: emt_x
        instance_id: host-connector
        project_id: p1
        """)
        seedConnectedProject("p1")
        cli.fail("projects use", message: "not signed in")
        cli.on("auth status", json: signedOutStatus)
        let stop = CallCounter()
        let store = makeStore(stop: stop)

        _ = await store.reassertConnection()

        XCTAssertEqual(store.state, .signedOut)
        XCTAssertEqual(store.connectedProjectIDs, ["p1"])
        XCTAssertEqual(stop.count, 0)
        XCTAssertTrue(EngineLifecyclePolicy.shouldRun(connectedProjectID: "p1",
                                                      configURL: configURL),
                      "the engine still has a matching config, so it keeps running")
    }
}
