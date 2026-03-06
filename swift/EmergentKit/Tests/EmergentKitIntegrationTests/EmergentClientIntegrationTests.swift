import XCTest
@testable import EmergentKit

/// End-to-end integration tests that exercise the full Go → C → Swift pipeline.
///
/// **Requirements to run:**
/// - A compiled `EmergentGoCore.xcframework` (run `task server:swift:xcframework`)
/// - A live Emergent server reachable at the URL set in the environment variable
///   `EMERGENT_TEST_SERVER_URL` (default: `http://localhost:9090`)
/// - A valid API key in `EMERGENT_TEST_API_KEY`
///
/// These tests are intentionally in a *separate* target so standard `swift test`
/// runs (unit tests only) do not fail in environments without a live server.
final class EmergentClientIntegrationTests: XCTestCase {

    var client: EmergentClient!

    override func setUp() async throws {
        let serverURL = ProcessInfo.processInfo.environment["EMERGENT_TEST_SERVER_URL"]
            ?? "http://localhost:9090"
        let apiKey = ProcessInfo.processInfo.environment["EMERGENT_TEST_API_KEY"]
            ?? ""

        guard !apiKey.isEmpty else {
            throw XCTSkip("EMERGENT_TEST_API_KEY not set — skipping integration tests")
        }

        client = try await EmergentClient(config: EmergentConfig(
            serverURL: serverURL,
            authMode: "apikey",
            apiKey: apiKey
        ))
    }

    override func tearDown() async throws {
        client = nil
    }

    // MARK: - Health

    func testHealthCheckReturnsStatus() async throws {
        let health = try await client.health()
        XCTAssertFalse(health.status.isEmpty, "health.status should not be empty")
    }

    // MARK: - Ping (via E2E — verifies CreateClient + Ping + FreeString + FreeClient)

    // Note: Ping is tested at the Go layer (bridge_test.go). The Swift layer
    // exercises the full lifecycle via health(), which uses the same bridge path.

    // MARK: - Search

    func testSearchReturnsResults() async throws {
        let resp = try await client.search(SearchRequest(query: "test", limit: 5))
        // Server may return 0 results on a fresh install — just verify the type.
        XCTAssertNotNil(resp.results)
    }

    // MARK: - List Documents

    func testListDocumentsReturnsResponse() async throws {
        let resp = try await client.listDocuments(ListDocumentsRequest(limit: 5))
        XCTAssertGreaterThanOrEqual(resp.total, 0)
    }

    // MARK: - Context

    func testSetContextDoesNotThrow() async throws {
        // We just verify no error is thrown; the context is used in subsequent calls.
        try await client.setContext(orgID: "test-org", projectID: "test-project")
    }
}
