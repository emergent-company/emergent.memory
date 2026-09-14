import XCTest
@testable import MemoryConnector

/// Validation and payload-building rules for `MCPServerForm`, per transport.
final class MCPServerFormTests: XCTestCase {

    // MARK: - Name

    func testNameRequired() {
        var form = MCPServerForm()
        form.transport = .stdio
        form.command = "/usr/bin/mcp"

        XCTAssertEqual(form.validationMessage(), "Name is required.")

        form.name = "   "
        XCTAssertEqual(form.validationMessage(), "Name is required.")
    }

    func testDuplicateNameRejected() {
        var form = MCPServerForm()
        form.name = "filesystem"
        form.command = "/usr/bin/mcp"

        XCTAssertEqual(form.validationMessage(existingNames: ["filesystem"]),
                       "A server named “filesystem” already exists.")
        XCTAssertNil(form.validationMessage(existingNames: ["other"]))
    }

    func testNameIsTrimmedBeforeDuplicateCheck() {
        var form = MCPServerForm()
        form.name = "  filesystem  "
        form.command = "/usr/bin/mcp"

        XCTAssertEqual(form.validationMessage(existingNames: ["filesystem"]),
                       "A server named “filesystem” already exists.")
    }

    // MARK: - Transport-specific required fields

    func testStdioRequiresCommand() {
        var form = MCPServerForm()
        form.name = "filesystem"
        form.transport = .stdio
        form.command = "   "

        XCTAssertEqual(form.validationMessage(), "Command is required for a stdio server.")

        form.command = "/usr/bin/mcp"
        XCTAssertNil(form.validationMessage())
    }

    func testHTTPRequiresURL() {
        var form = MCPServerForm()
        form.name = "remote"
        form.transport = .http

        XCTAssertEqual(form.validationMessage(), "URL is required for an http server.")

        form.url = "https://example.com/mcp"
        XCTAssertNil(form.validationMessage())
    }

    func testSSERequiresURL() {
        var form = MCPServerForm()
        form.name = "remote"
        form.transport = .sse

        XCTAssertEqual(form.validationMessage(), "URL is required for an sse server.")

        form.url = "https://example.com/sse"
        XCTAssertNil(form.validationMessage())
    }

    func testHTTPFormWithURLIsValidAndIgnoresCommand() {
        var form = MCPServerForm()
        form.name = "remote"
        form.transport = .http
        form.url = "https://example.com/mcp"
        form.command = "left-over"

        XCTAssertTrue(form.isValid)
        let config = form.config()
        XCTAssertNil(config.command)
        XCTAssertNil(config.args)
        XCTAssertNil(config.env)
    }

    // MARK: - Payload

    func testStdioConfigFiltersBlankArgsAndEmptyEnvKeys() {
        var form = MCPServerForm()
        form.name = "  filesystem "
        form.transport = .stdio
        form.command = " /usr/bin/mcp "
        form.args = ["--root", "  ", "/tmp"]
        form.env = [
            MCPServerForm.Entry(key: "TOKEN", value: "s3cret"),
            MCPServerForm.Entry(key: "   ", value: "dropped"),
        ]

        let config = form.config()

        XCTAssertEqual(config.name, "filesystem")
        XCTAssertEqual(config.command, "/usr/bin/mcp")
        XCTAssertEqual(config.args, ["--root", "/tmp"])
        XCTAssertEqual(config.env, ["TOKEN": "s3cret"])
        XCTAssertNil(config.url)
        XCTAssertTrue(config.enabled)
    }

    func testEmptyArraysBecomeNil() {
        var form = MCPServerForm()
        form.name = "filesystem"
        form.transport = .stdio
        form.command = "mcp"
        form.args = ["", "  "]
        form.env = [MCPServerForm.Entry()]

        let config = form.config()

        XCTAssertNil(config.args)
        XCTAssertNil(config.env)
    }

    func testHTTPConfigCarriesHeaders() {
        var form = MCPServerForm()
        form.name = "remote"
        form.transport = .http
        form.url = " https://example.com/mcp "
        form.headers = [MCPServerForm.Entry(key: "Authorization", value: "Bearer x")]
        form.enabled = false

        let config = form.config()

        XCTAssertEqual(config.url, "https://example.com/mcp")
        XCTAssertEqual(config.headers, ["Authorization": "Bearer x"])
        XCTAssertFalse(config.enabled)
    }

    // MARK: - Round trip from a decoded config

    func testFormRoundTripsAConfig() {
        let config = HostedMCPServerConfig(name: "filesystem",
                                           transport: .stdio,
                                           enabled: false,
                                           command: "/usr/bin/mcp",
                                           args: ["--root", "/tmp"],
                                           env: ["TOKEN": "s3cret"],
                                           disabledTools: ["delete_file"])

        let form = MCPServerForm(config: config)
        let rebuilt = form.config()

        XCTAssertEqual(rebuilt.name, config.name)
        XCTAssertEqual(rebuilt.transport, config.transport)
        XCTAssertEqual(rebuilt.enabled, config.enabled)
        XCTAssertEqual(rebuilt.command, config.command)
        XCTAssertEqual(rebuilt.args, config.args)
        XCTAssertEqual(rebuilt.env, config.env)
        XCTAssertEqual(rebuilt.disabledTools, config.disabledTools, "deny list must be preserved")
    }
}
