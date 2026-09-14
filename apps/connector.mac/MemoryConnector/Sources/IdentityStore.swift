import Foundation

/// Loads and holds the connected project + user identity for the Project &
/// Account page. Wraps `MemoryAPIClient` with a UI-friendly state machine and
/// degrades to a clear unavailable state instead of surfacing raw errors.
@MainActor
final class IdentityStore: ObservableObject {

    enum State: Equatable {
        case idle
        case loading
        case loaded(IdentitySnapshot)
        case unavailable(String)

        var isLoading: Bool {
            if case .loading = self { return true }
            return false
        }
    }

    @Published private(set) var state: State = .idle

    private var loadTask: Task<Void, Never>?

    nonisolated init() {}

    /// Resets to the signed-out state (no identity).
    func reset() {
        loadTask?.cancel()
        loadTask = nil
        state = .idle
    }

    /// Loads identity for the given server/token. Empty server URL or token
    /// short-circuits to an unavailable state without hitting the network.
    func load(serverURL: String, token: String?) async {
        let base = serverURL.trimmingCharacters(in: .whitespacesAndNewlines)
        let token = token?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        guard !base.isEmpty, !token.isEmpty else {
            state = .unavailable("Sign in with Memory to see your project and account.")
            return
        }

        state = .loading
        let client = MemoryAPIClient(serverURL: base, token: token)
        do {
            let snapshot = try await client.identitySnapshot()
            guard !Task.isCancelled else { return }
            state = .loaded(snapshot)
        } catch {
            guard !Task.isCancelled else { return }
            state = .unavailable(Self.message(for: error))
        }
    }

    /// Fire-and-forget refresh for button/onChange triggers.
    func refresh(serverURL: String, token: String?) {
        loadTask?.cancel()
        let base = serverURL
        let token = token
        loadTask = Task { [weak self] in
            await self?.load(serverURL: base, token: token)
        }
    }

    // MARK: - Error copy

    private static func message(for error: Error) -> String {
        guard let api = error as? MemoryAPIError else {
            return error.localizedDescription
        }
        switch api {
        case .notConfigured:
            return "Sign in with Memory to see your project and account."
        case .authFailed:
            return "The server rejected this session. Sign in again to continue."
        case .unreachable(let detail):
            return "Could not reach the server. \(detail)"
        case .httpStatus(let code):
            return "The server returned HTTP \(code)."
        case .decoding(let detail):
            return "Could not read the server response. \(detail)"
        }
    }
}
