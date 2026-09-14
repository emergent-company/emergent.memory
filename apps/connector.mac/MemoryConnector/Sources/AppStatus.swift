import SwiftUI

/// Overall connector status shown in the menu bar.
///
/// Derived from `EngineManager.state` (process lifecycle) plus the
/// `StatusMonitor` snapshot (hub reachability). Replaces the earlier
/// placeholder status enum.
enum AppStatus: Equatable {
    case connecting
    case connected
    case disconnected
    case error(String)

    /// Derives the menu-bar status from engine + hub state.
    static func derive(engine: EngineManager.State, snapshot: StatusSnapshot?) -> AppStatus {
        switch engine {
        case .failed:
            return .error("Engine failed to stay running")
        case .stopped:
            return .disconnected
        case .starting, .restarting:
            return .connecting
        case .running:
            switch snapshot?.hubState {
            case .connected:
                return .connected
            case .authFailed:
                return .error("Authentication failed")
            case .notConnected, .unreachable, .missingConfig, .unknown, nil:
                return .disconnected
            }
        }
    }

    var label: String {
        switch self {
        case .connecting:
            return "Connecting…"
        case .connected:
            return "Connected"
        case .disconnected:
            return "Disconnected"
        case .error(let message):
            return message
        }
    }

    var symbolName: String {
        switch self {
        case .connecting:
            return "arrow.triangle.2.circlepath"
        case .connected:
            return "circle.inset.filled"
        case .disconnected:
            return "circle.dashed"
        case .error:
            return "exclamationmark.triangle"
        }
    }

    var color: Color {
        switch self {
        case .connecting:
            return .secondary
        case .connected:
            return .green
        case .disconnected:
            return .secondary
        case .error:
            return .orange
        }
    }
}
