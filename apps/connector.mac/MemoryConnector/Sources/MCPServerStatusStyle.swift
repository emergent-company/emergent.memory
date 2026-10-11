import SwiftUI

// MARK: - Shared MCP status affordances

/// Colored status dot, matching the footer/menu-bar palette: green connected,
/// orange connecting/starting, red error, gray disabled.
enum MCPStatusStyle {
    static func color(enabled: Bool, connected: Bool, error: String?) -> Color {
        if !enabled {
            return .secondary
        }
        if connected {
            return .green
        }
        if let error, !error.isEmpty {
            return .red
        }
        return .orange
    }

    static func label(enabled: Bool, connected: Bool, error: String?) -> LocalizedStringResource {
        if !enabled {
            return "Disabled"
        }
        if connected {
            return "Connected"
        }
        if let error, !error.isEmpty {
            return "Error"
        }
        return "Not connected"
    }

    /// Status line for the detail sheet: `label`, but with the server's error
    /// text inlined and a loading placeholder until the detail arrives.
    static func detailLabel(enabled: Bool, connected: Bool, error: String?,
                            loading: Bool = false) -> LocalizedStringResource
    {
        if loading {
            return "Loading…"
        }
        if !enabled {
            return "Disabled"
        }
        if let error, !error.isEmpty {
            return "Error: \(error)"
        }
        return connected ? "Connected" : "Not connected"
    }
}

struct MCPStatusDot: View {
    let color: Color

    var body: some View {
        Circle()
            .fill(color)
            .frame(width: 8, height: 8)
    }
}

/// Small capsule showing a server's transport (`stdio` / `http` / `sse`).
struct MCPTransportBadge: View {
    let transport: MCPTransport

    var body: some View {
        Text(transport.displayName)
            .font(.caption2.weight(.semibold))
            .foregroundStyle(.secondary)
            .padding(.horizontal, 6)
            .padding(.vertical, 2)
            .background(
                Capsule().fill(.quaternary.opacity(0.6))
            )
    }
}

/// Small capsule marking a server as configured in Memory. Such servers are
/// read-only locally and are edited in Memory, not in this app.
struct MCPManagedBadge: View {
    var body: some View {
        Text("Managed")
            .font(.caption2.weight(.semibold))
            .foregroundStyle(.secondary)
            .padding(.horizontal, 6)
            .padding(.vertical, 2)
            .background(
                Capsule().fill(.quaternary.opacity(0.6))
            )
            .help("Managed in Memory. Edit this server in Memory, not here.")
    }
}
