import Combine
import Foundation

/// Aggregated data for the project Dashboard page.
///
/// Every part is optional (or empty) so the page can render whatever loaded;
/// `DashboardStore.load` fills the snapshot, tolerating individual endpoint
/// failures.
struct DashboardSnapshot: Equatable, Sendable {
    var project: ProjectDetail? = nil
    var objectCount: Int? = nil
    var relationshipCount: Int? = nil
    var schemaStats: SchemaStats? = nil
    /// Convenience copy of `project?.stats` (may be nil even when `project` is
    /// present, e.g. when `include_stats` returned no stats block).
    var projectStats: ProjectStats? = nil
    /// The project's real agent DEFINITIONS (not chat-session instances).
    var agents: [AgentDefinitionSummary] = []
    /// Organisation display name resolved by matching `orgID` against
    /// `GET /api/orgs`. nil when the org id is missing, the org list is
    /// unavailable, or it has no matching entry — the UI then omits the org
    /// line rather than inventing a placeholder.
    var organizationName: String? = nil

    var projectName: String? { project?.name }
    var orgID: String? { project?.orgID }
    var projectInfo: String? { project?.projectInfo }

    /// Total object count, preferring the project stats block when present.
    var resolvedObjectCount: Int? { projectStats?.objectCount ?? objectCount }
    /// Total relationship count, preferring the project stats block.
    var resolvedRelationshipCount: Int? { projectStats?.relationshipCount ?? relationshipCount }
}

/// Loads the project Dashboard's data layer.
///
/// Drives `GET /api/projects/{id}?include_stats=true`,
/// `GET /api/graph/objects/count`, `GET /api/graph/relationships/count`,
/// `GET /api/schema-registry/projects/{id}/stats`, and
/// `GET /api/projects/{id}/agent-definitions` CONCURRENTLY with the user's OIDC
/// access token, plus a best-effort `GET /api/orgs` to resolve the project's
/// organisation name. Individual endpoint failures are tolerated (missing stat →
/// nil, agents → []); `.error` is only published when every request fails, so a
/// partially available server still renders a page.
@MainActor
final class DashboardStore: ObservableObject {

    enum State: Equatable {
        case idle
        case loading
        case loaded(DashboardSnapshot)
        case error(String)
    }

    @Published private(set) var state: State = .idle

    /// Builds a client for a (serverURL, accessToken) pair. Injectable so tests
    /// can supply a stubbed `URLSession` (or any client) without touching the
    /// network.
    private let clientFactory: @Sendable (String, String) -> MemoryAPIClient

    init(session: URLSession = .shared) {
        self.clientFactory = { serverURL, accessToken in
            MemoryAPIClient(serverURL: serverURL, token: accessToken, session: session)
        }
    }

    init(clientFactory: @escaping @Sendable (String, String) -> MemoryAPIClient) {
        self.clientFactory = clientFactory
    }

    /// Loads every dashboard part concurrently and publishes the aggregate.
    /// Failures are swallowed per part; the error state is set only when all
    /// five requests fail.
    func load(projectID: String, serverURL: String, accessToken: String) async {
        state = .loading
        let client = clientFactory(serverURL, accessToken)

        async let detailTask = try? client.projectDetail(projectID: projectID,
                                                         accessToken: accessToken)
        async let objectCountTask = try? client.objectCount(projectID: projectID,
                                                            accessToken: accessToken)
        async let relationshipCountTask = try? client.relationshipCount(projectID: projectID,
                                                                        accessToken: accessToken)
        async let schemaTask = try? client.schemaStats(projectID: projectID,
                                                       accessToken: accessToken)
        async let agentsTask = try? client.agentDefinitions(projectID: projectID,
                                                            accessToken: accessToken)
        // Best-effort org name lookup. A failure (or empty list) just leaves the
        // org line off the dashboard; it never fails the snapshot.
        async let orgsTask = try? client.orgs()

        let detail = await detailTask
        let objectCount = await objectCountTask
        let relationshipCount = await relationshipCountTask
        let schemaStats = await schemaTask
        let agents = await agentsTask
        let orgs = await orgsTask

        if detail == nil, objectCount == nil, relationshipCount == nil,
           schemaStats == nil, agents == nil {
            state = .error("Couldn't load the project dashboard.")
            return
        }

        state = .loaded(DashboardSnapshot(
            project: detail,
            objectCount: objectCount,
            relationshipCount: relationshipCount,
            schemaStats: schemaStats,
            projectStats: detail?.stats,
            agents: agents ?? [],
            organizationName: Self.resolveOrganizationName(orgID: detail?.orgID, orgs: orgs)
        ))
    }

    /// Matches the project's organisation id against the fetched org list.
    /// Returns nil when there is no org id, the org list failed to load, or no
    /// entry matches, so the dashboard omits the org line instead of showing a
    /// placeholder like "Unknown organisation".
    private static func resolveOrganizationName(orgID: String?, orgs: [OrgInfo]?) -> String? {
        guard let orgID = orgID?.trimmingCharacters(in: .whitespacesAndNewlines),
              !orgID.isEmpty,
              let orgs
        else { return nil }
        guard let match = orgs.first(where: { $0.id == orgID }),
              let name = match.name?.trimmingCharacters(in: .whitespacesAndNewlines),
              !name.isEmpty
        else { return nil }
        return name
    }

    /// Resets to `.idle` (e.g. when no project is selected).
    func clear() {
        state = .idle
    }
}
