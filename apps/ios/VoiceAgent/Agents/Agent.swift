import Foundation

/// An agent configured on the Memory control plane.
///
/// Mirrors the gateway's Memory-backed `AgentDefinition` (see
/// `gateway/memory.go`): the list endpoint returns summaries
/// `{ id, name, flowType, toolCount, isDefault, ... }`. The app is read-only,
/// so only the summary fields it displays are modeled.
///
/// Only the fields the app needs are modeled. Decoding is defensive: unknown
/// or extra JSON fields are ignored, and every field that the server might
/// omit or return as `null` falls back to a default so a single odd record
/// never breaks the whole list.
struct Agent: Identifiable, Codable, Equatable, Sendable {
    let id: String
    let name: String
    /// Execution flow type: `"single"` | `"sequential"` | `"loop"` (may be
    /// absent in list summaries).
    let flowType: String?
    /// Number of tools the agent exposes (may be absent).
    let toolCount: Int?
    /// Whether this is the project's default agent (may be absent).
    let isDefault: Bool?
    /// Whether the agent is enabled (accepts conversations). Defaults to true
    /// when absent so a missing flag never breaks the list.
    let enabled: Bool

    enum CodingKeys: String, CodingKey {
        case id, name, flowType, toolCount, isDefault, enabled
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = (try? container.decodeIfPresent(String.self, forKey: .id)) ?? ""
        name = (try? container.decodeIfPresent(String.self, forKey: .name)) ?? ""
        flowType = (try? container.decodeIfPresent(String.self, forKey: .flowType)) ?? nil
        toolCount = (try? container.decodeIfPresent(Int.self, forKey: .toolCount)) ?? nil
        isDefault = (try? container.decodeIfPresent(Bool.self, forKey: .isDefault)) ?? nil
        enabled = (try? container.decodeIfPresent(Bool.self, forKey: .enabled)) ?? true
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(id, forKey: .id)
        try container.encode(name, forKey: .name)
        try container.encodeIfPresent(flowType, forKey: .flowType)
        try container.encodeIfPresent(toolCount, forKey: .toolCount)
        try container.encodeIfPresent(isDefault, forKey: .isDefault)
        try container.encode(enabled, forKey: .enabled)
    }
}
