import XCTest
@testable import MemoryConnector

/// New default: all local MCP tools are OFF until the user enables some, per
/// project or shared. Stored data must never be migrated against the user.
final class ConnectorSettingsToolDefaultsTests: XCTestCase {

    private var suiteName = ""
    private var defaults: UserDefaults!
    private var tempRoot = FileManager.default.temporaryDirectory

    override func setUpWithError() throws {
        suiteName = "mc-tooldefaults-\(UUID().uuidString)"
        defaults = UserDefaults(suiteName: suiteName)!
        tempRoot = FileManager.default.temporaryDirectory
            .appendingPathComponent("mc-tooldefaults-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: tempRoot, withIntermediateDirectories: true)
        ConnectorKeychainCleanup.clearAll(defaults: defaults)
    }

    override func tearDownWithError() throws {
        try? FileManager.default.removeItem(at: tempRoot)
        defaults.removePersistentDomain(forName: suiteName)
        ConnectorKeychainCleanup.clearAll(defaults: defaults)
    }

    private var catalogIDs: Set<String> { Set(ToolCatalog.tools.map(\.id)) }

    private func writeConfig(_ content: String) throws -> URL {
        let url = tempRoot.appendingPathComponent("config.yml")
        try EngineConfigWriter.write(configURL: url, content: content)
        return url
    }

    // MARK: - Fresh default

    @MainActor
    func testFreshSettingsDisableEveryCatalogTool() {
        let settings = ConnectorSettings(defaults: defaults)

        XCTAssertEqual(settings.disabledTools, catalogIDs)
        for tool in ToolCatalog.tools {
            XCTAssertFalse(settings.isToolEnabled(tool.id), "\(tool.id) must default OFF")
        }
    }

    // MARK: - Stored data is never migrated

    @MainActor
    func testStoredDisabledSetWithoutMarkerIsPreserved() {
        // An install that wrote the list before the marker existed.
        defaults.set(["notes_create"], forKey: "connector.disabledTools")

        let settings = ConnectorSettings(defaults: defaults)
        XCTAssertEqual(settings.disabledTools, ["notes_create"])
    }

    @MainActor
    func testStoredEmptySetWithMarkerStaysAllEnabled() {
        // User-intended "everything ON" is respected, not overwritten by the
        // new all-OFF default.
        defaults.set([], forKey: "connector.disabledTools")
        defaults.set(true, forKey: "connector.disabledToolsInitialized")

        let settings = ConnectorSettings(defaults: defaults)
        XCTAssertEqual(settings.disabledTools, [])
        XCTAssertTrue(ToolCatalog.tools.allSatisfy { settings.isToolEnabled($0.id) })
    }

    // MARK: - Enabling persists only that change

    @MainActor
    func testEnablingOneToolRemovesOnlyThatToolAndPersists() {
        let settings = ConnectorSettings(defaults: defaults)
        settings.setToolEnabled("notes_search", enabled: true)

        var expected = catalogIDs
        expected.remove("notes_search")
        XCTAssertEqual(settings.disabledTools, expected)
        XCTAssertTrue(settings.isToolEnabled("notes_search"))
        XCTAssertFalse(settings.isToolEnabled("notes_create"))

        // Marker written, so the user-intended set survives a reload.
        XCTAssertEqual(defaults.object(forKey: "connector.disabledToolsInitialized") as? Bool, true)
        let reloaded = ConnectorSettings(defaults: defaults)
        XCTAssertEqual(reloaded.disabledTools, expected)
    }

    @MainActor
    func testClearConfigurationReturnsToAllDisabled() {
        let settings = ConnectorSettings(defaults: defaults)
        settings.setToolEnabled("notes_search", enabled: true)

        settings.clearConfiguration()

        XCTAssertEqual(settings.disabledTools, catalogIDs)
    }

    // MARK: - First-run import

    @MainActor
    func testImportWithoutDisabledToolsBlockKeepsAllOff() throws {
        let url = try writeConfig("""
        server_url: https://import.example.test
        token: emt_x
        instance_id: h-connector
        """)

        let settings = ConnectorSettings(defaults: defaults)
        settings.importExistingConfigIfNeeded(configURL: url)

        XCTAssertEqual(settings.serverURL, "https://import.example.test")
        XCTAssertEqual(settings.disabledTools, catalogIDs,
                       "a file with no disabled_tools block must not re-enable tools")
    }

    @MainActor
    func testImportWithDisabledToolsBlockAdoptsIt() throws {
        let url = try writeConfig("""
        server_url: https://import.example.test
        token: emt_x
        instance_id: h-connector
        disabled_tools:
          - notes_create
        """)

        let settings = ConnectorSettings(defaults: defaults)
        settings.importExistingConfigIfNeeded(configURL: url)

        XCTAssertEqual(settings.disabledTools, ["notes_create"])
    }

    @MainActor
    func testImportDoesNotOverrideStoredDisabledSet() throws {
        defaults.set(["reminders_list"], forKey: "connector.disabledTools")
        defaults.set(true, forKey: "connector.disabledToolsInitialized")
        let url = try writeConfig("""
        server_url: https://import.example.test
        token: emt_x
        instance_id: h-connector
        disabled_tools:
          - notes_create
        """)

        let settings = ConnectorSettings(defaults: defaults)
        settings.importExistingConfigIfNeeded(configURL: url)

        XCTAssertEqual(settings.disabledTools, ["reminders_list"])
    }
}
