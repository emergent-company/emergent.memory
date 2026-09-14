import Foundation

/// The engine's Apple MCP tools as the app knows them (Section 6 uses this to
/// drive enable/disable toggles; the engine itself owns the schema).
struct ToolCatalog {

    enum Service: String, CaseIterable {
        case notes
        case reminders

        var displayName: String {
            switch self {
            case .notes: return "Apple Notes"
            case .reminders: return "Apple Reminders"
            }
        }
    }

    struct Tool: Identifiable, Equatable {
        let id: String          // engine tool name (must match the connector)
        let displayName: String
        let summary: String
        let service: Service
    }

    /// Must match the engine's registered tool ids.
    static let tools: [Tool] = [
        Tool(id: "notes_search",
             displayName: "Search Notes",
             summary: "Find notes by text in an optional folder.",
             service: .notes),
        Tool(id: "notes_create",
             displayName: "Create Note",
             summary: "Create a note in the default folder.",
             service: .notes),
        Tool(id: "reminders_list",
             displayName: "List Reminders",
             summary: "List reminders with their id, list, and completion state.",
             service: .reminders),
        Tool(id: "reminders_lists",
             displayName: "List Reminder Lists",
             summary: "List reminder lists with ids and reminder counts.",
             service: .reminders),
        Tool(id: "reminders_add",
             displayName: "Add Reminder",
             summary: "Add a reminder to a list with optional due date.",
             service: .reminders),
        Tool(id: "reminders_update",
             displayName: "Update Reminder",
             summary: "Edit, complete, move, or reschedule a reminder by id.",
             service: .reminders),
        Tool(id: "reminders_delete",
             displayName: "Delete Reminder",
             summary: "Delete a reminder by id.",
             service: .reminders),
    ]

    static func tool(id: String) -> Tool? {
        tools.first { $0.id == id }
    }

    static func tools(for service: Service) -> [Tool] {
        tools.filter { $0.service == service }
    }
}
