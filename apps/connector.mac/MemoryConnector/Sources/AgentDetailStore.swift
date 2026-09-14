import Combine
import Foundation

/// Loads one agent's FULL definition for the read-only detail sheet.
///
/// The list endpoint (`agentDefinitions`) returns only a summary with
/// `toolCount`; the actual `tools` whitelist (plus banned tools, skills,
/// native model tools, and workspace tools) lives on
/// `GET /agent-definitions/{id}`. This store owns that single fetch and its
/// loading/error states so the sheet can render them and retry.
@MainActor
final class AgentDetailStore: ObservableObject {

    enum State: Equatable {
        case idle
        case loading
        case loaded(AgentDefinitionDetail)
        case error(String)
    }

    @Published private(set) var state: State = .idle

    /// Builds a client for a (serverURL, accessToken) pair. Injectable so tests
    /// can supply a stubbed `URLSession` without touching the network.
    private let clientFactory: @Sendable (String, String) -> MemoryAPIClient

    init(session: URLSession = .shared) {
        self.clientFactory = { serverURL, accessToken in
            MemoryAPIClient(serverURL: serverURL, token: accessToken, session: session)
        }
    }

    init(clientFactory: @escaping @Sendable (String, String) -> MemoryAPIClient) {
        self.clientFactory = clientFactory
    }

    /// Fetches the full definition. A missing `data` payload is an error (the
    /// agent vanished between the list load and the tap).
    func load(projectID: String, id: String, serverURL: String, accessToken: String) async {
        state = .loading
        let client = clientFactory(serverURL, accessToken)
        do {
            let detail = try await client.agentDefinition(projectID: projectID,
                                                           id: id,
                                                           accessToken: accessToken)
            guard let detail else {
                state = .error("This agent definition could not be found.")
                return
            }
            state = .loaded(detail)
        } catch {
            state = .error(error.localizedDescription)
        }
    }

    /// Records a failure raised before the request could start (e.g. no access
    /// token), so the sheet shows the error state and a Retry.
    func fail(_ message: String) {
        state = .error(message)
    }

    /// Resets to `.idle` (used when the sheet is presented fresh).
    func clear() {
        state = .idle
    }
}
