@testable import MemoryConnector
import XCTest

/// Claude-config import behaviour of `MCPServersStore` against a stubbed
/// client. Kept in its own file so the main store test suite stays within the
/// SwiftLint length budget.
final class MCPServersStoreImportTests: XCTestCase {
    private let listJSON = """
    [{"name":"filesystem","transport":"stdio","enabled":true,"connected":true,"toolCount":2}]
    """

    private let statusJSON = """
    {"instanceID":"inst-1","serverURL":"https://api.example.test","connected":true,
     "toolCount":2,"servers":[{"name":"filesystem","transport":"stdio","enabled":true,
     "connected":true,"toolCount":2}]}
    """

    private let importJSON = """
    {"created":["github"],"updated":[],"skipped":[],"failed":[],"dryRun":true}
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

    @MainActor
    func testImportClaudeConfigStoresReportAndReloads() async {
        StubURLProtocol.registry.setHandler { request in
            switch (request.httpMethod ?? "", request.url?.path ?? "") {
            case ("POST", "/api/mcp-servers/import"): .ok(self.importJSON)
            case ("GET", "/api/mcp-servers"): .ok(self.listJSON)
            case ("GET", "/api/status"): .ok(self.statusJSON)
            default: .status(404)
            }
        }
        let store = makeStore()

        let succeeded = await store.importClaudeConfig(
            config: Data(#"{"mcpServers":{"github":{"command":"npx"}}}"#.utf8),
            overwrite: false,
            dryRun: true
        )

        XCTAssertTrue(succeeded)
        XCTAssertEqual(store.importResult?.created, ["github"])
        XCTAssertEqual(store.importResult?.dryRun, true)
        XCTAssertEqual(store.servers.map(\.name), ["filesystem"])
    }

    @MainActor
    func testImportClaudeConfigRecordsFailure() async {
        StubURLProtocol.registry.setHandler { _ in .status(500, json: #"{"error":"boom"}"#) }
        let store = makeStore()

        let succeeded = await store.importClaudeConfig(config: Data("{}".utf8),
                                                       overwrite: false,
                                                       dryRun: false)

        XCTAssertFalse(succeeded)
        XCTAssertNotNil(store.errorMessage)
        XCTAssertNil(store.importResult)
    }
}
