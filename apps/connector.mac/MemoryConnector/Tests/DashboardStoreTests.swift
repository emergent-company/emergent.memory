import XCTest
@testable import MemoryConnector

final class DashboardStoreTests: XCTestCase {

    private let baseURL = "https://api.example.test"
    private let accessToken = "user-access"
    private let projectID = "proj-1"

    private let detailJSON = """
    {"id":"proj-1","name":"Memory Dev","orgId":"org-1","project_info":"notes",
     "stats":{"documentCount":1,"objectCount":2,"relationshipCount":3,
              "totalJobs":4,"runningJobs":5,"queuedJobs":6}}
    """

    private let schemaJSON = """
    {"total_types":5,"enabled_types":3,"template_types":1,"custom_types":2,
     "discovered_types":0,"total_objects":120,"types_with_objects":4}
    """

    private let agentsJSON = """
    {"success":true,"data":[
      {"id":"a1","name":"Extractor","enabled":true,"description":"pulls facts"}
    ]}
    """

    override func setUp() {
        super.setUp()
        StubURLProtocol.registry.reset()
    }

    override func tearDown() {
        StubURLProtocol.registry.reset()
        super.tearDown()
    }

    /// Handler serving every dashboard endpoint with success payloads.
    private func successHandler() -> (URLRequest) -> StubResult {
        { request in
            switch request.url?.path {
            case "/api/projects/proj-1": return .ok(self.detailJSON)
            case "/api/graph/objects/count": return .ok(#"{"count":2}"#)
            case "/api/graph/relationships/count": return .ok(#"{"count":3}"#)
            case "/api/schema-registry/projects/proj-1/stats": return .ok(self.schemaJSON)
            case "/api/projects/proj-1/agent-definitions": return .ok(self.agentsJSON)
            default: return .status(404)
            }
        }
    }

    @MainActor
    private func makeStore() -> DashboardStore {
        DashboardStore(session: StubURLProtocol.makeSession())
    }

    // MARK: - Aggregation

    @MainActor
    func testLoadAggregatesAllParts() async {
        StubURLProtocol.registry.setHandler(successHandler())
        let store = makeStore()

        await store.load(projectID: projectID, serverURL: baseURL, accessToken: accessToken)

        guard case .loaded(let snapshot) = store.state else {
            return XCTFail("expected loaded, got \(store.state)")
        }
        XCTAssertEqual(snapshot.projectName, "Memory Dev")
        XCTAssertEqual(snapshot.orgID, "org-1")
        XCTAssertEqual(snapshot.projectInfo, "notes")
        XCTAssertEqual(snapshot.objectCount, 2)
        XCTAssertEqual(snapshot.relationshipCount, 3)
        XCTAssertEqual(snapshot.schemaStats?.totalTypes, 5)
        XCTAssertEqual(snapshot.schemaStats?.totalObjects, 120)
        XCTAssertEqual(snapshot.projectStats?.documentCount, 1)
        XCTAssertEqual(snapshot.projectStats?.runningJobs, 5)
        XCTAssertEqual(snapshot.agents.count, 1)
        XCTAssertEqual(snapshot.agents.first?.name, "Extractor")
        XCTAssertEqual(snapshot.resolvedObjectCount, 2)
        XCTAssertEqual(snapshot.resolvedRelationshipCount, 3)
    }

    @MainActor
    func testLoadSendsAuthAndProjectHeadersOnEveryRequest() async {
        StubURLProtocol.registry.setHandler(successHandler())

        await makeStore().load(projectID: projectID, serverURL: baseURL, accessToken: accessToken)

        let requests = StubURLProtocol.registry.capturedRequests
        XCTAssertEqual(requests.count, 6)
        let paths = Set(requests.compactMap { $0.url?.path })
        XCTAssertEqual(paths, [
            "/api/projects/proj-1",
            "/api/graph/objects/count",
            "/api/graph/relationships/count",
            "/api/schema-registry/projects/proj-1/stats",
            "/api/projects/proj-1/agent-definitions",
            "/api/orgs",
        ])
        for request in requests {
            XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer \(accessToken)")
        }
        // Every dashboard part is project-scoped; the org identity lookup is not.
        for request in requests where request.url?.path != "/api/orgs" {
            XCTAssertEqual(request.value(forHTTPHeaderField: "X-Project-ID"), projectID)
        }
    }

    /// Deterministic replacement for the old timing-based overlap check: one
    /// `load` must issue exactly one GET per dashboard endpoint, each carrying
    /// the auth + project headers, and aggregate the results into the snapshot.
    /// (Overlapping `URLProtocol.startLoading` is not observable here — the stub
    /// serialises dispatch — so concurrency is asserted structurally, not by
    /// wall-clock overlap.)
    @MainActor
    func testLoadIssuesEachDashboardRequestOnce() async {
        StubURLProtocol.registry.setHandler(successHandler())

        let store = makeStore()
        await store.load(projectID: projectID, serverURL: baseURL, accessToken: accessToken)

        let requests = StubURLProtocol.registry.capturedRequests
        XCTAssertEqual(requests.count, 6, "one request per dashboard part plus the org lookup")
        for request in requests {
            XCTAssertEqual(request.httpMethod, "GET")
            XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer \(accessToken)")
        }
        for request in requests where request.url?.path != "/api/orgs" {
            XCTAssertEqual(request.value(forHTTPHeaderField: "X-Project-ID"), projectID)
        }
        let paths = requests.compactMap { $0.url?.path }
        XCTAssertEqual(paths.count, 6)
        XCTAssertEqual(Set(paths), [
            "/api/graph/objects/count",
            "/api/graph/relationships/count",
            "/api/schema-registry/projects/proj-1/stats",
            "/api/projects/proj-1/agent-definitions",
            "/api/projects/proj-1",
            "/api/orgs",
        ])
        // The project-detail request carries the stats query parameter.
        let detail = requests.first { $0.url?.path == "/api/projects/proj-1" }
        XCTAssertEqual(detail?.url?.query, "include_stats=true")

        guard case .loaded(let snapshot) = store.state else {
            return XCTFail("expected loaded, got \(store.state)")
        }
        XCTAssertEqual(snapshot.objectCount, 2)
        XCTAssertEqual(snapshot.relationshipCount, 3)
        XCTAssertEqual(snapshot.schemaStats?.totalObjects, 120)
        XCTAssertEqual(snapshot.projectStats?.objectCount, 2)
        XCTAssertEqual(snapshot.agents.first?.id, "a1")
    }

    // MARK: - Organisation resolution

    private let orgsJSON = """
    [{"id":"org-1","name":"Maciej Kucharz's Org"},{"id":"org-2","name":"Second Org"}]
    """

    /// Project detail with no `orgId` (and no camelCase variant).
    private let detailNoOrgJSON = """
    {"id":"proj-1","name":"Memory Dev","project_info":"notes",
     "stats":{"documentCount":1,"objectCount":2,"relationshipCount":3,
              "totalJobs":4,"runningJobs":5,"queuedJobs":6}}
    """

    private func handler(detail: String,
                         orgs: StubResult) -> (URLRequest) -> StubResult {
        { request in
            switch request.url?.path {
            case "/api/projects/proj-1": return .ok(detail)
            case "/api/graph/objects/count": return .ok(#"{"count":2}"#)
            case "/api/graph/relationships/count": return .ok(#"{"count":3}"#)
            case "/api/schema-registry/projects/proj-1/stats": return .ok(self.schemaJSON)
            case "/api/projects/proj-1/agent-definitions": return .ok(self.agentsJSON)
            case "/api/orgs": return orgs
            default: return .status(404)
            }
        }
    }

    @MainActor
    func testLoadResolvesOrganizationNameFromOrgs() async {
        StubURLProtocol.registry.setHandler(handler(detail: detailJSON, orgs: .ok(orgsJSON)))

        let store = makeStore()
        await store.load(projectID: projectID, serverURL: baseURL, accessToken: accessToken)

        guard case .loaded(let snapshot) = store.state else {
            return XCTFail("expected loaded, got \(store.state)")
        }
        XCTAssertEqual(snapshot.orgID, "org-1")
        XCTAssertEqual(snapshot.organizationName, "Maciej Kucharz's Org")
    }

    @MainActor
    func testLoadOrganizationNameNilWhenOrgsEmpty() async {
        StubURLProtocol.registry.setHandler(handler(detail: detailJSON, orgs: .ok("[]")))

        let store = makeStore()
        await store.load(projectID: projectID, serverURL: baseURL, accessToken: accessToken)

        guard case .loaded(let snapshot) = store.state else {
            return XCTFail("expected loaded, got \(store.state)")
        }
        XCTAssertEqual(snapshot.orgID, "org-1")
        XCTAssertNil(snapshot.organizationName)
    }

    @MainActor
    func testLoadOrganizationNameNilWhenOrgsFail() async {
        StubURLProtocol.registry.setHandler(handler(detail: detailJSON,
                                                     orgs: .status(500)))

        let store = makeStore()
        await store.load(projectID: projectID, serverURL: baseURL, accessToken: accessToken)

        guard case .loaded(let snapshot) = store.state else {
            return XCTFail("expected loaded, got \(store.state)")
        }
        XCTAssertNil(snapshot.organizationName,
                     "a failed org lookup must not fail or fabricate the dashboard")
    }

    @MainActor
    func testLoadOrganizationNameNilWhenNoOrgID() async {
        StubURLProtocol.registry.setHandler(handler(detail: detailNoOrgJSON,
                                                     orgs: .ok(orgsJSON)))

        let store = makeStore()
        await store.load(projectID: projectID, serverURL: baseURL, accessToken: accessToken)

        guard case .loaded(let snapshot) = store.state else {
            return XCTFail("expected loaded, got \(store.state)")
        }
        XCTAssertNil(snapshot.orgID)
        XCTAssertNil(snapshot.organizationName)
    }

    // MARK: - Partial failure tolerance

    @MainActor
    func testLoadToleratesIndividualFailures() async {
        StubURLProtocol.registry.setHandler { request in
            switch request.url?.path {
            case "/api/projects/proj-1": return .ok(self.detailJSON)
            case "/api/graph/objects/count": return .status(500)
            case "/api/graph/relationships/count": return .ok(#"{"count":3}"#)
            case "/api/schema-registry/projects/proj-1/stats": return .status(500)
            case "/api/projects/proj-1/agent-definitions": return .status(500)
            default: return .status(404)
            }
        }

        let store = makeStore()
        await store.load(projectID: projectID, serverURL: baseURL, accessToken: accessToken)

        guard case .loaded(let snapshot) = store.state else {
            return XCTFail("expected loaded, got \(store.state)")
        }
        XCTAssertEqual(snapshot.projectName, "Memory Dev")
        XCTAssertNil(snapshot.objectCount, "failed stat should be nil")
        XCTAssertNil(snapshot.schemaStats, "failed stat should be nil")
        XCTAssertEqual(snapshot.relationshipCount, 3)
        XCTAssertEqual(snapshot.agents, [], "failed agents should be empty")
        // Project stats still supply the object count on the loaded page.
        XCTAssertEqual(snapshot.resolvedObjectCount, 2)
    }

    @MainActor
    func testLoadDefinitionsFailureYieldsEmptyAgentsButStaysLoaded() async {
        StubURLProtocol.registry.setHandler { request in
            switch request.url?.path {
            case "/api/projects/proj-1": return .ok(self.detailJSON)
            case "/api/graph/objects/count": return .ok(#"{"count":2}"#)
            case "/api/graph/relationships/count": return .ok(#"{"count":3}"#)
            case "/api/schema-registry/projects/proj-1/stats": return .ok(self.schemaJSON)
            case "/api/projects/proj-1/agent-definitions": return .status(500)
            default: return .status(404)
            }
        }

        let store = makeStore()
        await store.load(projectID: projectID, serverURL: baseURL, accessToken: accessToken)

        guard case .loaded(let snapshot) = store.state else {
            return XCTFail("expected loaded, got \(store.state)")
        }
        XCTAssertEqual(snapshot.agents, [], "definitions failure should not fail the snapshot")
        XCTAssertEqual(snapshot.projectName, "Memory Dev")
    }

    /// The agents card must show real agent-decision NAMES — never a
    /// chat-session instance label like "Chat session for operator".
    @MainActor
    func testLoadAgentsComeFromDefinitionsNotChatSessions() async {
        let definitions = """
        {"success":true,"data":[
          {"id":"d1","name":"Researcher","enabled":true},
          {"id":"d2","name":"Summarizer","enabled":false}
        ]}
        """
        StubURLProtocol.registry.setHandler { request in
            switch request.url?.path {
            case "/api/projects/proj-1": return .ok(self.detailJSON)
            case "/api/graph/objects/count": return .ok(#"{"count":2}"#)
            case "/api/graph/relationships/count": return .ok(#"{"count":3}"#)
            case "/api/schema-registry/projects/proj-1/stats": return .ok(self.schemaJSON)
            case "/api/projects/proj-1/agent-definitions": return .ok(definitions)
            default: return .status(404)
            }
        }

        let store = makeStore()
        await store.load(projectID: projectID, serverURL: baseURL, accessToken: accessToken)

        guard case .loaded(let snapshot) = store.state else {
            return XCTFail("expected loaded, got \(store.state)")
        }
        XCTAssertEqual(snapshot.agents.map(\.name), ["Researcher", "Summarizer"])
        XCTAssertFalse(snapshot.agents.contains { $0.name.hasPrefix("Chat session") })
        let paths = StubURLProtocol.registry.capturedRequests.compactMap { $0.url?.path }
        XCTAssertTrue(paths.contains("/api/projects/proj-1/agent-definitions"))
        XCTAssertFalse(paths.contains("/api/projects/proj-1/agents"),
                       "dashboard must not call the chat-session agents endpoint")
    }

    @MainActor
    func testLoadUsesEmptyAgentsWhenDefinitionsEnvelopeHasNoData() async {
        StubURLProtocol.registry.setHandler { request in
            switch request.url?.path {
            case "/api/projects/proj-1": return .ok(self.detailJSON)
            case "/api/graph/objects/count": return .ok(#"{"count":2}"#)
            case "/api/graph/relationships/count": return .ok(#"{"count":3}"#)
            case "/api/schema-registry/projects/proj-1/stats": return .ok(self.schemaJSON)
            case "/api/projects/proj-1/agent-definitions": return .ok(#"{"success":true}"#)
            default: return .status(404)
            }
        }

        let store = makeStore()
        await store.load(projectID: projectID, serverURL: baseURL, accessToken: accessToken)

        guard case .loaded(let snapshot) = store.state else {
            return XCTFail("expected loaded, got \(store.state)")
        }
        XCTAssertEqual(snapshot.agents, [])
    }

    // MARK: - Total failure

    @MainActor
    func testLoadErrorsOnlyWhenEverythingFails() async {
        StubURLProtocol.registry.setHandler { _ in .status(500) }

        let store = makeStore()
        await store.load(projectID: projectID, serverURL: baseURL, accessToken: accessToken)

        guard case .error = store.state else {
            return XCTFail("expected error, got \(store.state)")
        }
    }

    // MARK: - Clear

    @MainActor
    func testClearResetsToIdle() async {
        StubURLProtocol.registry.setHandler(successHandler())
        let store = makeStore()
        await store.load(projectID: projectID, serverURL: baseURL, accessToken: accessToken)

        store.clear()

        XCTAssertEqual(store.state, .idle)
    }
}
