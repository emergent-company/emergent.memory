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
            .error("Engine failed to stay running")
        case .stopped:
            .disconnected
        case .starting, .restarting:
            .connecting
        case .running:
            switch snapshot?.hubState {
            case .connected:
                .connected
            case .authFailed:
                .error("Authentication failed")
            case .notConnected, .unreachable, .missingConfig, .unknown, nil:
                .disconnected
            }
        }
    }

    var label: String {
        switch self {
        case .connecting:
            "Connecting…"
        case .connected:
            "Connected"
        case .disconnected:
            "Disconnected"
        case let .error(message):
            message
        }
    }

    var symbolName: String {
        switch self {
        case .connecting:
            "arrow.triangle.2.circlepath"
        case .connected:
            "circle.inset.filled"
        case .disconnected:
            "circle.dashed"
        case .error:
            "exclamationmark.triangle"
        }
    }

    var color: Color {
        switch self {
        case .connecting:
            .secondary
        case .connected:
            .green
        case .disconnected:
            .secondary
        case .error:
            .orange
        }
    }
}
