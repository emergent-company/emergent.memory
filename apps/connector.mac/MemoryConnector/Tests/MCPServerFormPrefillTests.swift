@testable import MemoryConnector
import XCTest

/// Prefill rules for the Add sheet when it is seeded from a registry
/// suggestion, plus the required/secret metadata the sheet surfaces.
final class MCPServerFormPrefillTests: XCTestCase {
    // MARK: - Stdio

    func testStdioSuggestionPrefillsForm() {
        let suggested = HostedMCPServerConfig(name: "chrome-devtools-mcp",
                                              transport: .stdio,
                                              enabled: true,
                                              command: "npx",
                                              args: ["-y", "chrome-devtools-mcp@1.2.3"],
                                              env: ["CHROME_PATH": ""])

        let form = MCPServerForm(suggested: suggested, fallbackName: "chrome-devtools-mcp")

        XCTAssertEqual(form.name, "chrome-devtools-mcp")
        XCTAssertEqual(form.transport, .stdio)
        XCTAssertTrue(form.enabled)
        XCTAssertEqual(form.command, "npx")
        XCTAssertEqual(form.args, ["-y", "chrome-devtools-mcp@1.2.3"])
        XCTAssertEqual(form.env.map(\.key), ["CHROME_PATH"])
        XCTAssertTrue(form.isValid)

        let rebuilt = form.config()
        XCTAssertEqual(rebuilt.command, "npx")
        XCTAssertEqual(rebuilt.args, ["-y", "chrome-devtools-mcp@1.2.3"])
        XCTAssertEqual(rebuilt.env, ["CHROME_PATH": ""])
    }

    func testEmptySuggestedNameUsesDerivedFallback() {
        let suggested = HostedMCPServerConfig(name: "", transport: .stdio, command: "npx")

        let form = MCPServerForm(suggested: suggested, fallbackName: "chrome-devtools-mcp")

        XCTAssertEqual(form.name, "chrome-devtools-mcp")
    }

    func testExistingSuggestedNameIsKeptOverFallback() {
        let suggested = HostedMCPServerConfig(name: "from-connector",
                                              transport: .stdio,
                                              command: "npx")

        let form = MCPServerForm(suggested: suggested, fallbackName: "derived")

        XCTAssertEqual(form.name, "from-connector")
    }

    // MARK: - Remote

    func testRemoteSuggestionPrefillsForm() {
        let suggested = HostedMCPServerConfig(name: "remote-mcp",
                                              transport: .http,
                                              enabled: true,
                                              url: "https://example.com/mcp",
                                              headers: ["Authorization": ""])

        let form = MCPServerForm(suggested: suggested, fallbackName: "remote-mcp")

        XCTAssertEqual(form.transport, .http)
        XCTAssertEqual(form.url, "https://example.com/mcp")
        XCTAssertEqual(form.headers.map(\.key), ["Authorization"])
        XCTAssertFalse(form.transport.usesCommand)

        let rebuilt = form.config()
        XCTAssertEqual(rebuilt.url, "https://example.com/mcp")
        XCTAssertEqual(rebuilt.headers, ["Authorization": ""])
        XCTAssertNil(rebuilt.command)
        XCTAssertNil(rebuilt.args)
    }

    // MARK: - Derived local name

    func testLocalNameDerivedFromRegistryID() {
        XCTAssertEqual(RegistryServer(name: "io.github.ChromeDevTools/chrome-devtools-mcp").localName,
                       "chrome-devtools-mcp")
        XCTAssertEqual(RegistryServer(name: "plain-name").localName, "plain-name")
    }

    // MARK: - Notes

    func testNotesMarkRequiredAndSecretFields() throws {
        let json = """
        {"server":{"name":"io.github.example/remote-mcp","packages":[],
          "remotes":[],"envVars":[],"hasPackages":false,"hasRemotes":true},
         "suggested":null,
         "envVars":[{"name":"API_KEY","isRequired":true,"isSecret":true},
                    {"name":"CHROME_PATH","isRequired":true,"isSecret":false}],
         "headers":[{"name":"Authorization","isRequired":true,"isSecret":true}],
         "warnings":["npx is not on PATH"]}
        """
        let detail = try JSONDecoder().decode(MCPRegistryServerDetail.self, from: Data(json.utf8))

        let notes = MCPRegistryNotes(detail: detail)

        XCTAssertEqual(notes.warnings, ["npx is not on PATH"])
        XCTAssertEqual(notes.requiredEnvLabels, ["API_KEY (secret)", "CHROME_PATH"])
        XCTAssertEqual(notes.requiredHeaderLabels, ["Authorization (secret)"])
        XCTAssertFalse(notes.isEmpty)
    }

    func testNotesAreEmptyForPlainManualAdd() {
        XCTAssertTrue(MCPRegistryNotes().isEmpty)
    }
}
