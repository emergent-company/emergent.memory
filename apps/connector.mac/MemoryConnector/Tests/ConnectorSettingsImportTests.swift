import XCTest
@testable import MemoryConnector

/// First-run import of an existing engine config into app state. Uses a temp
/// config file and an isolated defaults suite. Only non-secret settings are
/// adopted; the connector CLI owns tokens.
final class ConnectorSettingsImportTests: XCTestCase {

    private var suiteName = ""
    private var defaults = UserDefaults(suiteName: "") ?? .standard
    private var tempRoot = FileManager.default.temporaryDirectory

    override func setUpWithError() throws {
        suiteName = "mc-import-test-\(UUID().uuidString)"
        defaults = UserDefaults(suiteName: suiteName) ?? .standard
        tempRoot = FileManager.default.temporaryDirectory
            .appendingPathComponent("mc-import-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: tempRoot, withIntermediateDirectories: true)
        ConnectorKeychainCleanup.clearAll(defaults: defaults)
    }

    override func tearDownWithError() throws {
        try? FileManager.default.removeItem(at: tempRoot)
        if !suiteName.isEmpty { defaults.removePersistentDomain(forName: suiteName) }
        ConnectorKeychainCleanup.clearAll(defaults: defaults)
    }

    private func writeConfig(disabled: [String]) throws -> URL {
        let url = tempRoot.appendingPathComponent("config.yml")
        let values = EngineConfigWriter.Values(
            serverURL: "https://import.example.test",
            token: "emt_ignored_token",
            instanceID: "imported-host-connector",
            disabledTools: disabled
        )
        try EngineConfigWriter.write(configURL: url, values: values)
        return url
    }

    @MainActor
    func testImportPrefillsFromExistingConfig() throws {
        let configURL = try writeConfig(disabled: ["notes_create"])

        let settings = ConnectorSettings(defaults: defaults)
        XCTAssertTrue(settings.serverURL.isEmpty)

        settings.importExistingConfigIfNeeded(configURL: configURL)

        XCTAssertEqual(settings.serverURL, "https://import.example.test")
        XCTAssertEqual(settings.instanceID, "imported-host-connector")
        XCTAssertEqual(settings.disabledTools, ["notes_create"])
        XCTAssertTrue(settings.importedFromConfig)

        // Non-secret fields persisted to the injected suite.
        XCTAssertEqual(defaults.string(forKey: "connector.serverURL"), "https://import.example.test")
        XCTAssertEqual(defaults.string(forKey: "connector.instanceID"), "imported-host-connector")
        XCTAssertEqual(defaults.stringArray(forKey: "connector.disabledTools"), ["notes_create"])

        // The config's token is never adopted into app state.
        let defaultsValues = defaults.dictionaryRepresentation().values.compactMap { $0 as? String }
        XCTAssertFalse(defaultsValues.contains("emt_ignored_token"),
                       "config token must never be persisted to UserDefaults")
        for key in ["connector.token", "connector.manualToken", "oidc.access", "oidc.refresh", "oidc.id"] {
            XCTAssertNil(defaults.object(forKey: key), "unexpected secret key \(key) in UserDefaults")
        }
    }

    @MainActor
    func testImportDoesNotOverrideSavedValues() throws {
        defaults.set("https://saved.example.test", forKey: "connector.serverURL")
        defaults.set("saved-host-connector", forKey: "connector.instanceID")
        defaults.set(["reminders_list"], forKey: "connector.disabledTools")
        defaults.set(true, forKey: "connector.disabledToolsInitialized")
        let configURL = try writeConfig(disabled: ["notes_create"])

        let settings = ConnectorSettings(defaults: defaults)
        settings.importExistingConfigIfNeeded(configURL: configURL)

        XCTAssertEqual(settings.serverURL, "https://saved.example.test")
        XCTAssertEqual(settings.instanceID, "saved-host-connector")
        XCTAssertEqual(settings.disabledTools, ["reminders_list"])
        XCTAssertFalse(settings.importedFromConfig, "nothing missing => no import reported")
    }

    @MainActor
    func testImportMissingFileIsNoop() {
        let settings = ConnectorSettings(defaults: defaults)
        settings.importExistingConfigIfNeeded(configURL: tempRoot.appendingPathComponent("nope.yml"))
        XCTAssertTrue(settings.serverURL.isEmpty)
        XCTAssertFalse(settings.importedFromConfig)
    }
}
