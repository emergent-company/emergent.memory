import Foundation
import Observation

/// Navigation state for the main window (Diane idiom: a small observable
/// holding the selected sidebar item; content is switched by the window view).
@MainActor
@Observable
final class AppState {
    var selectedSidebarItem: SidebarItem = .dashboard

    nonisolated init() {}
}

// MARK: - SidebarItem

/// Navigable sections of the main window, grouped so the sidebar can separate
/// INFORMATION (what the connector is doing) from SETTINGS (how it is
/// configured).
enum SidebarItem: String, CaseIterable, Identifiable, Hashable {
    // Information
    case dashboard
    case project
    // Settings
    case tools
    case mcpServers
    case permissions
    case connection
    case about

    var id: String {
        rawValue
    }

    var title: String {
        switch self {
        case .dashboard: "Dashboard"
        case .project: "Project & Account"
        case .tools: "MCP Tools"
        case .mcpServers: "MCP Servers"
        case .permissions: "Permissions"
        case .connection: "Connection"
        case .about: "About"
        }
    }

    var systemIcon: String {
        switch self {
        case .dashboard: "gauge.medium"
        case .project: "person.crop.circle"
        case .tools: "wrench.and.screwdriver"
        case .mcpServers: "server.rack"
        case .permissions: "lock.shield"
        case .connection: "network"
        case .about: "info.circle"
        }
    }

    /// The sidebar cluster an item belongs to. `about` is deliberately `nil`:
    /// it is not project/settings context and is pinned separately at the
    /// bottom of the sidebar.
    var group: Group? {
        switch self {
        case .dashboard, .project: .information
        case .tools, .mcpServers, .permissions, .connection: .settings
        case .about: nil
        }
    }

    /// Sidebar sections, in display order.
    enum Group: String, CaseIterable, Identifiable {
        case information = "Information"
        case settings = "Settings"

        var id: String {
            rawValue
        }

        var items: [SidebarItem] {
            SidebarItem.allCases.filter { $0.group == self }
        }
    }
}
