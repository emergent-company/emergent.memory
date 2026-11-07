@testable import MemoryConnector
import XCTest

/// The picker's read models over `ProjectStore` (`activeProject`,
/// `connectedProjects`). They live in `ProjectStore+PickerDisplay` so the
/// store's own body stays untouched — this suite is what keeps that extension
/// honest.
final class ProjectStorePickerDisplayTests: XCTestCase {
    private var suiteName = ""
    private var defaults = UserDefaults(suiteName: "") ?? .standard
    private var root = FileManager.default.temporaryDirectory
    private var configURL = FileManager.default.temporaryDirectory.appendingPathComponent("config.yml")
    private var cli = StubCLI()

    private let listJSON = #"{"schema_version":1,"server":"https://api.example.test","projects":[{"id":"p1","name":"One","active":false},{"id":"p2","name":"Two","active":true},{"id":"p3","name":"Three","active":false}]}"#

    override func setUpWithError() throws {
        suiteName = "mc-picker-display-\(UUID().uuidString)"
        defaults = UserDefaults(suiteName: suiteName) ?? .standard
        root = FileManager.default.temporaryDirectory
            .appendingPathComponent("mc-picker-display-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
        configURL = root.appendingPathComponent("memory-connector.yml")
        cli = StubCLI()
        cli.on("projects list", json: listJSON)
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
    private func makeStore() -> ProjectStore {
        let settings = ConnectorSettings(defaults: defaults)
        settings.serverURL = "https://api.example.test"
        settings.instanceID = "host-connector"
        settings.updateDisabledTools([])
        let store = ProjectStore(settings: settings,
                                 defaults: defaults,
                                 restart: { _, _, _ in },
                                 stopProject: { _ in },
                                 stop: {})
        store.connectorCLI = cli.cli
        store.cliConfigPath = configURL.path
        return store
    }

    private func scope() -> ProjectStoreScope {
        let account = Account(id: "prod:picker", environmentID: "prod",
                              email: "picker@example.test", displayName: "Picker")
        return ProjectStoreScope.account(account, environment: .prod, rootDirectory: root)
    }

    // MARK: - activeProject

    @MainActor
    func testActiveProjectResolvesTheLoadedRow() async {
        let store = makeStore()
        store.applyScope(scope())
        await store.loadProjects(accessToken: "")

        await store.selectProject("p3", accessToken: "")

        XCTAssertEqual(store.activeProject?.id, "p3")
        XCTAssertEqual(store.activeProject?.name, "Three")
    }

    @MainActor
    func testActiveProjectIsNilWhenTheListIsEmpty() async {
        cli.on("projects list", json: #"{"schema_version":1,"server":"https://api.example.test","projects":[]}"#)
        let store = makeStore()
        store.applyScope(scope())
        await store.loadProjects(accessToken: "")

        XCTAssertNil(store.activeProject)
        XCTAssertTrue(store.connectedProjects.isEmpty)
    }

    @MainActor
    func testActiveProjectAdoptsTheFirstRowWhenTheSelectionIsGone() async {
        let store = makeStore()
        let scoped = scope()
        // A selection persisted for a project that has since been deleted or is
        // no longer visible to this account: the load self-heals to the first
        // row, and the picker's read model follows it rather than naming a
        // project that is not in the list.
        defaults.set("gone", forKey: scoped.activeProjectIDKey)
        store.applyScope(scoped)
        await store.loadProjects(accessToken: "")

        XCTAssertEqual(store.activeProject?.id, "p1")
        XCTAssertEqual(store.activeProject?.name, "One")
    }

    // MARK: - connectedProjects

    @MainActor
    func testConnectedProjectsAreNameOrderedAndIgnoreUnloadedIDs() async {
        let store = makeStore()
        let scoped = scope()
        let connected = ProjectProfile(disabledTools: [], instanceID: "host-connector", connected: true)
        scoped.profileStore.save(connected, for: "p3")
        scoped.profileStore.save(connected, for: "p1")
        // Connected intent for a project the list no longer returns (deleted, or
        // another account's id): it must not surface as a row without a name.
        scoped.profileStore.save(connected, for: "gone")

        store.applyScope(scoped)
        await store.loadProjects(accessToken: "")

        XCTAssertEqual(store.connectedProjects.map(\.id), ["p1", "p3"])
        XCTAssertEqual(store.connectedProjects.map(\.name), ["One", "Three"])
    }

    @MainActor
    func testConnectedProjectsIsEmptyWhenNothingIsConnected() async {
        let store = makeStore()
        store.applyScope(scope())
        await store.loadProjects(accessToken: "")

        XCTAssertTrue(store.connectedProjects.isEmpty)
    }
}
