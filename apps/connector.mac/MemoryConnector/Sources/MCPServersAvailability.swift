import Foundation

/// How available the connector's loopback management API is to the Hosted MCP
/// Servers page, derived from the engine lifecycle plus the connected project.
///
/// Deliberately free of any "engine"/"management API" vocabulary: the page
/// renders these as ordinary connection states, so internal detail is never
/// surfaced to the user.
enum MCPServersAvailability: Equatable {
    /// The connector is running and a project is connected: the page can load.
    case available
    /// The connector is starting or restarting.
    case connecting
    /// No project is connected, so the connector is intentionally stopped.
    case noConnectedProject
    /// A project is connected but the connector is stopped or failed.
    case unavailable

    /// Resolves the page state. `engineState` comes from the engine supervisor;
    /// `hasConnectedProject` from the project store.
    @MainActor
    static func resolve(engineState: EngineManager.State,
                        hasConnectedProject: Bool) -> MCPServersAvailability
    {
        switch engineState {
        case .running:
            hasConnectedProject ? .available : .noConnectedProject
        case .starting, .restarting:
            .connecting
        case .stopped:
            hasConnectedProject ? .unavailable : .noConnectedProject
        case .failed:
            .unavailable
        }
    }

    /// True only when the page may issue management-API requests.
    var isAvailable: Bool {
        self == .available
    }
}
