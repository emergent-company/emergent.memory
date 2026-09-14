import XCTest
@testable import MemoryConnector

final class EngineConfigWriterTests: XCTestCase {

    private var tempRoot: URL!

    override func setUpWithError() throws {
        tempRoot = FileManager.default.temporaryDirectory
            .appendingPathComponent("mc-writer-\(UUID().uuidString)", isDirectory: true)
    }

    override func tearDownWithError() throws {
        try? FileManager.default.removeItem(at: tempRoot)
    }

    private func makeValues(disabled: [String] = []) -> EngineConfigWriter.Values {
        EngineConfigWriter.Values(
            serverURL: "https://memory.example.test",
            token: "emt_secret_token",
            instanceID: "host-connector",
            disabledTools: disabled
        )
    }

    func testWritesExactYAMLLines() throws {
        let configURL = tempRoot.appendingPathComponent("nested/config.yml")
        try EngineConfigWriter.write(configURL: configURL, values: makeValues())

        let content = try String(contentsOf: configURL, encoding: .utf8)
        XCTAssertTrue(content.contains("server_url: https://memory.example.test\n"))
        XCTAssertTrue(content.contains("token: emt_secret_token\n"))
        XCTAssertTrue(content.contains("instance_id: host-connector\n"))
        XCTAssertFalse(content.contains("project_id"))
        XCTAssertFalse(content.contains("disabled_tools"))
    }

    func testDisabledToolsPresentWhenNonEmpty() throws {
        let configURL = tempRoot.appendingPathComponent("config.yml")
        try EngineConfigWriter.write(
            configURL: configURL,
            values: makeValues(disabled: ["notes_search", "reminders_list"])
        )

        let content = try String(contentsOf: configURL, encoding: .utf8)
        XCTAssertTrue(content.contains("disabled_tools:\n"))
        XCTAssertTrue(content.contains("  - notes_search\n"))
        XCTAssertTrue(content.contains("  - reminders_list\n"))
    }

    func testWritesWholeCatalogWhenAllToolsDisabled() throws {
        // The new default disables every catalog tool; the writer must emit the
        // full list without truncating or choking on it.
        let allTools = ToolCatalog.tools.map(\.id)
        let configURL = tempRoot.appendingPathComponent("config.yml")
        try EngineConfigWriter.write(configURL: configURL, values: makeValues(disabled: allTools))

        let content = try String(contentsOf: configURL, encoding: .utf8)
        XCTAssertTrue(content.contains("disabled_tools:\n"))
        for id in allTools {
            XCTAssertTrue(content.contains("  - \(id)\n"), "missing \(id)")
        }
    }

    func testProjectIDIncludedWhenProvided() throws {
        let configURL = tempRoot.appendingPathComponent("config.yml")
        var values = makeValues()
        values = EngineConfigWriter.Values(
            serverURL: values.serverURL, token: values.token, instanceID: values.instanceID,
            projectID: "proj-123", disabledTools: []
        )
        try EngineConfigWriter.write(configURL: configURL, values: values)
        let content = try String(contentsOf: configURL, encoding: .utf8)
        XCTAssertTrue(content.contains("project_id: proj-123\n"))
    }

    func testFileMode0600AndParent0700() throws {
        let configURL = tempRoot.appendingPathComponent("nested/deeper/config.yml")
        try EngineConfigWriter.write(configURL: configURL, values: makeValues())

        let attrs = try FileManager.default.attributesOfItem(atPath: configURL.path)
        let mode = attrs[.posixPermissions] as? NSNumber
        XCTAssertEqual(mode?.intValue, 0o600, "config file must be 0600")

        let parentAttrs = try FileManager.default.attributesOfItem(atPath: configURL.deletingLastPathComponent().path)
        let parentMode = parentAttrs[.posixPermissions] as? NSNumber
        XCTAssertEqual(parentMode?.intValue, 0o700, "created parent directory must be 0700")
    }

