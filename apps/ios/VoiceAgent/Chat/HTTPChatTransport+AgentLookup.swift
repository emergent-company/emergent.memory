import Foundation

// MARK: - Agent definition

extension HTTPChatTransport {
    /// Resolves the selected agent's definition id, which the chat request
    /// needs to talk to the agent the user picked. Best-effort: when the lookup
    /// fails the request omits it and the server uses the project default.
    func resolveAgentDefinitionID(name: String) async -> String? {
        if agentDefinitionResolved {
            return agentDefinitionID
        }
        agentDefinitionResolved = true
        guard !name.isEmpty else { return nil }
        do {
            let agents = try await ControlPlaneClient(config: config).listAgents()
            agentDefinitionID = agents.first { $0.name == name }?.id
        } catch {
            agentDefinitionID = nil
        }
        return agentDefinitionID
    }
}
