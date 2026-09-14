import XCTest
@testable import MemoryConnector

/// Account-scoped `ProjectStore`: profiles, active project and connected flag
/// must not cross accounts when the scope is swapped. Token minting/config
/// writing now happen in the connector CLI, so the Swift token store stays
/// empty by design.
final class ProjectStoreAccountScopeTests: XCTestCase {

    private var suiteName = ""
    private var defaults = UserDefaults(suiteName: "") ?? .standard
    private var root = FileManager.default.temporaryDirectory
    private var baseSecrets = AppSecretStore(baseDirectory: FileManager.default.temporaryDirectory)
    private var configURL = FileManager.default.temporaryDirectory.appendingPathComponent("config.yml")
    private var cli = StubCLI()

    override func setUpWithError() throws {
        suiteName = "mc-projscope-\(UUID().uuidString)"
        defaults = UserDefaults(suiteName: suiteName) ?? .standard
        root = FileManager.default.temporaryDirectory
            .appendingPathComponent("mc-projscope-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
        configURL = root.appendingPathComponent("memory-connector.yml")
        baseSecrets = AppSecretStore(baseDirectory: root.appendingPathComponent("shared", isDirectory: true))
        cli = StubCLI()
        cli.on("projects use", json: #"{"schema_version":1,"server":"https://shared.example.test","project":{"id":"p1","name":"One"},"config":"/tmp/memory-connector.yml"}"#)
        StubURLProtocol.registry.reset()
        ConnectorKeychainCleanup.clearAll(defaults: defaults)
    }

    override func tearDownWithError() throws {
        if !suiteName.isEmpty { defaults.removePersistentDomain(forName: suiteName) }
        try? FileManager.default.removeItem(at: root)
        StubURLProtocol.registry.reset()
        ConnectorKeychainCleanup.clearAll(defaults: defaults)
    }

    @MainActor
    private func makeSettings() -> ConnectorSettings {
        let settings = ConnectorSettings(defaults: defaults)
        settings.serverURL = "https://shared.example.test"
        settings.instanceID = "host-connector"
        settings.updateDisabledTools([])
        return settings
    }

    @MainActor
    private func makeStore(restart: CallCounter, stop: CallCounter) -> ProjectStore {
        let store = ProjectStore(settings: makeSettings(),
                                 defaults: defaults,
                                 restart: { restart.increment() },
                                 stop: { stop.increment() })
        store.connectorCLI = cli.cli
        store.cliConfigPath = configURL.path
        return store
    }

    private func accountA() -> Account {
        Account(id: "prod:user-A", environmentID: "prod", email: "a@example.test", displayName: "A")
    }

    private func accountB() -> Account {
        Account(id: "dev:user-B", environmentID: "dev", email: "b@example.test", displayName: "B")
    }

    private func flagValue(_ flag: String, in arguments: [String]) -> String? {
        guard let index = arguments.firstIndex(of: flag), arguments.indices.contains(index + 1) else {
            return nil
        }
        return arguments[index + 1]
    }

    private func useCalls() -> [[String]] {
        cli.calls.filter { Array($0.prefix(2)).joined(separator: " ") == "projects use" }
    }

    @MainActor
    func testAccountScopesIsolateProfilesAndSelection() async {
        let restart = CallCounter()
        let stop = CallCounter()
        let store = makeStore(restart: restart, stop: stop)

        let scopeA = ProjectStoreScope.account(accountA(), environment: .prod, rootDirectory: root)
        let scopeB = ProjectStoreScope.account(accountB(), environment: .dev, rootDirectory: root)

        store.applyScope(scopeA)
        await store.connect(projectID: "p1", accessToken: "access-A")
        XCTAssertEqual(store.connectedProjectID, "p1")
        XCTAssertEqual(store.activeProjectID, "p1")
        XCTAssertEqual(scopeA.profileStore.profile(for: "p1")?.connected, true)

        // Switching to B stops the engine and shows nothing from A.
        let stopBefore = stop.count
        store.applyScope(scopeB)
        XCTAssertEqual(stop.count, stopBefore + 1, "scope swap stops the engine")
        XCTAssertNil(store.connectedProjectID)
        XCTAssertNil(store.activeProjectID)
        XCTAssertNil(scopeB.profileStore.profile(for: "p1"))
        XCTAssertEqual(scopeB.profileStore.connectedProjectID(), nil)

        // Switching back restores A's persisted state from disk.
        store.applyScope(scopeA)
        XCTAssertEqual(store.connectedProjectID, "p1")
        XCTAssertEqual(store.activeProjectID, "p1")
    }

    @MainActor
    func testConnectUsesAccountEnvironmentForCLI() async throws {
        let store = makeStore(restart: CallCounter(), stop: CallCounter())
        store.applyScope(ProjectStoreScope.account(accountA(), environment: .prod, rootDirectory: root))

        await store.connect(projectID: "p1", accessToken: "access-A")

        let use = try XCTUnwrap(useCalls().last)
        XCTAssertEqual(flagValue("--server", in: use), Environment.prod.serverURLString)
        XCTAssertEqual(flagValue("--instance-id", in: use), "host-connector")
        XCTAssertEqual(flagValue("--config", in: use), configURL.path)
    }

    @MainActor
    func testSharedScopeIsUnaffectedByAccountConnect() async {
        let store = makeStore(restart: CallCounter(), stop: CallCounter())

        // Connect inside an account, then return to the shared scope.
        store.applyScope(ProjectStoreScope.account(accountA(), environment: .prod, rootDirectory: root))
        await store.connect(projectID: "p1", accessToken: "access-A")

        store.applyScope(ProjectStoreScope.shared(defaults: defaults, secrets: baseSecrets))
        XCTAssertNil(store.connectedProjectID)
        XCTAssertNil(store.activeProjectID)
        XCTAssertNil(ProjectProfileStore(defaults: defaults).profile(for: "p1"),
                     "shared profiles stay empty")
    }
}
