import XCTest
@testable import MemoryConnector

/// Locked counter for restart/stop closures.
final class CallCounter: @unchecked Sendable {
    private let lock = NSLock()
    private var _count = 0

    func increment() {
        lock.lock(); _count += 1; lock.unlock()
    }

    var count: Int {
        lock.lock(); defer { lock.unlock() }
        return _count
    }
}

final class ProjectStoreTests: XCTestCase {

    private var settingsSuite = ""
    private var storeSuite = ""
    private var settingsDefaults = UserDefaults(suiteName: "") ?? .standard
    private var storeDefaults = UserDefaults(suiteName: "") ?? .standard
    private var tempRoot = FileManager.default.temporaryDirectory
    private var configURL = FileManager.default.temporaryDirectory.appendingPathComponent("config.yml")
    private var cli = StubCLI()

    private let listJSON = #"{"schema_version":1,"server":"https://api.example.test","projects":[{"id":"p1","name":"One","active":true},{"id":"p2","name":"Two","active":false}]}"#
    private let useJSON = #"{"schema_version":1,"server":"https://api.example.test","project":{"id":"p1","name":"One"},"config":"/tmp/memory-connector.yml"}"#

    override func setUpWithError() throws {
        settingsSuite = "mc-projectstore-settings-\(UUID().uuidString)"
        storeSuite = "mc-projectstore-store-\(UUID().uuidString)"
        settingsDefaults = UserDefaults(suiteName: settingsSuite) ?? .standard
        storeDefaults = UserDefaults(suiteName: storeSuite) ?? .standard
        tempRoot = FileManager.default.temporaryDirectory
            .appendingPathComponent("mc-projectstore-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: tempRoot, withIntermediateDirectories: true)
        configURL = tempRoot.appendingPathComponent("memory-connector.yml")
        cli = StubCLI()
        StubURLProtocol.registry.reset()
        ConnectorKeychainCleanup.clearAll(defaults: settingsDefaults)
        ConnectorKeychainCleanup.clearAll(defaults: storeDefaults)
    }

    override func tearDownWithError() throws {
        settingsDefaults.removePersistentDomain(forName: settingsSuite)
        storeDefaults.removePersistentDomain(forName: storeSuite)
        try? FileManager.default.removeItem(at: tempRoot)
        StubURLProtocol.registry.reset()
        ConnectorKeychainCleanup.clearAll(defaults: settingsDefaults)
        ConnectorKeychainCleanup.clearAll(defaults: storeDefaults)
    }

    @MainActor
    private func makeSettings(instanceID: String = "host-connector",
                              disabled: Set<String> = []) -> ConnectorSettings {
        let settings = ConnectorSettings(defaults: settingsDefaults)
        settings.serverURL = "https://api.example.test"
        settings.instanceID = instanceID
        settings.updateDisabledTools(disabled)
        return settings
    }

    @MainActor
    private func makeStore(settings: ConnectorSettings? = nil,
                           restart: CallCounter = CallCounter(),
                           stop: CallCounter = CallCounter()) -> ProjectStore {
        let store = ProjectStore(settings: settings ?? makeSettings(),
                                 defaults: storeDefaults,
                                 restart: { restart.increment() },
                                 stop: { stop.increment() })
        store.connectorCLI = cli.cli
        store.cliConfigPath = configURL.path
        return store
    }

    // MARK: - CLI call helpers

    private func useCalls() -> [[String]] {
        cli.calls.filter { Array($0.prefix(2)).joined(separator: " ") == "projects use" }
    }

    private func flagValue(_ flag: String, in arguments: [String]) -> String? {
        guard let index = arguments.firstIndex(of: flag), arguments.indices.contains(index + 1) else {
            return nil
        }
        return arguments[index + 1]
    }

    private func stubConnectSuccess(_ json: String? = nil) {
        cli.on("projects use", json: json ?? useJSON)
    }

    // MARK: - Loading

    @MainActor
    func testInitialStateIsInert() {
        let store = makeStore()
        XCTAssertEqual(store.state, .idle)
        XCTAssertTrue(store.projects.isEmpty)
        XCTAssertNil(store.activeProjectID)
        XCTAssertNil(store.connectedProjectID)
        XCTAssertFalse(store.hasConnectedProject)
        XCTAssertTrue(cli.calls.isEmpty, "no CLI command runs until driven")
    }

    @MainActor
    func testLoadProjectsSelectsFirstWhenNonePersisted() async {
        cli.on("projects list", json: listJSON)
        let store = makeStore()

        await store.loadProjects(accessToken: "user-access")

        XCTAssertEqual(store.state, .loaded)
        XCTAssertEqual(store.projects.map(\.id), ["p1", "p2"])
        XCTAssertEqual(store.projects.map(\.name), ["One", "Two"])
        XCTAssertEqual(store.activeProjectID, "p1")
        XCTAssertEqual(storeDefaults.string(forKey: ProjectStore.activeProjectIDKey), "p1")
        XCTAssertTrue(cli.called("projects list"))
        let call = cli.calls.first { Array($0.prefix(2)).joined(separator: " ") == "projects list" }
        XCTAssertEqual(flagValue("--server", in: call ?? []), "https://api.example.test")
        XCTAssertEqual(flagValue("--config", in: call ?? []), configURL.path)
    }

    @MainActor
    func testLoadProjectsKeepsPersistedSelection() async {
        storeDefaults.set("p2", forKey: ProjectStore.activeProjectIDKey)
        cli.on("projects list", json: listJSON)
        let store = makeStore()

        await store.loadProjects(accessToken: "user-access")

        XCTAssertEqual(store.activeProjectID, "p2")
    }

    @MainActor
    func testLoadProjectsFailureKeepsSelection() async {
        storeDefaults.set("p2", forKey: ProjectStore.activeProjectIDKey)
        cli.fail("projects list", message: "boom")
        let store = makeStore()

        await store.loadProjects(accessToken: "user-access")

        guard case .error = store.state else { return XCTFail("expected error state") }
        XCTAssertEqual(store.activeProjectID, "p2")
    }

    // MARK: - Auth failure → signed out

    private let signedInStatus = #"{"schema_version":1,"server":"https://api.example.test","signed_in":true}"#
    private let signedOutStatus = #"{"schema_version":1,"server":"https://api.example.test","signed_in":false}"#

    @MainActor
    func testLoadProjectsNotSignedInTextSurfacesSignedOut() async {
        cli.fail("projects list", message: "not signed in — run 'memory-connector auth login' first")
        let store = makeStore()

        await store.loadProjects(accessToken: "user-access")

        XCTAssertEqual(store.state, .signedOut)
    }

    @MainActor
    func testLoadProjectsCLIStatusUnsignedSurfacesSignedOut() async {
        cli.fail("projects list", message: "boom")
        cli.on("auth status", json: signedOutStatus)
        let store = makeStore()

        await store.loadProjects(accessToken: "user-access")

        XCTAssertEqual(store.state, .signedOut)
    }

    @MainActor
    func testLoadProjectsUnauthorizedSurfacesSignedOut() async {
        cli.fail("projects list", message: "401 Unauthorized")
        cli.on("auth status", json: signedInStatus)
        let store = makeStore()

        await store.loadProjects(accessToken: "user-access")

        XCTAssertEqual(store.state, .signedOut)
    }

    @MainActor
    func testLoadProjectsInvalidTokenSurfacesSignedOut() async {
        cli.fail("projects list", message: "invalid_token")
        cli.on("auth status", json: signedInStatus)
        let store = makeStore()

        await store.loadProjects(accessToken: "user-access")

        XCTAssertEqual(store.state, .signedOut)
    }

    @MainActor
    func testLoadProjectsNonAuthFailureShowsError() async {
        cli.fail("projects list", message: "boom")
        cli.on("auth status", json: signedInStatus)
        let store = makeStore()

        await store.loadProjects(accessToken: "user-access")

        guard case .error = store.state else { return XCTFail("expected error state") }
    }

    @MainActor
    func testLoadProjectsAuthFailureRepairFailsShowsSignedOut() async {
        cli.fail("projects list", message: "not signed in")
        let repairs = CallCounter()
        let store = makeStore()
        store.repairConnectorSession = {
            repairs.increment()
            return false
        }

        await store.loadProjects(accessToken: "user-access")

        XCTAssertEqual(store.state, .signedOut)
        XCTAssertEqual(repairs.count, 1, "repair is attempted exactly once")
    }

    @MainActor
    func testLoadProjectsAuthFailureRetriesOnceAfterRepair() async {
        let okJSON = listJSON
        let listCalls = CallCounter()
        cli.on("projects list") { _ in
            listCalls.increment()
            if listCalls.count == 1 {
                return ProcessResult(stdout: "not signed in", exitCode: 1, timedOut: false)
            }
            return ProcessResult(stdout: okJSON, exitCode: 0, timedOut: false)
        }
        let repairs = CallCounter()
        let store = makeStore()
        store.repairConnectorSession = {
            repairs.increment()
            return true
        }

        await store.loadProjects(accessToken: "user-access")

        XCTAssertEqual(store.state, .loaded)
        XCTAssertEqual(store.projects.map(\.id), ["p1", "p2"])
        XCTAssertEqual(repairs.count, 1)
        XCTAssertEqual(listCalls.count, 2, "the list is retried exactly once after a repair")
    }

    // MARK: - Connect / disconnect (explicit user decision)

    @MainActor
    func testConnectUsesCLIAndRestarts() async {
        let restart = CallCounter()
        let store = makeStore(restart: restart)
        stubConnectSuccess()

        await store.connect(projectID: "p1", accessToken: "user-access")

        XCTAssertEqual(store.state, .loaded)
        XCTAssertEqual(store.connectedProjectID, "p1")
        XCTAssertEqual(store.activeProjectID, "p1")
        XCTAssertTrue(store.hasConnectedProject)
        XCTAssertTrue(store.isConnected("p1"))
        XCTAssertEqual(restart.count, 1)
        XCTAssertEqual(StubURLProtocol.registry.capturedRequest, nil, "token minting moved to the CLI")

        let uses = useCalls()
        XCTAssertEqual(uses.count, 1)
        XCTAssertEqual(uses.first.map { Array($0.prefix(3)) }, ["projects", "use", "p1"])
        XCTAssertEqual(flagValue("--server", in: uses.first ?? []), "https://api.example.test")
        XCTAssertEqual(flagValue("--instance-id", in: uses.first ?? []), "host-connector")
        XCTAssertEqual(flagValue("--disabled-tools", in: uses.first ?? []), "")

        let profile = ProjectProfileStore(defaults: storeDefaults).profile(for: "p1")
        XCTAssertEqual(profile?.connected, true, "connection is persisted on the profile")
    }

    @MainActor
    func testConnectNeverMintsTokenInSwift() async {
        // The CLI reuses its own stored token; ProjectStore must not touch the
        // Swift token-mint endpoint at all.
        let restart = CallCounter()
        let store = makeStore(restart: restart)
        stubConnectSuccess()

        await store.connect(projectID: "p1", accessToken: "user-access")

        XCTAssertNil(StubURLProtocol.registry.capturedRequest)
        XCTAssertEqual(restart.count, 1)
        XCTAssertEqual(store.connectedProjectID, "p1")
        XCTAssertTrue(cli.called("projects use"))
    }

    @MainActor
    func testConnectFailureLeavesConnectionUnchanged() async {
        storeDefaults.set("p0", forKey: ProjectStore.activeProjectIDKey)
        let restart = CallCounter()
        let store = makeStore(restart: restart)
        cli.fail("projects use", message: "boom")

        await store.connect(projectID: "p1", accessToken: "user-access")

        guard case .error = store.state else { return XCTFail("expected error state") }
        XCTAssertNil(store.connectedProjectID)
        XCTAssertEqual(store.activeProjectID, "p0", "failed connect must not change selection")
        XCTAssertEqual(restart.count, 0)
        XCTAssertNotEqual(ProjectProfileStore(defaults: storeDefaults).profile(for: "p1")?.connected, true)
    }

    @MainActor
    func testConnectFailureKeepsPreviousConnection() async {
        let restart = CallCounter()
        let store = makeStore(restart: restart)
        let okJSON = useJSON
        cli.on("projects use") { arguments in
            if arguments.contains("p1") {
                return ProcessResult(stdout: okJSON, exitCode: 0, timedOut: false)
            }
            return ProcessResult(stdout: "boom", exitCode: 1, timedOut: false)
        }

        await store.connect(projectID: "p1", accessToken: "user-access")
        XCTAssertEqual(store.connectedProjectID, "p1")

        await store.connect(projectID: "p2", accessToken: "user-access")

        guard case .error = store.state else { return XCTFail("expected error state") }
        XCTAssertEqual(store.connectedProjectID, "p1", "failed connect must not drop the current connection")
        XCTAssertEqual(restart.count, 1)
        XCTAssertEqual(ProjectProfileStore(defaults: storeDefaults).profile(for: "p1")?.connected, true)
    }

    @MainActor
    func testConnectSecondProjectDisconnectsFirst() async {
        let restart = CallCounter()
        let store = makeStore(restart: restart)
        stubConnectSuccess()

        await store.connect(projectID: "p1", accessToken: "user-access")
        await store.connect(projectID: "p2", accessToken: "user-access")

        XCTAssertEqual(store.connectedProjectID, "p2", "exactly one project is connected")
        XCTAssertEqual(restart.count, 2)
        let profiles = ProjectProfileStore(defaults: storeDefaults)
        XCTAssertEqual(profiles.profile(for: "p1")?.connected, false, "A is flipped to disconnected")
        XCTAssertEqual(profiles.profile(for: "p2")?.connected, true)
        XCTAssertEqual(profiles.connectedProjectID(), "p2")

        let lastUse = useCalls().last ?? []
        XCTAssertEqual(flagValue("--config", in: lastUse), configURL.path)
        XCTAssertTrue(lastUse.contains("p2"))
    }

    @MainActor
    func testDisconnectStopsEngineAndPersists() async {
        let restart = CallCounter()
        let stop = CallCounter()
        let store = makeStore(restart: restart, stop: stop)
        stubConnectSuccess()

        await store.connect(projectID: "p1", accessToken: "user-access")
        XCTAssertEqual(restart.count, 1)

        store.disconnect()

        XCTAssertEqual(stop.count, 1, "disconnect stops the engine")
        XCTAssertNil(store.connectedProjectID)
        XCTAssertFalse(store.hasConnectedProject)
        let profile = ProjectProfileStore(defaults: storeDefaults).profile(for: "p1")
        XCTAssertEqual(profile?.connected, false, "disconnect is persisted")
        XCTAssertFalse(EngineLifecyclePolicy.shouldRun(connectedProjectID: store.connectedProjectID,
                                                       configURL: configURL))
    }

    @MainActor
    func testConnectedProjectPersistsAcrossStores() async {
        let first = makeStore()
        stubConnectSuccess()
        await first.connect(projectID: "p1", accessToken: "user-access")
        XCTAssertEqual(first.connectedProjectID, "p1")

        let second = makeStore()
        XCTAssertEqual(second.connectedProjectID, "p1", "connection state is reconstructed from profiles")
    }

    // MARK: - Selection (view / configure — never connects)

    @MainActor
    func testSelectNonConnectedDoesNotUseCLIOrRestart() async {
        let restart = CallCounter()
        let stop = CallCounter()
        let store = makeStore(restart: restart, stop: stop)

        await store.selectProject("p1", accessToken: "user-access")

        XCTAssertEqual(store.activeProjectID, "p1")
        XCTAssertNil(store.connectedProjectID, "select must not implicitly connect")
        XCTAssertTrue(useCalls().isEmpty, "no projects use for a non-connected project")
        XCTAssertEqual(restart.count, 0)
        XCTAssertEqual(stop.count, 1, "nothing connected: engine stopped, no lingering connection")
        XCTAssertNotNil(ProjectProfileStore(defaults: storeDefaults).profile(for: "p1"),
                        "profile still applied/seeded for editing")
    }

    @MainActor
    func testSelectNonConnectedKeepsOtherConnectedEngineRunning() async {
        let restart = CallCounter()
        let stop = CallCounter()
        let store = makeStore(restart: restart, stop: stop)
        stubConnectSuccess()

        await store.connect(projectID: "p1", accessToken: "user-access")
        XCTAssertEqual(restart.count, 1)

        await store.selectProject("p2", accessToken: "user-access")

        XCTAssertEqual(store.connectedProjectID, "p1", "viewing another project does not disconnect")
        XCTAssertEqual(restart.count, 1)
        XCTAssertEqual(stop.count, 0)
        XCTAssertEqual(useCalls().count, 1, "select of a non-connected project makes no CLI call")
    }

    @MainActor
    func testSelectConnectedProjectRewritesConfigAndRestarts() async {
        let restart = CallCounter()
        let store = makeStore(restart: restart)
        stubConnectSuccess()

        await store.connect(projectID: "p1", accessToken: "user-access")
        XCTAssertEqual(restart.count, 1)

        await store.saveActiveProfile(disabledTools: ["notes_create"], instanceID: "host-connector")
        XCTAssertEqual(restart.count, 2, "editing the connected project's tools rewrites + restarts")

        await store.selectProject("p1", accessToken: "user-access")
        XCTAssertEqual(restart.count, 3, "selecting the connected project refreshes it")

        XCTAssertEqual(useCalls().count, 3)
        let editCall = useCalls()[1]
        XCTAssertEqual(flagValue("--disabled-tools", in: editCall), "notes_create")
    }

    // MARK: - All tools in one call

    @MainActor
    func testSetAllToolsDisablesWholeCatalog() async {
        let restart = CallCounter()
        let store = makeStore(restart: restart)
        stubConnectSuccess()

        await store.connect(projectID: "p1", accessToken: "user-access")
        var profile = await store.setAllTools(enabled: true, projectID: "p1")
        XCTAssertEqual(profile.disabledTools, [], "enable all: empty disabled set")
        XCTAssertEqual(restart.count, 2, "connected project is rewritten")

        profile = await store.setAllTools(enabled: false, projectID: "p1")
        XCTAssertEqual(Set(profile.disabledTools), Set(ToolCatalog.tools.map(\.id)))
        XCTAssertEqual(restart.count, 3)

        let stored = ProjectProfileStore(defaults: storeDefaults).profile(for: "p1")
        XCTAssertEqual(Set(stored?.disabledTools ?? []), Set(ToolCatalog.tools.map(\.id)))
        let lastUse = useCalls().last ?? []
        let disabled = Set((flagValue("--disabled-tools", in: lastUse) ?? "")
            .split(separator: ",").map(String.init))
        XCTAssertEqual(disabled, Set(ToolCatalog.tools.map(\.id)))
    }

    @MainActor
    func testSetAllToolsOnNonConnectedProjectDoesNotTouchEngine() async {
        let restart = CallCounter()
        let stop = CallCounter()
        let store = makeStore(restart: restart, stop: stop)

        let profile = await store.setAllTools(enabled: true, projectID: "p1")

        XCTAssertEqual(profile.disabledTools, [])
        XCTAssertEqual(restart.count, 0)
        XCTAssertEqual(stop.count, 0)
        XCTAssertTrue(useCalls().isEmpty)
        XCTAssertEqual(ProjectProfileStore(defaults: storeDefaults).profile(for: "p1")?.disabledTools, [],
                       "still persisted for later")
    }

    @MainActor
    func testSaveActiveProfileOnNonConnectedProjectPersistsWithoutEngine() async {
        storeDefaults.set("p1", forKey: ProjectStore.activeProjectIDKey)
        let restart = CallCounter()
        let stop = CallCounter()
        let store = makeStore(restart: restart, stop: stop)
        XCTAssertEqual(store.activeProjectID, "p1")
        XCTAssertNil(store.connectedProjectID)

        await store.saveActiveProfile(disabledTools: ["notes_create"], instanceID: "p1-instance")

        XCTAssertEqual(restart.count, 0, "non-connected project edits are not pushed to the engine")
        XCTAssertEqual(stop.count, 0)
        XCTAssertTrue(useCalls().isEmpty)
        let profile = ProjectProfileStore(defaults: storeDefaults).profile(for: "p1")
        XCTAssertEqual(profile?.disabledTools, ["notes_create"])
        XCTAssertEqual(profile?.connected, false, "editing must not connect")
    }

    // MARK: - Sign-out teardown

    @MainActor
    func testStopAndClearClearsSelection() throws {
        storeDefaults.set("p1", forKey: ProjectStore.activeProjectIDKey)
        ProjectProfileStore(defaults: storeDefaults).setConnected(true, for: "p1")

        let stop = CallCounter()
        let store = makeStore(stop: stop)
        XCTAssertEqual(store.activeProjectID, "p1")
        XCTAssertEqual(store.connectedProjectID, "p1")

        store.stopAndClear()

        XCTAssertEqual(stop.count, 1)
        XCTAssertNil(store.activeProjectID)
        XCTAssertNil(store.connectedProjectID)
        XCTAssertNil(storeDefaults.string(forKey: ProjectStore.activeProjectIDKey))
        XCTAssertEqual(ProjectProfileStore(defaults: storeDefaults).profile(for: "p1")?.connected, false)
        XCTAssertEqual(store.state, .idle)
    }

    // MARK: - 9.2 per-project profiles

    @MainActor
    func testConnectSeedsProfileFromSharedOnFirstConnect() async {
        let settings = makeSettings(instanceID: "shared-host-connector", disabled: ["notes_create"])
        let store = makeStore(settings: settings)
        stubConnectSuccess()

        await store.connect(projectID: "p1", accessToken: "user-access")

        // Seeded profile captured the shared starting values.
        let profile = ProjectProfileStore(defaults: storeDefaults).profile(for: "p1")
        XCTAssertEqual(profile?.instanceID, "shared-host-connector")
        XCTAssertEqual(profile?.disabledTools, ["notes_create"])
        XCTAssertEqual(profile?.connected, true)

        let use = useCalls().first ?? []
        XCTAssertEqual(flagValue("--instance-id", in: use), "shared-host-connector")
        XCTAssertEqual(flagValue("--disabled-tools", in: use), "notes_create")
    }

    @MainActor
    func testConnectWithZeroToolsEnabledIsAllowed() async {
        // Never-configured shared settings: all tools OFF. Connecting is still
        // allowed (node may report 0 tools while the user tunes them).
        let settings = ConnectorSettings(defaults: settingsDefaults)
        settings.serverURL = "https://api.example.test"
        settings.instanceID = "host-connector"
        XCTAssertEqual(settings.disabledTools, Set(ToolCatalog.tools.map(\.id)))

        let store = makeStore(settings: settings)
        stubConnectSuccess()

        await store.connect(projectID: "p1", accessToken: "user-access")

        XCTAssertEqual(store.connectedProjectID, "p1")
        let profile = ProjectProfileStore(defaults: storeDefaults).profile(for: "p1")
        XCTAssertEqual(Set(profile?.disabledTools ?? []), Set(ToolCatalog.tools.map(\.id)),
                       "new project must start with every tool disabled")
        let use = useCalls().first ?? []
        // The seeded profile sorts the disabled set, so compare as a set.
        let disabled = Set((flagValue("--disabled-tools", in: use) ?? "")
            .split(separator: ",").map(String.init))
        XCTAssertEqual(disabled, Set(ToolCatalog.tools.map(\.id)))
    }

    @MainActor
    func testConnectReappliesEachProjectsOwnProfile() async {
        let profiles = ProjectProfileStore(defaults: storeDefaults)
        profiles.save(ProjectProfile(disabledTools: [], instanceID: "p1-connector"), for: "p1")
        profiles.save(ProjectProfile(disabledTools: ["notes_create"], instanceID: "p2-connector"), for: "p2")

        let store = makeStore()
        stubConnectSuccess()

        await store.connect(projectID: "p1", accessToken: "user-access")
        await store.connect(projectID: "p2", accessToken: "user-access")
        await store.connect(projectID: "p1", accessToken: "user-access")

        let uses = useCalls()
        XCTAssertEqual(uses.count, 3)
        XCTAssertEqual(flagValue("--instance-id", in: uses[0]), "p1-connector")
        XCTAssertEqual(flagValue("--disabled-tools", in: uses[0]), "")
        XCTAssertEqual(flagValue("--instance-id", in: uses[1]), "p2-connector")
        XCTAssertEqual(flagValue("--disabled-tools", in: uses[1]), "notes_create")
        XCTAssertEqual(flagValue("--instance-id", in: uses[2]), "p1-connector")
        XCTAssertEqual(store.connectedProjectID, "p1")
    }

    @MainActor
    func testSaveActiveProfilePersistsAndRewritesConfig() async {
        let settings = makeSettings(instanceID: "shared-host-connector")
        let restart = CallCounter()
        let store = makeStore(settings: settings, restart: restart)
        stubConnectSuccess()
        await store.connect(projectID: "p1", accessToken: "user-access")
        XCTAssertEqual(restart.count, 1)

        await store.saveActiveProfile(disabledTools: ["reminders_list"], instanceID: "edited-instance")

        XCTAssertEqual(restart.count, 2)
        let profile = ProjectProfileStore(defaults: storeDefaults).profile(for: "p1")
        XCTAssertEqual(profile?.instanceID, "edited-instance")
        XCTAssertEqual(profile?.disabledTools, ["reminders_list"])

        let editCall = useCalls().last ?? []
        XCTAssertEqual(flagValue("--instance-id", in: editCall), "edited-instance")
        XCTAssertEqual(flagValue("--disabled-tools", in: editCall), "reminders_list")
    }

    @MainActor
    func testSaveActiveProfileWithoutProjectUpdatesSharedDefaults() async throws {
        let settings = makeSettings(instanceID: "old-shared")
        let restart = CallCounter()
        let store = makeStore(settings: settings, restart: restart)
        XCTAssertNil(store.activeProjectID)

        await store.saveActiveProfile(disabledTools: ["notes_create"], instanceID: "new-shared")

        XCTAssertEqual(restart.count, 0, "no active project => nothing is pushed to the engine")
        XCTAssertEqual(settings.instanceID, "new-shared")
        XCTAssertEqual(settings.disabledTools, ["notes_create"])
        XCTAssertTrue(useCalls().isEmpty, "shared path must not use the CLI")
        XCTAssertFalse(FileManager.default.fileExists(atPath: configURL.path),
                       "no project-bound config is written without a project")
    }

    // MARK: - 9.3/9.4 active-profile accessors for the UI

    @MainActor
    func testActiveProfileAccessorsFallBackToSharedWithoutProject() {
        let settings = makeSettings(instanceID: "host-connector", disabled: ["notes_create"])
        let store = makeStore(settings: settings)

        XCTAssertFalse(store.hasActiveProject)
        XCTAssertNil(store.activeProfile)
        XCTAssertEqual(store.activeDisabledTools, ["notes_create"])
        XCTAssertEqual(store.activeInstanceID, "host-connector")
    }

    @MainActor
    func testActiveProfileAccessorsReadStoredProfile() async {
        let settings = makeSettings(instanceID: "shared-host-connector", disabled: ["notes_create"])
        let store = makeStore(settings: settings)
        stubConnectSuccess()

        await store.connect(projectID: "p1", accessToken: "user-access")

        XCTAssertTrue(store.hasActiveProject)
        XCTAssertEqual(store.activeProjectID, "p1")
        // First switch seeded the profile from the shared values.
        XCTAssertEqual(store.activeDisabledTools, ["notes_create"])
        XCTAssertEqual(store.activeInstanceID, "shared-host-connector")

        // Rewriting the active profile is reflected by the accessors.
        await store.saveActiveProfile(disabledTools: ["reminders_list"], instanceID: "p1-instance")
        XCTAssertEqual(store.activeDisabledTools, ["reminders_list"])
        XCTAssertEqual(store.activeInstanceID, "p1-instance")
    }

    // MARK: - Organisation grouping

    // Projects with a mix of organisation ids (resolved and unknown) and one
    // without an org id at all.
    private let orgProjectsJSON = #"{"schema_version":1,"server":"https://api.example.test","projects":[{"id":"p1","name":"One","org_id":"org-1","active":false},{"id":"p2","name":"Two","org_id":"org-1","active":false},{"id":"p3","name":"Zed","org_id":"org-9","active":false},{"id":"p4","name":"Four","active":false}]}"#

    @MainActor
    func testLoadProjectsMapsOrgIDAndGroupsByResolvedOrgName() async {
        cli.on("projects list", json: orgProjectsJSON)
        let store = makeStore()
        // org-2 is returned by the resolver but no loaded project references it.
        store.organizationNameLoader = { ["org-1": "Acme", "org-2": "Beta"] }

        await store.loadProjects(accessToken: "user-access")

        XCTAssertEqual(store.state, .loaded)
        let orgIDs: [String?] = store.projects.map(\.orgID)
        XCTAssertEqual(orgIDs, ["org-1", "org-1", "org-9", nil])
        // Only orgs a loaded project references are registered.
        XCTAssertEqual(store.organizationNames, ["org-1": "Acme"])
        XCTAssertEqual(store.organizationName(for: store.projects[0]), "Acme")

        let groups = store.projectsByOrganization
        XCTAssertEqual(groups.map(\.sectionTitle), ["Acme", "Other"])
        XCTAssertEqual(groups[0].projects.map(\.id), ["p1", "p2"])
        XCTAssertEqual(groups[0].projects.map(\.name), ["One", "Two"])
        XCTAssertEqual(groups[1].projects.map(\.id), ["p4", "p3"])
    }

    @MainActor
    func testLoadProjectsUnknownOrgFallsBackToOtherWithoutFailing() async {
        cli.on("projects list", json: orgProjectsJSON)
        let store = makeStore()
        // Resolver unavailable (e.g. GET /api/orgs failed) — load still succeeds.
        store.organizationNameLoader = { [:] }

        await store.loadProjects(accessToken: "user-access")

        XCTAssertEqual(store.state, .loaded)
        XCTAssertTrue(store.organizationNames.isEmpty)
        XCTAssertTrue(store.projects.allSatisfy { store.organizationName(for: $0) == nil })

        let groups = store.projectsByOrganization
        XCTAssertEqual(groups.count, 1)
        XCTAssertEqual(groups[0].sectionTitle, "Other")
        XCTAssertEqual(groups[0].projects.map(\.id), ["p4", "p1", "p2", "p3"])
    }

    // The CLI's project rows may omit an organisation id, so every project
    // groups under "Other" (sorted by name) instead of breaking grouping.
    private let noOrgProjectsJSON = #"{"schema_version":1,"server":"https://api.example.test","projects":[{"id":"p1","name":"One","active":false},{"id":"p2","name":"Two","active":false},{"id":"p3","name":"Zed","active":false},{"id":"p4","name":"Four","active":false}]}"#

    @MainActor
    func testProjectsWithoutOrgInfoGroupUnderOtherSortedByName() async {
        cli.on("projects list", json: noOrgProjectsJSON)
        let store = makeStore()

        await store.loadProjects(accessToken: "user-access")

        XCTAssertEqual(store.state, .loaded)
        XCTAssertEqual(store.projects.count, 4)
        XCTAssertTrue(store.organizationNames.isEmpty)
        XCTAssertTrue(store.projects.allSatisfy { store.organizationName(for: $0) == nil })

        let groups = store.projectsByOrganization
        XCTAssertEqual(groups.count, 1)
        XCTAssertNil(groups[0].organization)
        XCTAssertEqual(groups[0].sectionTitle, "Other")
        XCTAssertEqual(groups[0].projects.map(\.name), ["Four", "One", "Two", "Zed"])
        XCTAssertEqual(groups[0].projects.map(\.id), ["p4", "p1", "p2", "p3"])
    }
}