    func testRewriteOverwritesContent() throws {
        let configURL = tempRoot.appendingPathComponent("config.yml")
        try EngineConfigWriter.write(configURL: configURL, values: makeValues())
        try EngineConfigWriter.write(
            configURL: configURL,
            values: makeValues(disabled: ["notes_create"])
        )
        let content = try String(contentsOf: configURL, encoding: .utf8)
        XCTAssertTrue(content.contains("disabled_tools:\n"))
        XCTAssertFalse(content.contains("notes_search"))
    }
}

// MARK: - read() — first-run import parser

extension EngineConfigWriterTests {

    func testReadRoundTrip() throws {
        let configURL = tempRoot.appendingPathComponent("config.yml")
        let values = makeValues(disabled: ["notes_create", "reminders_add"])
        try EngineConfigWriter.write(configURL: configURL, values: values)

        let read = EngineConfigWriter.read(configURL: configURL)
        XCTAssertEqual(read, values)
    }

    func testReadProjectIDRoundTrip() throws {
        let configURL = tempRoot.appendingPathComponent("config.yml")
        let values = EngineConfigWriter.Values(
            serverURL: "https://memory.example.test",
            token: "emt_secret_token",
            instanceID: "host-connector",
            projectID: "proj-123",
            disabledTools: []
        )
        try EngineConfigWriter.write(configURL: configURL, values: values)
        XCTAssertEqual(EngineConfigWriter.read(configURL: configURL), values)
    }

    func testReadToleratesBlankCommentAndUnknownLines() throws {
        let configURL = tempRoot.appendingPathComponent("config.yml")
        let content = """
        # engine config

        server_url:   https://memory.example.test

        unknown_key: whatever
        token: emt_secret_token
          stray: indented unknown
        instance_id: host-connector

        disabled_tools:
          - notes_search
          - reminders_list

        """
        try EngineConfigWriter.write(configURL: configURL, content: content)

        let read = EngineConfigWriter.read(configURL: configURL)
        XCTAssertEqual(read?.serverURL, "https://memory.example.test")
        XCTAssertEqual(read?.token, "emt_secret_token")
        XCTAssertEqual(read?.instanceID, "host-connector")
        XCTAssertEqual(read?.disabledTools, ["notes_search", "reminders_list"])
    }

    func testReadDisabledToolsEmpty() throws {
        let configURL = tempRoot.appendingPathComponent("config.yml")
        try EngineConfigWriter.write(configURL: configURL, values: makeValues())
        let read = EngineConfigWriter.read(configURL: configURL)
        XCTAssertEqual(read?.disabledTools, [])
    }

    func testReadDetailedReportsMissingDisabledToolsBlock() throws {
        let configURL = tempRoot.appendingPathComponent("config.yml")
        try EngineConfigWriter.write(configURL: configURL, values: makeValues())

        let parsed = EngineConfigWriter.readDetailed(configURL: configURL)
        XCTAssertEqual(parsed?.values.disabledTools, [])
        XCTAssertEqual(parsed?.hasDisabledTools, false,
                       "no disabled_tools block means tools were never configured")
    }

    func testReadDetailedReportsPresentEmptyDisabledToolsBlock() throws {
        // An explicitly empty block (all tools ON) is different from absence.
        let configURL = tempRoot.appendingPathComponent("config.yml")
        try EngineConfigWriter.write(configURL: configURL, content: """
        server_url: https://memory.example.test
        token: emt_secret_token
        instance_id: host-connector
        disabled_tools:

        """)

        let parsed = EngineConfigWriter.readDetailed(configURL: configURL)
        XCTAssertEqual(parsed?.values.disabledTools, [])
        XCTAssertEqual(parsed?.hasDisabledTools, true)
    }

    func testReadMissingFileIsNil() {
        let missing = tempRoot.appendingPathComponent("does-not-exist.yml")
        XCTAssertNil(EngineConfigWriter.read(configURL: missing))
    }

    func testReadRequiresServerURL() throws {
        let configURL = tempRoot.appendingPathComponent("config.yml")
        try EngineConfigWriter.write(configURL: configURL, content: "token: emt_x\ninstance_id: h-connector\n")
        XCTAssertNil(EngineConfigWriter.read(configURL: configURL))
    }
}
