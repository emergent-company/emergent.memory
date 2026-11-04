@testable import MemoryConnector
import XCTest

/// Tolerant decoding of the `managed` flag (defaulting to `false` when absent),
/// across the list summary, status summary, and detail responses.
final class MCPServersManagedDecodingTests: XCTestCase {
    func testSummaryDefaultsManagedFalseWhenAbsent() throws {
        let json = """
        [{"name":"x","transport":"stdio","enabled":true,"connected":false,"toolCount":0}]
        """
        let servers = try JSONDecoder().decode([HostedMCPServer].self, from: Data(json.utf8))
        XCTAssertFalse(servers[0].managed)
    }

    func testSummaryDecodesManagedTrue() throws {
        let json = """
        [{"name":"x","transport":"http","enabled":true,"connected":true,"toolCount":1,"managed":true}]
        """
        let servers = try JSONDecoder().decode([HostedMCPServer].self, from: Data(json.utf8))
        XCTAssertTrue(servers[0].managed)
    }

    func testDetailDefaultsManagedFalseWhenAbsent() throws {
        let json = """
        {"name":"x","transport":"stdio","enabled":true,"command":"/usr/bin/mcp",
         "status":{"connected":false,"toolCount":0,"tools":[]}}
        """
        let detail = try JSONDecoder().decode(HostedMCPServerDetail.self, from: Data(json.utf8))
        XCTAssertFalse(detail.managed)
        XCTAssertEqual(detail.config.name, "x")
    }

    func testDetailDecodesManagedTrue() throws {
        let json = """
        {"name":"x","transport":"http","enabled":true,"url":"https://example.com/mcp","managed":true,
         "status":{"connected":true,"toolCount":2,"tools":[]}}
        """
        let detail = try JSONDecoder().decode(HostedMCPServerDetail.self, from: Data(json.utf8))
        XCTAssertTrue(detail.managed)
        XCTAssertEqual(detail.config.url, "https://example.com/mcp")
    }
}

/// Store behaviour for Memory-managed servers: the flag is surfaced, managed
/// names stay reserved, and every local mutation is refused without any write.
final class MCPServersManagedStoreTests: XCTestCase {
    private let listJSON = """
    [{"name":"local","transport":"stdio","enabled":true,"connected":true,"toolCount":1,
      "managed":false},
     {"name":"team-search","transport":"http","enabled":true,"connected":true,"toolCount":3,
      "managed":true}]
    """

    private let detailJSON = """
    {"name":"team-search","transport":"http","enabled":true,"url":"https://example.com/mcp",
     "managed":true,"status":{"connected":true,"toolCount":1,"tools":[]}}
    """

    override func setUp() {
        super.setUp()
        StubURLProtocol.registry.reset()
    }

    override func tearDown() {
        StubURLProtocol.registry.reset()
        super.tearDown()
    }

    @MainActor
    private func makeStore() -> MCPServersStore {
        MCPServersStore(client: MCPServersAPIClient(
            baseURL: "http://127.0.0.1:8890",
            session: StubURLProtocol.makeSession()
        ))
    }

    /// Serves the list (the only request `load()` needs for these tests) and
    /// 404s everything else so an unexpected write fails loudly.
    private func listHandler() -> (URLRequest) -> StubResult {
        { request in
            switch (request.httpMethod ?? "", request.url?.path ?? "") {
            case ("GET", "/api/mcp-servers"): .ok(self.listJSON)
            default: .status(404)
            }
        }
    }

    private func teamSearchConfig() -> HostedMCPServerConfig {
        HostedMCPServerConfig(name: "team-search", transport: .http, url: "https://example.com/mcp")
    }

    @MainActor
    func testManagedFlagIsSurfacedAndNameReserved() async {
        StubURLProtocol.registry.setHandler(listHandler())
        let store = makeStore()

        await store.load()

        XCTAssertTrue(store.isManaged("team-search"))
        XCTAssertFalse(store.isManaged("local"))
        XCTAssertEqual(store.managedNames, ["team-search"])
        XCTAssertTrue(store.reservedNames.contains("team-search"),
                      "a managed name stays reserved for the Add form")
        XCTAssertTrue(store.reservedNames.contains("local"))
    }

    @MainActor
    func testIsManagedFallsBackToCachedDetail() async {
        StubURLProtocol.registry.setHandler { request in
            switch (request.httpMethod ?? "", request.url?.path ?? "") {
            case ("GET", "/api/mcp-servers"): .ok("[]")
            case ("GET", "/api/mcp-servers/team-search"): .ok(self.detailJSON)
            default: .status(404)
            }
        }
        let store = makeStore()

        let detail = await store.detail(name: "team-search")

        XCTAssertEqual(detail?.managed, true)
        XCTAssertTrue(store.isManaged("team-search"),
                      "a cached detail still reports the server as managed")
    }

    @MainActor
    func testManagedServerMutationsAreRefusedWithoutWrites() async {
        StubURLProtocol.registry.setHandler(listHandler())
        let store = makeStore()
        await store.load()

        let tool = HostedMCPTool(name: "team-search_search", description: "", inputSchema: nil)
        let updated = await store.update(name: "team-search", config: teamSearchConfig())
        let toggled = await store.setEnabled(name: "team-search", enabled: false)
        let deleted = await store.delete(name: "team-search")
        let shared = await store.setToolShared(server: "team-search", tool: tool, shared: false)

        XCTAssertFalse(updated)
        XCTAssertFalse(toggled)
        XCTAssertFalse(deleted)
        XCTAssertFalse(shared)
        XCTAssertEqual(store.errorMessage, "This server is managed in Memory. Edit it there instead.")
        let writes = StubURLProtocol.registry.capturedRequests.filter { $0.httpMethod != "GET" }
        XCTAssertTrue(writes.isEmpty, "managed servers are never written through the store")
    }

    @MainActor
    func testLocalServerIsStillMutable() async {
        let localDetailJSON = """
        {"name":"local","transport":"stdio","enabled":false,"command":"/usr/bin/mcp",
         "status":{"connected":true,"toolCount":1,"tools":[]}}
        """
        StubURLProtocol.registry.setHandler { request in
            switch (request.httpMethod ?? "", request.url?.path ?? "") {
            case ("GET", "/api/mcp-servers"): .ok(self.listJSON)
            case ("PUT", "/api/mcp-servers/local/enabled"): .ok(localDetailJSON)
            default: .status(404)
            }
        }
        let store = makeStore()
        await store.load()

        let toggled = await store.setEnabled(name: "local", enabled: false)

        XCTAssertTrue(toggled)
        XCTAssertNil(store.errorMessage)
    }
}
