import XCTest
@testable import MemoryConnector

/// Endpoint + error-mapping coverage for `MCPServersAPIClient` against a
/// stubbed `URLSession` (no real network).
final class MCPServersAPIClientTests: XCTestCase {

    private let baseURL = "http://127.0.0.1:8890"

    private lazy var detailJSON = """
    {"name":"filesystem","transport":"stdio","enabled":true,
     "command":"/usr/bin/mcp","args":["--root","/tmp"],
     "env":{"TOKEN":"s3cret"},"disabled_tools":["delete_file"],
     "status":{"connected":true,"toolCount":2,"tools":[
       {"name":"filesystem_read","description":"Read a file",
        "inputSchema":{"type":"object","properties":{}}},
       {"name":"filesystem_write","description":"Write a file","inputSchema":{}}
     ]}}
    """

    private let listJSON = """
    [{"name":"filesystem","transport":"stdio","enabled":true,"connected":true,"toolCount":2},
     {"name":"remote","transport":"http","enabled":false,"connected":false,
      "error":"dial tcp: connection refused","toolCount":0}]
    """

    private let toolsJSON = """
    [{"name":"filesystem_read","description":"Read a file","inputSchema":{"type":"object"}}]
    """

    private let statusJSON = """
    {"instanceID":"inst-1","serverURL":"https://api.example.test","connected":true,
     "toolCount":3,"servers":[{"name":"filesystem","transport":"stdio","enabled":true,
     "connected":true,"toolCount":2}]}
    """

    override func setUp() {
        super.setUp()
        StubURLProtocol.registry.reset()
    }

    override func tearDown() {
        StubURLProtocol.registry.reset()
        super.tearDown()
    }

    private func makeClient() -> MCPServersAPIClient {
        MCPServersAPIClient(baseURL: baseURL, session: StubURLProtocol.makeSession())
    }

    // MARK: - List

    func testListDecodesSummaries() async throws {
        StubURLProtocol.registry.setHandler { _ in .ok(self.listJSON) }

        let servers = try await makeClient().list()

        XCTAssertEqual(servers.count, 2)
        XCTAssertEqual(servers[0].name, "filesystem")
        XCTAssertEqual(servers[0].transport, .stdio)
        XCTAssertTrue(servers[0].connected)
        XCTAssertEqual(servers[0].toolCount, 2)
        XCTAssertEqual(servers[1].transport, .http)
        XCTAssertFalse(servers[1].enabled)
        XCTAssertEqual(servers[1].error, "dial tcp: connection refused")

        let request = StubURLProtocol.registry.capturedRequest
        XCTAssertEqual(request?.httpMethod, "GET")
        XCTAssertEqual(request?.url?.path, "/api/mcp-servers")
    }

    // MARK: - Get

    func testGetDecodesFullConfigAndStatus() async throws {
        StubURLProtocol.registry.setHandler { _ in .ok(self.detailJSON) }

        let detail = try await makeClient().get(name: "filesystem")

        XCTAssertEqual(detail.config.name, "filesystem")
        XCTAssertEqual(detail.config.transport, .stdio)
        XCTAssertEqual(detail.config.command, "/usr/bin/mcp")
        XCTAssertEqual(detail.config.args, ["--root", "/tmp"])
        XCTAssertEqual(detail.config.env?["TOKEN"], "s3cret")
        XCTAssertEqual(detail.config.disabledTools, ["delete_file"])
        XCTAssertTrue(detail.status.connected)
        XCTAssertEqual(detail.status.tools.map(\.name), ["filesystem_read", "filesystem_write"])
        XCTAssertEqual(StubURLProtocol.registry.capturedRequest?.url?.path,
                       "/api/mcp-servers/filesystem")
    }

    func testGetEscapesServerNameInPath() async throws {
        StubURLProtocol.registry.setHandler { _ in .ok(self.detailJSON) }

        _ = try? await makeClient().get(name: "my server")

        XCTAssertEqual(StubURLProtocol.registry.capturedRequest?.url?.path,
                       "/api/mcp-servers/my server")
        XCTAssertEqual(StubURLProtocol.registry.capturedRequest?.url?.absoluteString,
                       "\(baseURL)/api/mcp-servers/my%20server")
    }

