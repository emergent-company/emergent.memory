@testable import MemoryConnector
import XCTest

/// `ProjectStore.reassertConnection()` when a connected project cannot be
/// re-asserted.
///
/// A project is connected and its engine config (written by the CLI on a
/// long-lived project token) binds that project, but `projects use` failed for
/// some reason. What the failure means decides what survives: only a failure
/// that reconnecting cannot fix tears a connection (and its engine) down.
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
    private let needsReauthStatus = #"{"schema_version":1,"server":"https://api.example.test","signed_in":true,"needs_reauth":true}"#

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
    private func makeStore(stop: CallCounter,
                           stopProject: @escaping @MainActor (String) -> Void = { _ in }) -> ProjectStore
    {
        let settings = ConnectorSettings(defaults: defaults)
        settings.serverURL = "https://api.example.test"
        settings.instanceID = "host-connector"
        settings.updateDisabledTools([])
        let store = ProjectStore(settings: settings,
                                 defaults: defaults,
                                 restart: { _, _, _ in },
                                 stopProject: stopProject,
                                 stop: { stop.increment() })
        store.connectorCLI = cli.cli
        store.cliConfigPath = configURL.path
        return store
    }

    /// Seeds a persisted connection for `id` so the store reconstructs
    /// `connectedProjectIDs` on init, mirroring a launch with a configured
    /// project already on disk.
    private func seedConnectedProject(_ id: String = "p1") {
        ProjectProfileStore(defaults: defaults).setConnected(true, for: id)
    }

    private func useCalls() -> [[String]] {
        cli.calls.filter { Array($0.prefix(2)).joined(separator: " ") == "projects use" }
    }

    // MARK: - Auth: keep everything

    @MainActor
    func testReassertConnectionAuthFailureKeepsConnectionAndEngine() async {
        seedConnectedProject("p1")
        cli.fail("projects use", message: "not signed in — run 'memory-connector auth login' first")
        cli.on("auth status", json: signedOutStatus)
        let stop = CallCounter()
        let store = makeStore(stop: stop)
        XCTAssertEqual(store.connectedProjectIDs, ["p1"])

        let result = await store.reassertConnection()

        XCTAssertEqual(result, .authRequired)
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

        XCTAssertEqual(result, .authRequired)
        XCTAssertEqual(store.state, .signedOut)
        XCTAssertEqual(store.connectedProjectIDs, ["p1"])
        XCTAssertEqual(stop.count, 0)
    }

    @MainActor
    func testReassertConnectionRejectedSessionKeepsConnectionAndEngine() async {
        // A stored session the provider already rejected: `signed_in` is still
        // true, so only `needs_reauth` distinguishes it from a healthy one. A
        // code-less 400 from `projects use` must classify as an auth failure —
        // keeping the connection and the engine — not as a plain error that
        // tears both down.
        seedConnectedProject("p1")
        cli.fail("projects use", message: "refresh failed with status 400")
        cli.on("auth status", json: needsReauthStatus)
        let stop = CallCounter()
        let store = makeStore(stop: stop)

        let result = await store.reassertConnection()

        XCTAssertEqual(result, .authRequired)
        XCTAssertEqual(store.state, .signedOut)
        XCTAssertEqual(store.connectedProjectIDs, ["p1"],
                       "a rejected session is a re-auth, not a lost connection")
        XCTAssertEqual(stop.count, 0, "the engine is left alone")
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

    // MARK: - Transient: keep everything, change nothing

    @MainActor
    func testReassertConnectionTransientFailureKeepsEverything() async {
        seedConnectedProject("p1")
        cli.fail("projects use", message: "dial tcp 127.0.0.1:8090: connection refused")
        cli.on("auth status", json: signedInStatus)
        let stop = CallCounter()
        let store = makeStore(stop: stop)

        let result = await store.reassertConnection()

        XCTAssertEqual(result, .transientFailure)
        XCTAssertEqual(store.connectedProjectIDs, ["p1"], "a blip is not a lost connection")
        XCTAssertTrue(store.hasConnectedProject)
        XCTAssertEqual(store.state, .idle,
                       "nothing was lost, so the state is left for the next attempt to settle")
        XCTAssertEqual(stop.count, 0, "the engine is left alone")
    }

    // MARK: - Permanent: drop only the project that is gone

    @MainActor
    func testReassertConnectionPermanentFailureClearsConnectionAndStops() async {
        seedConnectedProject("p1")
        cli.fail("projects use", message: "[404] not_found: project not found")
        cli.on("auth status", json: signedInStatus)
        let stop = CallCounter()
        let store = makeStore(stop: stop)
        XCTAssertEqual(store.connectedProjectIDs, ["p1"])

        let result = await store.reassertConnection()

        XCTAssertEqual(result, .permanentFailure)
        guard case .error = store.state else { return XCTFail("expected error state") }
        XCTAssertTrue(store.connectedProjectIDs.isEmpty, "a permanent failure must clear the connection")
        XCTAssertFalse(store.hasConnectedProject)
        XCTAssertEqual(stop.count, 1, "losing the last connection stops the engine")
    }

    @MainActor
    func testReassertConnectionPermanentFailureDropsOnlyThatProject() async {
        seedConnectedProject("p1")
        seedConnectedProject("p2")
        cli.on("projects use") { arguments in
            // `projects use <project-id> …`: the id is the first positional.
            let project = arguments.indices.contains(2) ? arguments[2] : ""
            if project == "p2" {
                return ProcessResult(stdout: "[404] not_found: project not found", exitCode: 1, timedOut: false)
            }
            return ProcessResult(stdout: """
            {"schema_version":1,"server":"https://api.example.test",\
            "project":{"id":"\(project)","name":"Project"},"config":"/tmp/memory-connector.yml"}
            """, exitCode: 0, timedOut: false)
        }
        cli.on("auth status", json: signedInStatus)
        let stop = CallCounter()
        var stoppedProjects: [String] = []
        let store = makeStore(stop: stop, stopProject: { stoppedProjects.append($0) })

        let result = await store.reassertConnection()

        XCTAssertEqual(result, .permanentFailure)
        XCTAssertEqual(store.connectedProjectIDs, ["p1"], "the healthy project is untouched")
        XCTAssertEqual(stoppedProjects, ["p2"], "only the gone project's engine is stopped")
        XCTAssertEqual(stop.count, 0, "the surviving connection keeps running")
    }

    @MainActor
    func testStaleReauthMarkWinsOverPermanentStatus() async {
        // The documented trade: while a session needs re-auth, any failure is
        // treated as auth, so a stale session can never tear a connection down
        // (the cost is a retry that shows the sign-in affordance early).
        seedConnectedProject("p1")
        cli.fail("projects use", message: "[404] not_found: project not found")
        cli.on("auth status", json: needsReauthStatus)
        let stop = CallCounter()
        let store = makeStore(stop: stop)

        let result = await store.reassertConnection()

        XCTAssertEqual(result, .authRequired)
        XCTAssertEqual(store.state, .signedOut)
        XCTAssertEqual(store.connectedProjectIDs, ["p1"])
        XCTAssertEqual(stop.count, 0)
    }

    // MARK: - Nothing connected

    @MainActor
    func testReassertConnectionWithoutConnectionsStopsTheEngine() async {
        let stop = CallCounter()
        let store = makeStore(stop: stop)

        let result = await store.reassertConnection()

        XCTAssertEqual(result, .noneConnected)
        XCTAssertEqual(useCalls().count, 0, "there is nothing to re-assert")
        XCTAssertEqual(stop.count, 1, "no connection means no engine")
    }
}
