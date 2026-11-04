@testable import MemoryConnector
import XCTest

/// State transitions for `MCPRegistryBrowseModel` against a stubbed client:
/// success, empty, search failure, and detail failure.
final class MCPRegistryBrowseModelTests: XCTestCase {
    private let baseURL = "http://127.0.0.1:8890"

    private let searchJSON = """
    {"servers":[{"name":"io.github.example/one","packages":[],"remotes":[],
      "envVars":[],"hasPackages":false,"hasRemotes":false}],
     "nextCursor":"","count":1}
    """

    private let emptyJSON = #"{"servers":[],"nextCursor":"","count":0}"#

    private let detailJSON = """
    {"server":{"name":"io.github.example/one","packages":[],"remotes":[],"envVars":[]},
     "suggested":{"name":"one","transport":"stdio","enabled":true,"command":"npx","args":["-y","one"]},
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

    @MainActor
    private func makeModel() -> MCPRegistryBrowseModel {
        MCPRegistryBrowseModel(client: MCPRegistryAPIClient(
            baseURL: baseURL,
            session: StubURLProtocol.makeSession()
        ))
    }

    @MainActor
    func testSearchPopulatesLoadedState() async {
        StubURLProtocol.registry.setHandler { _ in .ok(self.searchJSON) }
        let model = makeModel()

        await model.search()

        XCTAssertEqual(model.phase, .loaded)
        XCTAssertEqual(model.results.map(\.name), ["io.github.example/one"])
        XCTAssertNil(model.errorMessage)
    }

    @MainActor
    func testSearchEmptyResultSetsEmptyState() async {
        StubURLProtocol.registry.setHandler { _ in .ok(self.emptyJSON) }
        let model = makeModel()

        await model.search()

        XCTAssertEqual(model.phase, .empty)
        XCTAssertTrue(model.results.isEmpty)
        XCTAssertNil(model.errorMessage)
    }

    @MainActor
    func testSearchFailureSetsErrorState() async {
        StubURLProtocol.registry.setHandler { _ in .failure(URLError(.cannotConnectToHost)) }
        let model = makeModel()

        await model.search()

        XCTAssertEqual(model.phase, .error)
        XCTAssertNotNil(model.errorMessage)
    }

    @MainActor
    func testDetailFailureReturnsNilAndRecordsError() async {
        StubURLProtocol.registry.setHandler { _ in
            .status(502, json: #"{"error":"registry unavailable"}"#)
        }
        let model = makeModel()

        let detail = await model.detail(name: "io.github.example/one")

        XCTAssertNil(detail)
        XCTAssertEqual(model.errorMessage, "registry unavailable")
    }

    // MARK: - Project scoping

    @MainActor
    func testSetPortRoutesSearchToActiveProjectPort() async {
        StubURLProtocol.registry.setHandler { _ in .ok(self.searchJSON) }
        let model = makeModel()

        model.setPort(9012)
        await model.search()

        XCTAssertEqual(StubURLProtocol.registry.capturedRequest?.url?.port, 9012,
                       "the registry client targets the active project's management port")
        XCTAssertEqual(StubURLProtocol.registry.capturedRequest?.url?.host, "127.0.0.1")
    }

    @MainActor
    func testSetPortRoutesDetailToActiveProjectPort() async {
        StubURLProtocol.registry.setHandler { _ in .ok(self.detailJSON) }
        let model = makeModel()

        model.setPort(9013)
        _ = await model.detail(name: "io.github.example/one")

        XCTAssertEqual(StubURLProtocol.registry.capturedRequest?.url?.port, 9013)
    }

    @MainActor
    func testSetPortNilFallsBackToDefaultPort() async {
        StubURLProtocol.registry.setHandler { _ in .ok(self.searchJSON) }
        let model = makeModel()

        model.setPort(9012)
        model.setPort(nil)
        await model.search()

        XCTAssertEqual(StubURLProtocol.registry.capturedRequest?.url?.port, 8931,
                       "a nil active port falls back to the default management port")
    }
}
