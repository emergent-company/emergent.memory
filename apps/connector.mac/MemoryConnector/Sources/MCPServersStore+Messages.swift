extension MCPServersStore {
    /// User-facing message for an error, collapsing the raw loopback-connection
    /// failure into a generic unavailable message so internal transport detail
    /// never reaches the user. Server/validation messages pass through verbatim.
    /// Pure and testable.
    nonisolated static func userMessage(for error: Error) -> String {
        if let api = error as? MCPServersAPIError, case .unreachable = api {
            return "The connector isn't available right now."
        }
        return error.localizedDescription
    }

    /// Names of currently configured servers, for form validation (duplicate
    /// names are rejected by the engine with HTTP 409).
    var existingNames: Set<String> {
        Set(servers.map(\.name))
    }

    /// Names of servers configured in Memory. These are read-only locally and
    /// cannot be edited, toggled, deleted, or tool-toggled from the app.
    var managedNames: Set<String> {
        Set(servers.filter(\.managed).map(\.name))
    }

    /// Names the Add form must treat as taken. Every configured server is
    /// reserved, including Memory-managed ones — a new local server may not
    /// reuse a managed name.
    var reservedNames: Set<String> {
        existingNames.union(managedNames)
    }

    /// Whether `name` is a Memory-managed server. Checks the list first, then
    /// any cached detail (so a detail-only view still knows).
    func isManaged(_ name: String) -> Bool {
        if let server = servers.first(where: { $0.name == name }) {
            return server.managed
        }
        return details[name]?.managed ?? false
    }
}
