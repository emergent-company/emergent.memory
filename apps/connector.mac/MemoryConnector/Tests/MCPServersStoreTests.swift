import XCTest
@testable import MemoryConnector

/// Behaviour of `MCPServersStore` against a stubbed client: load, mutations,
/// tools, and error states. Hermetic — no real network.
final class MCPServersStoreTests: XCTestCase {

    private let listJSON = """
    [{"name":"filesystem","transport":"stdio","enabled":true,"connected":true,"toolCount":2}]
    """

    private lazy var detailJSON = """
    {"name":"filesystem","transport":"stdio","enabled":true,
     "command":"/usr/bin/mcp","args":["--root","/tmp"],
     "status":{"connected":true,"toolCount":2,"tools":[
       {"name":"filesystem_read","description":"Read a file","inputSchema":{}}]}}
    """

    private let toolsJSON = """
    [{"name":"filesystem_read","description":"Read a file","inputSchema":{}}]
    """

    private let statusJSON = """
    {"instanceID":"inst-1","serverURL":"https://api.example.test","connected":true,
     "toolCount":2,"servers":[{"name":"filesystem","transport":"stdio","enabled":true,
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

    @MainActor
    private func makeStore() -> MCPServersStore {
        MCPServersStore(client: MCPServersAPIClient(
            baseURL: "http://127.0.0.1:8890",
            session: StubURLProtocol.makeSession()
        ))
    }

    /// Serves every endpoint the store may hit.
    private func successHandler() -> (URLRequest) -> StubResult {
        { request in
            let method = request.httpMethod ?? ""
            let path = request.url?.path ?? ""
            switch (method, path) {
            case ("GET", "/api/mcp-servers"): return .ok(self.listJSON)
            case ("GET", "/api/status"): return .ok(self.statusJSON)
            case ("GET", "/api/mcp-servers/filesystem"): return .ok(self.detailJSON)
            case ("GET", "/api/mcp-servers/filesystem/tools"): return .ok(self.toolsJSON)
            case ("POST", "/api/mcp-servers"): return .status(201, json: self.detailJSON)
            case ("PUT", "/api/mcp-servers/filesystem/config"): return .ok(self.detailJSON)
            case ("PUT", "/api/mcp-servers/filesystem/enabled"): return .ok(self.detailJSON)
            case ("DELETE", "/api/mcp-servers/filesystem"): return .status(204)
            default: return .status(404)
            }
        }
    }

    // MARK: - Load

    @MainActor
    func testLoadPopulatesServersAndStatus() async {
        StubURLProtocol.registry.setHandler(successHandler())
        let store = makeStore()

        await store.load()

        XCTAssertEqual(store.phase, .loaded)
        XCTAssertEqual(store.servers.map(\.name), ["filesystem"])
        XCTAssertTrue(store.servers[0].connected)
        XCTAssertEqual(store.status?.instanceID, "inst-1")
        XCTAssertEqual(store.status?.toolCount, 2)
        XCTAssertNil(store.errorMessage)
        XCTAssertNotNil(store.lastUpdated)
    }

    @MainActor
    func testLoadFailureSetsErrorState() async {
        StubURLProtocol.registry.setHandler { _ in .failure(URLError(.cannotConnectToHost)) }
        let store = makeStore()

        await store.load()

        XCTAssertEqual(store.phase, .error)
        XCTAssertTrue(store.servers.isEmpty)
        XCTAssertNotNil(store.errorMessage)
    }

    @MainActor
    func testLoadKeepsExistingServersWhenRefreshFails() async {
        let handler = successHandler()
        StubURLProtocol.registry.setHandler(handler)
        let store = makeStore()
        await store.load()
        XCTAssertEqual(store.servers.count, 1)

        StubURLProtocol.registry.setHandler { _ in .failure(URLError(.cannotConnectToHost)) }
        await store.load()

        XCTAssertEqual(store.phase, .loaded, "an existing list survives a failed refresh")
        XCTAssertEqual(store.servers.count, 1)
        XCTAssertNotNil(store.errorMessage)
    }

    // MARK: - Create

    @MainActor
    func testCreatePostsAndRefreshes() async {
        StubURLProtocol.registry.setHandler(successHandler())
        let store = makeStore()

        let config = HostedMCPServerConfig(name: "filesystem",
                                           transport: .stdio,
                                           command: "/usr/bin/mcp")
        let succeeded = await store.create(config)

        XCTAssertTrue(succeeded)
        XCTAssertEqual(store.servers.map(\.name), ["filesystem"])
        XCTAssertNil(store.errorMessage)
        let posts = StubURLProtocol.registry.capturedRequests.filter { $0.httpMethod == "POST" }
        XCTAssertEqual(posts.count, 1)
        XCTAssertEqual(posts.first?.url?.path, "/api/mcp-servers")
    }

    @MainActor
    func testCreateFailureRecordsError() async {
        StubURLProtocol.registry.setHandler { request in
            if request.httpMethod == "POST" {
                return .status(409, json: #"{"error":"mcp server \"filesystem\" already exists"}"#)
            }
            return .status(500)
        }
        let store = makeStore()

        let succeeded = await store.create(HostedMCPServerConfig(name: "filesystem",
                                                                 transport: .stdio,
                                                                 command: "/usr/bin/mcp"))

        XCTAssertFalse(succeeded)
        XCTAssertEqual(store.errorMessage, "mcp server \"filesystem\" already exists")
    }

    // MARK: - Update / toggle / delete

    @MainActor
    func testUpdateCallsConfigEndpointAndRefreshes() async {
        StubURLProtocol.registry.setHandler(successHandler())
        let store = makeStore()

        let succeeded = await store.update(name: "filesystem",
                                           config: HostedMCPServerConfig(name: "filesystem",
                                                                         transport: .stdio,
                                                                         command: "/usr/bin/mcp"))

        XCTAssertTrue(succeeded)
        let puts = StubURLProtocol.registry.capturedRequests.filter { $0.httpMethod == "PUT" }
        XCTAssertEqual(puts.map { $0.url?.path }, ["/api/mcp-servers/filesystem/config"])
    }

    @MainActor
    func testSetEnabledCallsEnabledEndpoint() async {
        StubURLProtocol.registry.setHandler(successHandler())
        let store = makeStore()

        let succeeded = await store.setEnabled(name: "filesystem", enabled: false)

        XCTAssertTrue(succeeded)
        let puts = StubURLProtocol.registry.capturedRequests.filter { $0.httpMethod == "PUT" }
        XCTAssertEqual(puts.first?.url?.path, "/api/mcp-servers/filesystem/enabled")
    }

    @MainActor
    func testDeleteCallsDeleteEndpoint() async {
        StubURLProtocol.registry.setHandler(successHandler())
        let store = makeStore()

        let succeeded = await store.delete(name: "filesystem")

        XCTAssertTrue(succeeded)
        let deletes = StubURLProtocol.registry.capturedRequests.filter { $0.httpMethod == "DELETE" }
        XCTAssertEqual(deletes.first?.url?.path, "/api/mcp-servers/filesystem")
    }

    // MARK: - Tools / detail

    @MainActor
    func testFetchToolsReturnsDiscoveredTools() async {
        StubURLProtocol.registry.setHandler(successHandler())
        let store = makeStore()

        let tools = await store.fetchTools(name: "filesystem")

        XCTAssertEqual(tools.map(\.name), ["filesystem_read"])
        XCTAssertNil(store.errorMessage)
    }

    @MainActor
    func testFetchToolsFailureRecordsError() async {
        StubURLProtocol.registry.setHandler { _ in .status(500, json: #"{"error":"boom"}"#) }
        let store = makeStore()

        let tools = await store.fetchTools(name: "filesystem")

        XCTAssertTrue(tools.isEmpty)
        XCTAssertEqual(store.errorMessage, "boom")
    }

    @MainActor
    func testDetailReturnsConfigAndStatus() async {
        StubURLProtocol.registry.setHandler(successHandler())
        let store = makeStore()

        let detail = await store.detail(name: "filesystem")

        XCTAssertEqual(detail?.config.command, "/usr/bin/mcp")
        XCTAssertEqual(detail?.status.tools.count, 1)
    }

    // MARK: - Clear

    @MainActor
    func testClearResetsState() async {
        StubURLProtocol.registry.setHandler(successHandler())
        let store = makeStore()
        await store.load()

        store.clear()

        XCTAssertEqual(store.phase, .idle)
        XCTAssertTrue(store.servers.isEmpty)
        XCTAssertNil(store.status)
    }

    // MARK: - Per-tool sharing

    /// A full detail response with the given `disabled_tools`, plus the other
    /// fields a sharing write must preserve (command/args/env).
    private func serverDetailJSON(disabledTools: [String]) -> String {
        var fields = """
        "name":"filesystem","transport":"stdio","enabled":true,
        "command":"/usr/bin/mcp","args":["--root","/tmp"],"env":{"TOKEN":"s3cret"}
        """
        if !disabledTools.isEmpty {
            let list = disabledTools.map { "\"\($0)\"" }.joined(separator: ",")
            fields += ",\"disabled_tools\":[\(list)]"
        }
        return """
        {\(fields),
        "status":{"connected":true,"toolCount":2,"tools":[
          {"name":"filesystem_read","description":"Read a file","inputSchema":{}},
          {"name":"filesystem_write","description":"Write a file","inputSchema":{}}]}}
        """
    }

    /// Serves the detail with a fixed `disabled_tools` list, the list/status/
    /// tools refresh, and the config PUT. No state is mutated between calls.
    private func sharingHandler(disabledTools: [String]) -> (URLRequest) -> StubResult {
        { request in
            let method = request.httpMethod ?? ""
            let path = request.url?.path ?? ""
            switch (method, path) {
            case ("GET", "/api/mcp-servers"): return .ok(self.listJSON)
            case ("GET", "/api/status"): return .ok(self.statusJSON)
            case ("GET", "/api/mcp-servers/filesystem"):
                return .ok(self.serverDetailJSON(disabledTools: disabledTools))
            case ("GET", "/api/mcp-servers/filesystem/tools"): return .ok(self.toolsJSON)
            case ("PUT", "/api/mcp-servers/filesystem/config"):
                return .ok(self.serverDetailJSON(disabledTools: disabledTools))
            default: return .status(404)
            }
        }
    }

    private static func disabledTools(from request: URLRequest) -> [String]? {
        let body = HTTPBodyReader.string(from: request)
        guard let json = try? JSONSerialization.jsonObject(with: Data(body.utf8)) as? [String: Any] else {
            return nil
        }
        return json["disabled_tools"] as? [String]
    }

    private func readTool() -> HostedMCPTool {
        HostedMCPTool(name: "filesystem_read", description: "Read a file", inputSchema: nil)
    }

    @MainActor
    func testSetToolSharedAddsOwnNameAndPreservesFullConfig() async throws {
        StubURLProtocol.registry.setHandler(sharingHandler(disabledTools: ["delete_file"]))
        let store = makeStore()

        let succeeded = await store.setToolShared(server: "filesystem",
                                                  tool: readTool(),
                                                  shared: false)

        XCTAssertTrue(succeeded)
        XCTAssertNil(store.errorMessage)

        let requests = StubURLProtocol.registry.capturedRequests
        XCTAssertEqual(requests.first?.url?.path, "/api/mcp-servers/filesystem",
                       "the current config is fetched before the write")
        let put = try XCTUnwrap(requests.first { $0.httpMethod == "PUT" })
        XCTAssertEqual(put.url?.path, "/api/mcp-servers/filesystem/config")

        let body = HTTPBodyReader.string(from: put)
        let json = try XCTUnwrap(JSONSerialization.jsonObject(with: Data(body.utf8)) as? [String: Any])
        // The server's own (un-namespaced) tool name is added, not "filesystem_read".
        XCTAssertEqual(Set(json["disabled_tools"] as? [String] ?? []), ["delete_file", "read"])
        // Every other field is preserved.
        XCTAssertEqual(json["name"] as? String, "filesystem")
        XCTAssertEqual(json["transport"] as? String, "stdio")
        XCTAssertEqual(json["enabled"] as? Bool, true)
        XCTAssertEqual(json["command"] as? String, "/usr/bin/mcp")
        XCTAssertEqual(json["args"] as? [String], ["--root", "/tmp"])
        XCTAssertEqual(json["env"] as? [String: String], ["TOKEN": "s3cret"])

        // The tool set and detail are refreshed after the write.
        XCTAssertNotNil(store.details["filesystem"])
        XCTAssertNotNil(store.toolsByServer["filesystem"])
    }

    @MainActor
    func testSetToolSharedRemovesOwnName() async throws {
        StubURLProtocol.registry.setHandler(sharingHandler(disabledTools: ["read", "delete_file"]))
        let store = makeStore()

        let succeeded = await store.setToolShared(server: "filesystem",
                                                  tool: readTool(),
                                                  shared: true)

        XCTAssertTrue(succeeded)
        let put = try XCTUnwrap(StubURLProtocol.registry.capturedRequests.first { $0.httpMethod == "PUT" })
        XCTAssertEqual(Self.disabledTools(from: put), ["delete_file"])
    }

    @MainActor
    func testSetToolSharedTogglingBackRestores() async {
        let state = ServerDisabledState(["delete_file"])
        StubURLProtocol.registry.setHandler { request in
            let method = request.httpMethod ?? ""
            let path = request.url?.path ?? ""
            switch (method, path) {
            case ("GET", "/api/mcp-servers"): return .ok(self.listJSON)
            case ("GET", "/api/status"): return .ok(self.statusJSON)
            case ("GET", "/api/mcp-servers/filesystem"):
                return .ok(self.serverDetailJSON(disabledTools: state.value))
            case ("GET", "/api/mcp-servers/filesystem/tools"): return .ok(self.toolsJSON)
            case ("PUT", "/api/mcp-servers/filesystem/config"):
                if let updated = Self.disabledTools(from: request) { state.set(updated) }
                return .ok(self.serverDetailJSON(disabledTools: state.value))
            default: return .status(404)
            }
        }
        let store = makeStore()

        let unshared = await store.setToolShared(server: "filesystem", tool: readTool(), shared: false)
        XCTAssertTrue(unshared)
        XCTAssertEqual(state.value, ["delete_file", "read"])

        let reshared = await store.setToolShared(server: "filesystem", tool: readTool(), shared: true)
        XCTAssertTrue(reshared)
        XCTAssertEqual(state.value, ["delete_file"], "sharing the tool again removes it from disabled_tools")
    }

    @MainActor
    func testSetToolSharedConfigFetchFailureLeavesStateConsistent() async {
        StubURLProtocol.registry.setHandler { request in
            if request.url?.path == "/api/mcp-servers/filesystem" {
                return .status(500, json: #"{"error":"boom"}"#)
            }
            return .status(404)
        }
        let store = makeStore()

        let succeeded = await store.setToolShared(server: "filesystem",
                                                  tool: readTool(),
                                                  shared: false)

        XCTAssertFalse(succeeded)
        XCTAssertEqual(store.errorMessage, "boom")
        XCTAssertTrue(StubURLProtocol.registry.capturedRequests.allSatisfy { $0.httpMethod != "PUT" },
                      "no config is written when the current config cannot be fetched")
        XCTAssertNil(store.details["filesystem"])
        XCTAssertNil(store.toolsByServer["filesystem"])
    }
}

/// Mutable `disabled_tools` state behind a lock so a stub handler (which runs on
/// a URLSession thread) can update it while the test reads it on the main actor.
private final class ServerDisabledState: @unchecked Sendable {
    private let lock = NSLock()
    private var storage: [String]

    init(_ initial: [String]) { storage = initial }

    var value: [String] {
        lock.lock(); defer { lock.unlock() }
        return storage
    }

    func set(_ newValue: [String]) {
        lock.lock(); storage = newValue; lock.unlock()
    }
}
