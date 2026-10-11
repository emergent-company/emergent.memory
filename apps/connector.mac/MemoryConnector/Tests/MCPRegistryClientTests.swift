@testable import MemoryConnector
import XCTest

/// Registry client coverage against a stubbed `URLSession`: search/detail
/// decode, query/path shape, and error mapping.
final class MCPRegistryClientTests: XCTestCase {
    private let baseURL = "http://127.0.0.1:8890"

    private let searchJSON = """
    {"servers":[{"name":"io.github.ChromeDevTools/chrome-devtools-mcp",
      "title":"Chrome DevTools","description":"Control Chrome from an MCP client",
      "version":"1.2.3",
      "repository":{"url":"https://github.com/ChromeDevTools/chrome-devtools-mcp","source":"github"},
      "packages":[{"registryType":"npm","name":"chrome-devtools-mcp",
        "identifier":"chrome-devtools-mcp","transport":"stdio"}],
      "remotes":[],
      "envVars":[{"name":"CHROME_PATH","description":"Path to Chrome","isRequired":true,"isSecret":false}],
      "hasPackages":true,"hasRemotes":false}],
     "nextCursor":"page-2","count":1}
    """

    private let stdioDetailJSON = """
    {"server":{"name":"io.github.ChromeDevTools/chrome-devtools-mcp",
      "title":"Chrome DevTools","description":"Control Chrome from an MCP client",
      "version":"1.2.3",
      "packages":[{"registryType":"npm","name":"chrome-devtools-mcp",
        "identifier":"chrome-devtools-mcp","transport":"stdio"}],
      "remotes":[],"envVars":[],"hasPackages":true,"hasRemotes":false},
     "suggested":{"name":"chrome-devtools-mcp","transport":"stdio","enabled":true,
       "command":"npx","args":["-y","chrome-devtools-mcp@1.2.3"],"env":{"CHROME_PATH":""}},
     "envVars":[{"name":"CHROME_PATH","description":"Path to Chrome","isRequired":true,"isSecret":false}],
     "headers":[],
     "warnings":["npx is not on PATH; install Node.js to run this server"]}
    """

    private let remoteDetailJSON = """
    {"server":{"name":"io.github.example/remote-mcp","title":"Example Remote",
      "description":"A remote server","packages":[],
      "remotes":[{"type":"streamable-http","url":"https://example.com/mcp"}],
      "envVars":[],"hasPackages":false,"hasRemotes":true},
     "suggested":{"name":"remote-mcp","transport":"http","enabled":true,
       "url":"https://example.com/mcp","headers":{"Authorization":""}},
     "envVars":[],
     "headers":[{"name":"Authorization","description":"API token","isRequired":true,"isSecret":true}],
     "warnings":[]}
    """

    override func setUp() {
        super.setUp()
        StubURLProtocol.registry.reset()
    }

    override func tearDown() {
        StubURLProtocol.registry.reset()
        super.tearDown()
    }

    private func makeClient() -> MCPRegistryAPIClient {
        MCPRegistryAPIClient(baseURL: baseURL, session: StubURLProtocol.makeSession())
    }

    // MARK: - Project scoping

    func testDefaultBaseURLMatchesLegacyManagementPort() {
        XCTAssertEqual(MCPRegistryAPIClient.defaultBaseURL, "http://127.0.0.1:8931")
    }

    func testBaseURLTargetsManagementPort() {
        XCTAssertEqual(MCPRegistryAPIClient.baseURL(port: 9012), "http://127.0.0.1:9012")
    }

    func testClientBuiltForPortHitsThatPort() async {
        StubURLProtocol.registry.setHandler { _ in .ok(self.searchJSON) }

        let client = MCPRegistryAPIClient(baseURL: MCPRegistryAPIClient.baseURL(port: 9012),
                                          session: StubURLProtocol.makeSession())
        _ = try? await client.search(query: "chrome")

        XCTAssertEqual(StubURLProtocol.registry.capturedRequest?.url?.port, 9012,
                       "a client built for a port targets that project's management port")
        XCTAssertEqual(StubURLProtocol.registry.capturedRequest?.url?.host, "127.0.0.1")
    }

    // MARK: - Search

