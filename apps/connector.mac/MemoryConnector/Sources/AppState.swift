import Combine
import Foundation

/// Navigation state for the main window (Diane idiom: a small observable
/// holding the selected sidebar item; content is switched by the window view).
@MainActor
final class AppState: ObservableObject {

    @Published var selectedSidebarItem: SidebarItem = .dashboard

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

    var id: String { rawValue }

    var title: String {
        switch self {
        case .dashboard:   return "Dashboard"
        case .project:     return "Project & Account"
        case .tools:       return "MCP Tools"
        case .mcpServers:  return "MCP Servers"
        case .permissions: return "Permissions"
        case .connection:  return "Connection"
        case .about:       return "About"
        }
    }

    var systemIcon: String {
        switch self {
        case .dashboard:   return "gauge.medium"
        case .project:     return "person.crop.circle"
        case .tools:       return "wrench.and.screwdriver"
        case .mcpServers:  return "server.rack"
        case .permissions: return "lock.shield"
        case .connection:  return "network"
        case .about:       return "info.circle"
        }
    }

    var group: Group {
        switch self {
        case .dashboard, .project:                         return .information
        case .tools, .mcpServers, .permissions, .connection, .about: return .settings
        }
    }

    /// Sidebar sections, in display order.
    enum Group: String, CaseIterable, Identifiable {
        case information = "Information"
        case settings = "Settings"

        var id: String { rawValue }

        var items: [SidebarItem] {
            SidebarItem.allCases.filter { $0.group == self }
        }
    }
}