    // MARK: - Create

    func testCreateSendsPOSTWithSnakeCaseBodyAndDecodesDetail() async throws {
        StubURLProtocol.registry.setHandler { _ in .status(201, json: self.detailJSON) }

        let config = HostedMCPServerConfig(name: "filesystem",
                                           transport: .stdio,
                                           enabled: true,
                                           command: "/usr/bin/mcp",
                                           args: ["--root", "/tmp"],
                                           env: ["TOKEN": "s3cret"],
                                           disabledTools: ["delete_file"])
        let detail = try await makeClient().create(config)

        XCTAssertEqual(detail.config.name, "filesystem")

        let request = StubURLProtocol.registry.capturedRequest
        XCTAssertEqual(request?.httpMethod, "POST")
        XCTAssertEqual(request?.url?.path, "/api/mcp-servers")
        XCTAssertEqual(request?.value(forHTTPHeaderField: "Content-Type"), "application/json")

        let body = HTTPBodyReader.string(from: request!)
        XCTAssertTrue(body.contains("\"name\":\"filesystem\""), body)
        XCTAssertTrue(body.contains("\"transport\":\"stdio\""), body)
        XCTAssertTrue(body.contains("\"disabled_tools\""), body)
        XCTAssertTrue(body.contains("delete_file"), body)
    }

    func testCreateHTTPConfigOmitsCommandFields() async throws {
        StubURLProtocol.registry.setHandler { _ in .status(201, json: self.detailJSON) }

        let config = HostedMCPServerConfig(name: "remote",
                                           transport: .http,
                                           url: "https://example.com/mcp",
                                           headers: ["Authorization": "Bearer x"])
        _ = try await makeClient().create(config)

        let body = HTTPBodyReader.string(from: StubURLProtocol.registry.capturedRequest!)
        // Decode instead of substring-matching: Foundation's JSONEncoder escapes
        // `/` as `\/`, so an exact "url":"https://..." substring is brittle.
        let json = try XCTUnwrap(JSONSerialization.jsonObject(with: Data(body.utf8)) as? [String: Any])
        XCTAssertEqual(json["url"] as? String, "https://example.com/mcp")
        XCTAssertNotNil(json["headers"])
        XCTAssertNil(json["command"], body)
        XCTAssertNil(json["args"], body)
    }

    // MARK: - Update / toggle / delete

    func testUpdateSendsPUTConfig() async throws {
        StubURLProtocol.registry.setHandler { _ in .ok(self.detailJSON) }

        let config = HostedMCPServerConfig(name: "filesystem",
                                           transport: .stdio,
                                           command: "/usr/bin/mcp")
        _ = try await makeClient().update(name: "filesystem", config: config)

        let request = StubURLProtocol.registry.capturedRequest
        XCTAssertEqual(request?.httpMethod, "PUT")
        XCTAssertEqual(request?.url?.path, "/api/mcp-servers/filesystem/config")
    }