    func testSearchRegistryDecodesResultsAndForwardsQuery() async throws {
        StubURLProtocol.registry.setHandler { _ in .ok(self.searchJSON) }

        let result = try await makeClient().search(query: "chrome", limit: 5, cursor: "page-2")

        XCTAssertEqual(result.count, 1)
        XCTAssertEqual(result.nextCursor, "page-2")
        XCTAssertEqual(result.servers.count, 1)
        let entry = try XCTUnwrap(result.servers.first)
        XCTAssertEqual(entry.name, "io.github.ChromeDevTools/chrome-devtools-mcp")
        XCTAssertEqual(entry.title, "Chrome DevTools")
        XCTAssertEqual(entry.version, "1.2.3")
        XCTAssertEqual(entry.repository?.source, "github")
        XCTAssertEqual(entry.packages.first?.registryType, "npm")
        XCTAssertEqual(entry.packages.first?.transport, "stdio")
        XCTAssertEqual(entry.envVars.first?.name, "CHROME_PATH")
        XCTAssertTrue(entry.envVars.first?.isRequired == true)
        XCTAssertTrue(entry.hasPackages)
        XCTAssertFalse(entry.hasRemotes)
        XCTAssertEqual(entry.localName, "chrome-devtools-mcp")

        let request = StubURLProtocol.registry.capturedRequest
        XCTAssertEqual(request?.httpMethod, "GET")
        XCTAssertEqual(request?.url?.path, "/api/mcp-registry/search")
        let items = try URLComponents(url: XCTUnwrap(request?.url), resolvingAgainstBaseURL: false)?.queryItems
        XCTAssertEqual(items?.first(where: { $0.name == "q" })?.value, "chrome")
        XCTAssertEqual(items?.first(where: { $0.name == "limit" })?.value, "5")
        XCTAssertEqual(items?.first(where: { $0.name == "cursor" })?.value, "page-2")
    }

    // MARK: - Detail

    func testDetailDecodesSuggestedStdioConfig() async throws {
        StubURLProtocol.registry.setHandler { _ in .ok(self.stdioDetailJSON) }

        let detail = try await makeClient().server(name: "io.github.ChromeDevTools/chrome-devtools-mcp")

        XCTAssertEqual(detail.server.localName, "chrome-devtools-mcp")
        let suggested = try XCTUnwrap(detail.suggested)
        XCTAssertEqual(suggested.name, "chrome-devtools-mcp")
        XCTAssertEqual(suggested.transport, .stdio)
        XCTAssertEqual(suggested.command, "npx")
        XCTAssertEqual(suggested.args, ["-y", "chrome-devtools-mcp@1.2.3"])
        XCTAssertEqual(suggested.env?["CHROME_PATH"], "")
        XCTAssertTrue(suggested.enabled)
        XCTAssertNil(suggested.url)
        XCTAssertEqual(detail.envVars.first?.isRequired, true)
        XCTAssertEqual(detail.warnings, ["npx is not on PATH; install Node.js to run this server"])
    }

    func testDetailDecodesSuggestedRemoteConfig() async throws {
        StubURLProtocol.registry.setHandler { _ in .ok(self.remoteDetailJSON) }

        let detail = try await makeClient().server(name: "io.github.example/remote-mcp")

        let suggested = try XCTUnwrap(detail.suggested)
        XCTAssertEqual(suggested.transport, .http)
        XCTAssertEqual(suggested.url, "https://example.com/mcp")
        XCTAssertEqual(suggested.headers?["Authorization"], "")
        XCTAssertNil(suggested.command)
        XCTAssertTrue(detail.server.hasRemotes)
        XCTAssertEqual(detail.headers.first?.isSecret, true)
        XCTAssertTrue(detail.warnings.isEmpty)
    }

    func testDetailToleratesPascalCaseSuggested() async throws {
        let json = """
        {"server":{"name":"io.github.example/one","packages":[],"remotes":[],"envVars":[]},
         "suggested":{"Name":"one","Transport":"stdio","Enabled":true,
           "Command":"npx","Args":["-y","one"]},
         "warnings":[]}
        """
        StubURLProtocol.registry.setHandler { _ in .ok(json) }

        let detail = try await makeClient().server(name: "io.github.example/one")

        XCTAssertEqual(detail.suggested?.name, "one")
        XCTAssertEqual(detail.suggested?.transport, .stdio)
        XCTAssertEqual(detail.suggested?.command, "npx")
        XCTAssertEqual(detail.suggested?.args, ["-y", "one"])
    }

    func testDetailEscapesSlashInRegistryName() async {
        StubURLProtocol.registry.setHandler { _ in .ok(self.stdioDetailJSON) }

        _ = try? await makeClient().server(name: "io.github.ChromeDevTools/chrome-devtools-mcp")

        let absolute = StubURLProtocol.registry.capturedRequest?.url?.absoluteString
        XCTAssertEqual(absolute,
                       "\(baseURL)/api/mcp-registry/servers/io.github.ChromeDevTools%2Fchrome-devtools-mcp")
    }

    // MARK: - Errors

    func testRegistryNotFoundIsMappedWithMessage() async {
        StubURLProtocol.registry.setHandler { _ in
            .status(404, json: #"{"error":"server \"io.github.x/y\" not found in registry"}"#)
        }

        do {
            _ = try await makeClient().server(name: "io.github.x/y")
            XCTFail("expected a 404 error")
        } catch {
            XCTAssertEqual(error as? MCPServersAPIError,
                           .server(404, "server \"io.github.x/y\" not found in registry"))
        }
    }

    func testRegistryUpstreamFailureIsMapped() async {
        StubURLProtocol.registry.setHandler { _ in
            .status(502, json: #"{"error":"registry unavailable"}"#)
        }

        do {
            _ = try await makeClient().search(query: "x")
            XCTFail("expected a 502 error")
        } catch {
            XCTAssertEqual(error as? MCPServersAPIError, .server(502, "registry unavailable"))
        }
    }
}
