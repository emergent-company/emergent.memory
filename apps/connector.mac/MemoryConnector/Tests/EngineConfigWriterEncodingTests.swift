@testable import MemoryConnector
import XCTest

/// Verifies the config writer encodes YAML-significant characters safely so a
/// value containing a colon, quote, leading indicator, or newline round-trips
/// unchanged and cannot corrupt the document or alter another key's meaning.
///
/// These pin the spec requirement "Engine configuration is persisted without
/// corruption" (specs/mac-connector-app): scalar values are quote/escaped when
/// they would otherwise be misread by the engine's YAML parser, and the reader
/// unquotes them back, staying backward-compatible with plain legacy files.
final class EngineConfigWriterEncodingTests: XCTestCase {
    private var tempRoot: URL!

    override func setUpWithError() throws {
        tempRoot = FileManager.default.temporaryDirectory
            .appendingPathComponent("mc-encoding-\(UUID().uuidString)", isDirectory: true)
    }

    override func tearDownWithError() throws {
        try? FileManager.default.removeItem(at: tempRoot)
    }

    private func makeValues(serverURL: String = "https://memory.example.test",
                            token: String = "emt_secret_token",
                            instanceID: String = "host-connector",
                            projectID: String = "",
                            disabledTools: [String] = []) -> EngineConfigWriter.Values
    {
        EngineConfigWriter.Values(serverURL: serverURL, token: token,
                                  instanceID: instanceID, projectID: projectID,
                                  disabledTools: disabledTools)
    }

    private func roundTrip(_ values: EngineConfigWriter.Values,
                           file: StaticString = #filePath,
                           line: UInt = #line) throws -> EngineConfigWriter.Values
    {
        let configURL = tempRoot.appendingPathComponent("config.yml")
        try EngineConfigWriter.write(configURL: configURL, values: values)
        let read = EngineConfigWriter.read(configURL: configURL)
        XCTAssertNotNil(read, "written config must parse back", file: file, line: line)
        return read!
    }

    // MARK: - Scenario: value containing a colon round-trips

    func testValueContainingColonRoundTrips() throws {
        // `host:port` server URL: colon not followed by space, still must survive.
        let values = makeValues(serverURL: "https://api.example.com:8443/path",
                                token: "emt:token:with:colons")
        let read = try roundTrip(values)
        XCTAssertEqual(read.serverURL, "https://api.example.com:8443/path")
        XCTAssertEqual(read.token, "emt:token:with:colons")
        XCTAssertEqual(read.instanceID, "host-connector", "another key must be unchanged")
    }

    func testValueContainingColonSpaceIsQuotedSoOtherKeysSurvive() throws {
        // `: ` inside a scalar is the YAML mapping separator: left unescaped it
        // would be read as a nested key and corrupt the document.
        let values = makeValues(token: "secret: value")
        let read = try roundTrip(values)
        XCTAssertEqual(read.token, "secret: value")
        XCTAssertEqual(read.serverURL, "https://memory.example.test", "server_url must be unchanged")
        XCTAssertEqual(read.instanceID, "host-connector", "instance_id must be unchanged")
    }

    // MARK: - Scenario: value containing a newline does not corrupt the document

    func testValueContainingNewlineDoesNotCorruptOtherKeys() throws {
        let values = makeValues(token: "line1\nline2",
                                projectID: "proj-123")
        let read = try roundTrip(values)
        XCTAssertEqual(read.token, "line1\nline2", "newline-containing value must round-trip")
        XCTAssertEqual(read.serverURL, "https://memory.example.test")
        XCTAssertEqual(read.instanceID, "host-connector")
        XCTAssertEqual(read.projectID, "proj-123")
    }

    // MARK: - Leading indicator + quote characters

    func testValueWithLeadingIndicatorRoundTrips() throws {
        // A leading `-`/`#`/`?` would be read as a list/comment/indicator.
        let values = makeValues(instanceID: "#tagged-instance")
        let read = try roundTrip(values)
        XCTAssertEqual(read.instanceID, "#tagged-instance")
        XCTAssertEqual(read.serverURL, "https://memory.example.test")
    }

    func testValueContainingQuoteRoundTrips() throws {
        let values = makeValues(token: "he said \"hi\"")
        let read = try roundTrip(values)
        XCTAssertEqual(read.token, "he said \"hi\"")
    }

    // MARK: - Scenario: existing (legacy) configurations still load

    func testLegacyPlainConfigStillLoads() throws {
        let configURL = tempRoot.appendingPathComponent("legacy.yml")
        // The on-disk shape the engine CLI (`projects use`) and the old writer
        // produced: plain scalars, no quotes.
        try EngineConfigWriter.write(configURL: configURL, content: """
        server_url: https://memory.example.test
        token: emt_secret_token
        instance_id: host-connector
        project_id: proj-123

        """)

        let read = EngineConfigWriter.read(configURL: configURL)
        XCTAssertEqual(read?.serverURL, "https://memory.example.test")
        XCTAssertEqual(read?.token, "emt_secret_token")
        XCTAssertEqual(read?.instanceID, "host-connector")
        XCTAssertEqual(read?.projectID, "proj-123")
    }

    // MARK: - Scenario: failed write leaves prior document intact

    func testFailedWriteLeavesPriorFileIntact() throws {
        let configURL = tempRoot.appendingPathComponent("config.yml")
        try EngineConfigWriter.write(configURL: configURL, values: makeValues())
        let before = try Data(contentsOf: configURL)

        // Make the parent directory read-only so the atomic temp-file write
        // fails; the previously valid document must remain byte-for-byte intact
        // rather than being replaced by a partial write.
        let dir = configURL.deletingLastPathComponent()
        try FileManager.default.setAttributes([.posixPermissions: 0o500], ofItemAtPath: dir.path)
        defer {
            try? FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: dir.path)
        }

        XCTAssertThrowsError(
            try EngineConfigWriter.write(configURL: configURL, values: makeValues(disabledTools: ["notes_search"])),
            "a write into a read-only directory must throw"
        )
        XCTAssertEqual(try Data(contentsOf: configURL), before,
                       "a failed write must not replace the prior config")
    }

    // MARK: - Full round-trip of a representative config

    func testFullRoundTripRepresentativeConfig() throws {
        let values = EngineConfigWriter.Values(
            serverURL: "https://api.example.com:8443/v1",
            token: "emt:token with \"quote\" and\nnewline",
            instanceID: "#host-connector-7",
            projectID: "proj:123",
            disabledTools: ["notes_search", "reminders:list", "calendar - read"]
        )
        let read = try roundTrip(values)
        XCTAssertEqual(read, values, "full config must round-trip through write + read")
    }

    func testDisabledToolsWithSpecialCharactersRoundTrip() throws {
        let values = makeValues(disabledTools: ["notes_search", "a tool: with colon"])
        let read = try roundTrip(values)
        XCTAssertEqual(read.disabledTools, ["notes_search", "a tool: with colon"])
    }
}