    func testSetEnabledSendsPUTEnabledBody() async throws {
        StubURLProtocol.registry.setHandler { _ in .ok(self.detailJSON) }

        _ = try await makeClient().setEnabled(name: "filesystem", enabled: false)

        let request = StubURLProtocol.registry.capturedRequest
        XCTAssertEqual(request?.httpMethod, "PUT")
        XCTAssertEqual(request?.url?.path, "/api/mcp-servers/filesystem/enabled")
        XCTAssertEqual(HTTPBodyReader.string(from: request!), #"{"enabled":false}"#)
    }

    func testDeleteSendsDELETEAndAccepts204() async throws {
        StubURLProtocol.registry.setHandler { _ in .status(204) }

        try await makeClient().delete(name: "filesystem")

        let request = StubURLProtocol.registry.capturedRequest
        XCTAssertEqual(request?.httpMethod, "DELETE")
        XCTAssertEqual(request?.url?.path, "/api/mcp-servers/filesystem")
    }

    // MARK: - Tools / status

    func testToolsDecodesList() async throws {
        StubURLProtocol.registry.setHandler { _ in .ok(self.toolsJSON) }

        let tools = try await makeClient().tools(name: "filesystem")

        XCTAssertEqual(tools.count, 1)
        XCTAssertEqual(tools.first?.name, "filesystem_read")
        XCTAssertEqual(tools.first?.description, "Read a file")
        XCTAssertEqual(StubURLProtocol.registry.capturedRequest?.url?.path,
                       "/api/mcp-servers/filesystem/tools")
    }

    func testStatusDecodesRuntime() async throws {
        StubURLProtocol.registry.setHandler { _ in .ok(self.statusJSON) }

        let status = try await makeClient().status()

        XCTAssertEqual(status.instanceID, "inst-1")
        XCTAssertEqual(status.serverURL, "https://api.example.test")
        XCTAssertTrue(status.connected)
        XCTAssertEqual(status.toolCount, 3)
        XCTAssertEqual(status.servers.first?.name, "filesystem")
        XCTAssertEqual(StubURLProtocol.registry.capturedRequest?.url?.path, "/api/status")
    }

    // MARK: - Error mapping

    func testServerErrorIsMappedWithMessage() async {
        StubURLProtocol.registry.setHandler { _ in
            .status(400, json: #"{"error":"mcp server \"x\": command is required for stdio transport"}"#)
        }

        await XCTAssertThrowsErrorAsync(try await self.makeClient().create(
            HostedMCPServerConfig(name: "x", transport: .stdio)
        )) { error in
            XCTAssertEqual(error as? MCPServersAPIError,
                           .server(400, "mcp server \"x\": command is required for stdio transport"))
        }
    }

    func testConflictErrorIsMapped() async {
        StubURLProtocol.registry.setHandler { _ in
            .status(409, json: #"{"error":"mcp server \"x\" already exists"}"#)
        }

        await XCTAssertThrowsErrorAsync(try await self.makeClient().create(
            HostedMCPServerConfig(name: "x", transport: .http, url: "http://x")
        )) { error in
            XCTAssertEqual(error as? MCPServersAPIError, .server(409, "mcp server \"x\" already exists"))
        }
    }

    func testNotFoundErrorIsMapped() async {
        StubURLProtocol.registry.setHandler { _ in .status(404, json: #"{"error":"mcp server \"gone\" not found"}"#) }

        await XCTAssertThrowsErrorAsync(try await self.makeClient().get(name: "gone")) { error in
            XCTAssertEqual(error as? MCPServersAPIError, .server(404, "mcp server \"gone\" not found"))
        }
    }

    func testNetworkFailureMapsToUnreachable() async {
        StubURLProtocol.registry.setHandler { _ in .failure(URLError(.cannotConnectToHost)) }

        await XCTAssertThrowsErrorAsync(try await self.makeClient().list()) { error in
            guard case .unreachable = (error as? MCPServersAPIError) else {
                return XCTFail("expected unreachable, got \(error)")
            }
        }
    }

    func testInvalidJSONMapsToDecoding() async {
        StubURLProtocol.registry.setHandler { _ in .ok("not json") }

        await XCTAssertThrowsErrorAsync(try await self.makeClient().list()) { error in
            guard case .decoding = (error as? MCPServersAPIError) else {
                return XCTFail("expected decoding, got \(error)")
            }
        }
    }
}

/// Small async-aware `XCTAssertThrowsError` equivalent (the stdlib one is
/// synchronous-only).
func XCTAssertThrowsErrorAsync<T>(
    _ expression: @autoclosure () async throws -> T,
    file: StaticString = #filePath,
    line: UInt = #line,
    _ handler: (Error) -> Void
) async {
    do {
        _ = try await expression()
        XCTFail("expected an error to be thrown", file: file, line: line)
    } catch {
        handler(error)
    }
}
