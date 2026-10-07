import Foundation

/// One Memory project this device has onboarded.
///
/// A device credential is project-scoped, so a connection is the unit of
/// identity: its project (`projectID`/`orgID`/`projectName`) plus the gateway
/// URLs and device credential minted for it. The active connection is mirrored
/// into the existing `MemoryConfig` keys, so long-lived clients keep reading
/// `MemoryConfig()` unchanged.
///
/// Decoding is deliberately tolerant: a payload minted by an older gateway may
/// omit the project identity or `addedAt`, and those fields must default rather
/// than fail the whole stored list.
struct MemoryProjectConnection: Codable, Equatable, Sendable {
    /// Stable id of the project this connection is bound to. Empty when the
    /// connection predates project identity (legacy migration).
    var projectID: String
    /// Id of the organization that owns the project. May be empty.
    var orgID: String
    /// Human-readable project name. May be empty.
    var projectName: String
    /// `ws://` URL of the LiveKit server for this project.
    var serverURL: String
    /// HTTP(S) URL of the token endpoint (`POST /api/token`).
    var tokenEndpoint: String
    /// Base URL of the Memory gateway for this project.
    var apiBaseURL: String
    /// Device credential (`emt_*`) sent as the `X-API-Key` header.
    var apiKey: String
    /// `"client"` = app speaks replies locally; `"server"` = worker streams TTS.
    var ttsStrategy: String
    /// When this connection was added, used only for stable ordering.
    var addedAt: Date

    /// The label shown in the switcher and Projects list: the project name,
    /// falling back to the project id, then a generic "Memory".
    var displayName: String {
        if !projectName.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            return projectName
        }
        if !projectID.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            return projectID
        }
        return "Memory"
    }

    /// Whether this connection carries a real project identity. The legacy
    /// migration synthesizes a connection with an empty identity.
    var hasIdentity: Bool {
        !projectID.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }

    init(
        projectID: String = "",
        orgID: String = "",
        projectName: String = "",
        serverURL: String,
        tokenEndpoint: String,
        apiBaseURL: String,
        apiKey: String,
        ttsStrategy: String = "server",
        addedAt: Date = Date()
    ) {
        self.projectID = projectID
        self.orgID = orgID
        self.projectName = projectName
        self.serverURL = serverURL
        self.tokenEndpoint = tokenEndpoint
        self.apiBaseURL = apiBaseURL
        self.apiKey = apiKey
        self.ttsStrategy = ttsStrategy
        self.addedAt = addedAt
    }

    private enum CodingKeys: String, CodingKey {
        case projectID
        case orgID
        case projectName
        case serverURL
        case tokenEndpoint
        case apiBaseURL
        case apiKey
        case ttsStrategy
        case addedAt
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        projectID = try container.decodeIfPresent(String.self, forKey: .projectID) ?? ""
        orgID = try container.decodeIfPresent(String.self, forKey: .orgID) ?? ""
        projectName = try container.decodeIfPresent(String.self, forKey: .projectName) ?? ""
        serverURL = try container.decodeIfPresent(String.self, forKey: .serverURL) ?? ""
        tokenEndpoint = try container.decodeIfPresent(String.self, forKey: .tokenEndpoint) ?? ""
        apiBaseURL = try container.decodeIfPresent(String.self, forKey: .apiBaseURL) ?? ""
        apiKey = try container.decodeIfPresent(String.self, forKey: .apiKey) ?? ""
        ttsStrategy = try container.decodeIfPresent(String.self, forKey: .ttsStrategy) ?? "server"
        addedAt = try container.decodeIfPresent(Date.self, forKey: .addedAt) ?? Date()
    }
}
