import Foundation

/// Errors surfaced by `MemoryAPIClient`.
enum MemoryAPIError: LocalizedError, Equatable, Sendable {
    case notConfigured
    case authFailed
    case unreachable(String)
    case httpStatus(Int)
    case decoding(String)

    var errorDescription: String? {
        switch self {
        case .notConfigured:
            return "No server URL or token configured."
        case .authFailed:
            return "Authentication failed — the server rejected the token."
        case .unreachable(let detail):
            return "Server unreachable: \(detail)"
        case .httpStatus(let code):
            return "Server returned HTTP \(code)."
        case .decoding(let detail):
            return "Could not read the server response: \(detail)"
        }
    }
}

/// `GET /api/schema-registry/projects/{id}/stats` (`SchemaRegistryStats`).
///
/// The server emits snake_case keys; camelCase variants are accepted too, so
/// responses from both the Memory server and SDK-style payloads decode.
struct SchemaStats: Decodable, Equatable, Sendable {
    let totalTypes: Int
    let enabledTypes: Int
    let customTypes: Int
    let totalObjects: Int
    enum CodingKeys: String, CodingKey {
        case totalTypes, enabledTypes, customTypes, totalObjects
        case total_types, enabled_types, custom_types, total_objects
        case TotalTypes, EnabledTypes, CustomTypes, TotalObjects
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        totalTypes = try container.decodeInt(.totalTypes, .total_types, .TotalTypes) ?? 0
        enabledTypes = try container.decodeInt(.enabledTypes, .enabled_types, .EnabledTypes) ?? 0
        customTypes = try container.decodeInt(.customTypes, .custom_types, .CustomTypes) ?? 0
        totalObjects = try container.decodeInt(.totalObjects, .total_objects, .TotalObjects) ?? 0
    }

}

/// One row of `GET /api/projects/{id}/agents` (`AgentDTO`, subset). The
/// server's `enabled` flag maps to `isEnabled`; `isEnabled` is also accepted.
struct AgentSummary: Decodable, Equatable, Sendable {
    let id: String
    let name: String
    let description: String?
    let isEnabled: Bool?
    let lastRunStatus: String?
    enum CodingKeys: String, CodingKey {
        case id, name, description, lastRunStatus
        case isEnabled = "enabled"
        case isEnabledAlt = "isEnabled"
        case Enabled, IsEnabled
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decodeIfPresent(String.self, forKey: .id) ?? ""
        name = try container.decodeIfPresent(String.self, forKey: .name) ?? ""
        description = try container.decodeIfPresent(String.self, forKey: .description)
        isEnabled = try container.decodeBool(.isEnabled, .isEnabledAlt, .Enabled, .IsEnabled)
        lastRunStatus = try container.decodeIfPresent(String.self, forKey: .lastRunStatus)
    }

}

/// One row of `GET /api/projects/{id}/agent-definitions` (the real, reusable
/// agent definitions — not a chat-session instance). Snake_case and camelCase
/// keys are both accepted; unknown keys are ignored. Optional fields stay nil
/// when the server omits them (`tools: null` → nil).
struct AgentDefinitionSummary: Decodable, Equatable, Sendable {
    let id: String
    let name: String
    let description: String?
    let model: String?
    let tools: [String]?
    let bannedTools: [String]?
    let enabled: Bool?
    let visibility: String?
    let triggerType: String?
    let isDefault: Bool?
    let flowType: String?
    /// Number of configured MCP tools. This is the ONLY tool signal the list
    /// endpoint exposes — it does not return the `tools` whitelist itself.
    let toolCount: Int?

    enum CodingKeys: String, CodingKey {
        case id, name, description, model, tools, enabled, visibility
        case bannedTools, banned_tools
        case triggerType, trigger_type
        case isDefault, is_default
        case flowType, flow_type
        case toolCount, tool_count
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decodeString(.id) ?? ""
        name = try container.decodeString(.name) ?? ""
        description = try container.decodeString(.description)
        model = try container.decodeString(.model)
        tools = try container.decodeIfPresent([String].self, forKey: .tools)
        bannedTools = try container.decodeStringArray(.bannedTools, .banned_tools)
        enabled = try container.decodeBool(.enabled)
        visibility = try container.decodeString(.visibility)
        triggerType = try container.decodeString(.triggerType, .trigger_type)
        isDefault = try container.decodeBool(.isDefault, .is_default)
        flowType = try container.decodeString(.flowType, .flow_type)
        toolCount = try container.decodeInt(.toolCount, .tool_count)
    }
}

/// Full agent definition from `GET /api/projects/{id}/agent-definitions/{defID}`
/// (`AgentDefinitionDTO`). Unlike `AgentDefinitionSummary`, this carries the
/// resolved `tools` whitelist (`nil`/`[]` means "no MCP tools"), the system
/// prompt, skills, and the nested model config.
///
/// Tolerant of snake_case/camelCase keys and of unknown keys. The server's
/// `model` is an OBJECT (`{name, nativeTools, …}`); `model` here is flattened to
/// the model NAME and the nested `nativeTools` are surfaced separately via
/// `modelNativeTools`. A plain-string `model` is also accepted. Workspace (sandbox)
/// tools are lifted out of `workspaceConfig.tools`.
struct AgentDefinitionDetail: Decodable, Equatable, Sendable {
    let id: String
    let name: String
    let description: String?
    let systemPrompt: String?
    let model: String?
    let modelNativeTools: [String]?
    let tools: [String]?
    let bannedTools: [String]?
    let skills: [String]?
    let autoLoadSkills: Bool?
    let enabled: Bool?
    let isDefault: Bool?
    let visibility: String?
    let triggerType: String?
    let flowType: String?
    let workspaceTools: [String]?

    enum CodingKeys: String, CodingKey {
        case id, name, description, model, tools, skills, enabled, visibility
        case systemPrompt, system_prompt
        case bannedTools, banned_tools
        case autoLoadSkills, auto_load_skills
        case isDefault, is_default
        case triggerType, trigger_type
        case flowType, flow_type
        case workspaceConfig, workspace_config
    }

    /// Nested `model` object shape emitted by the server.
    private struct ModelConfigDTO: Decodable {
        let name: String?
        let nativeTools: [String]?
        enum CodingKeys: String, CodingKey {
            case name
            case nativeTools, native_tools
        }
        init(from decoder: Decoder) throws {
            let container = try decoder.container(keyedBy: CodingKeys.self)
            name = try container.decodeString(.name)
            nativeTools = try container.decodeStringArray(.nativeTools, .native_tools)
        }
    }

    /// Keys of the nested `workspaceConfig` (sandbox) object.
    private enum WorkspaceKeys: String, CodingKey {
        case tools
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decodeString(.id) ?? ""
        name = try container.decodeString(.name) ?? ""
        description = try container.decodeString(.description)
        systemPrompt = try container.decodeString(.systemPrompt, .system_prompt)
        tools = try container.decodeIfPresent([String].self, forKey: .tools)
        bannedTools = try container.decodeStringArray(.bannedTools, .banned_tools)
        skills = try container.decodeStringArray(.skills)
        autoLoadSkills = try container.decodeBool(.autoLoadSkills, .auto_load_skills)
        enabled = try container.decodeBool(.enabled)
        isDefault = try container.decodeBool(.isDefault, .is_default)
        visibility = try container.decodeString(.visibility)
        triggerType = try container.decodeString(.triggerType, .trigger_type)
        flowType = try container.decodeString(.flowType, .flow_type)

        // `model` is an object on the server; accept a plain string too.
        if let modelObject = try? container.decode(ModelConfigDTO.self, forKey: .model) {
            model = modelObject.name
            modelNativeTools = modelObject.nativeTools
        } else if let modelString = try? container.decode(String.self, forKey: .model) {
            model = modelString
            modelNativeTools = nil
        } else {
            model = nil
            modelNativeTools = nil
        }

        // Sandbox tools live under `workspaceConfig.tools` (snake_case accepted).
        if let workspace = try? container.nestedContainer(keyedBy: WorkspaceKeys.self,
                                                          forKey: .workspaceConfig) {
            workspaceTools = try workspace.decodeIfPresent([String].self, forKey: .tools)
        } else if let workspace = try? container.nestedContainer(keyedBy: WorkspaceKeys.self,
                                                                 forKey: .workspace_config) {
            workspaceTools = try workspace.decodeIfPresent([String].self, forKey: .tools)
        } else {
            workspaceTools = nil
        }
    }
}

/// `stats` object of `ProjectDTO` (server emits camelCase; snake_case accepted).
struct ProjectStats: Decodable, Equatable, Sendable {
    let documentCount: Int
    let objectCount: Int
    let relationshipCount: Int
    let totalJobs: Int
    let runningJobs: Int
    let queuedJobs: Int
    enum CodingKeys: String, CodingKey {
        case documentCount, objectCount, relationshipCount, totalJobs, runningJobs, queuedJobs
        case document_count, object_count, relationship_count, total_jobs, running_jobs, queued_jobs
        case DocumentCount, ObjectCount, RelationshipCount, TotalJobs, RunningJobs, QueuedJobs
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        documentCount = try container.decodeInt(.documentCount, .document_count, .DocumentCount) ?? 0
        objectCount = try container.decodeInt(.objectCount, .object_count, .ObjectCount) ?? 0
        relationshipCount = try container.decodeInt(.relationshipCount, .relationship_count, .RelationshipCount) ?? 0
        totalJobs = try container.decodeInt(.totalJobs, .total_jobs, .TotalJobs) ?? 0
        runningJobs = try container.decodeInt(.runningJobs, .running_jobs, .RunningJobs) ?? 0
        queuedJobs = try container.decodeInt(.queuedJobs, .queued_jobs, .QueuedJobs) ?? 0
    }

}

/// `GET /api/projects/{id}?include_stats=true` (`ProjectDTO`, subset).
/// `orgId`/`project_info` are the server's keys; camelCase variants accepted.
struct ProjectDetail: Decodable, Equatable, Sendable {
    let name: String?
    let orgID: String?
    let projectInfo: String?
    let stats: ProjectStats?
    enum CodingKeys: String, CodingKey {
        case name, stats
        case orgID = "orgId"
        case org_id = "org_id"
        case projectInfo = "projectInfo"
        case project_info = "project_info"
        case OrgId, ProjectInfo
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        name = try container.decodeIfPresent(String.self, forKey: .name)
        orgID = try container.decodeString(.orgID, .org_id, .OrgId)
        projectInfo = try container.decodeString(.projectInfo, .project_info, .ProjectInfo)
        stats = try container.decodeIfPresent(ProjectStats.self, forKey: .stats)
    }

}

/// Thin client for the Memory REST API.
///
/// The token and base URL are always supplied by the caller (plus an optional
/// `URLSession`, which tests stub). When `projectID`/`orgID` are set they are
/// sent as `X-Project-ID`/`X-Org-ID` (project-scoped calls); identity
/// endpoints ignore them.
struct MemoryAPIClient: Sendable {
    let serverURL: String
    let token: String
    let projectID: String?
    let orgID: String?
    let session: URLSession

    init(serverURL: String,
         token: String,
         projectID: String? = nil,
         orgID: String? = nil,
         session: URLSession = .shared) {
        self.serverURL = serverURL
        self.token = token
        self.projectID = projectID
        self.orgID = orgID
        self.session = session
    }

    // MARK: - Identity / projects

    func authMe() async throws -> AuthMe {
        try await request(method: "GET", path: "/api/auth/me", as: AuthMe.self)
    }

    func userProfile() async throws -> UserProfile {
        try await request(method: "GET", path: "/api/user/profile", as: UserProfile.self)
    }

    /// Returns nil when the response has `project: null` (account-level token).
    func currentProject() async throws -> ProjectInfo? {
        let response = try await request(method: "GET", path: "/api/projects/current",
                                         as: CurrentProjectResponse.self)
        return response.project
    }

    /// `GET /api/projects` — the user's projects.
    func projects() async throws -> [ProjectInfo] {
        try await request(method: "GET", path: "/api/projects", as: [ProjectInfo].self)
    }

    /// `GET /api/orgs` — the orgs visible to the token (RequireAuth only).
    func orgs() async throws -> [OrgInfo] {
        try await request(method: "GET", path: "/api/orgs", as: [OrgInfo].self)
    }

    /// Convenience snapshot for the UI layer.
    ///
    /// The org NAME resolution is best-effort: if `GET /api/orgs` fails or has
    /// no entry for the current org id, the snapshot is still returned with the
    /// project/user data and `organization == nil`.
    func identitySnapshot() async throws -> IdentitySnapshot {
        var snapshot = IdentitySnapshot()
        snapshot.authMe = try await authMe()
        snapshot.profile = try await userProfile()
        let response = try await request(method: "GET", path: "/api/projects/current",
                                         as: CurrentProjectResponse.self)
        snapshot.project = response.project
        snapshot.projectMessage = response.message

        let orgID = (snapshot.project?.orgID).cleaned ?? (snapshot.authMe?.orgID).cleaned
        if let orgID, let allOrgs = try? await orgs() {
            snapshot.organization = allOrgs.first { $0.id == orgID }
        }
        return snapshot
    }

    // MARK: - Avatar

    /// `GET /api/user/avatar` — raw image bytes for the signed-in user.
    ///
    /// Returns `nil` when the user has no avatar (HTTP 404). Auth/transport/
    /// other-status failures map to the usual `MemoryAPIError` cases. Uses the
    /// OIDC user token (or the configured token) + `X-Project-ID`; the response
    /// is raw bytes, so no JSON decoding/content-type expectation.
    func avatarData(projectID: String) async throws -> Data? {
        do {
            let (data, _) = try await perform(method: "GET",
                                              path: "/api/user/avatar",
                                              projectHeader: projectID)
            return data
        } catch let error as MemoryAPIError where error == .httpStatus(404) {
            return nil
        }
    }

    // MARK: - Dashboard

    /// `GET /api/graph/objects/count` — object count for the project
    /// (soft-deleted objects excluded by default).
    func objectCount(projectID: String, accessToken: String) async throws -> Int? {
        let response = try await request(method: "GET",
                                         path: "/api/graph/objects/count",
                                         bearer: accessToken,
                                         projectHeader: projectID,
                                         as: CountResponse.self)
        return response.count
    }

    /// `GET /api/graph/relationships/count` — relationship count for the project.
    func relationshipCount(projectID: String, accessToken: String) async throws -> Int? {
        let response = try await request(method: "GET",
                                         path: "/api/graph/relationships/count",
                                         bearer: accessToken,
                                         projectHeader: projectID,
                                         as: CountResponse.self)
        return response.count
    }

    /// `GET /api/schema-registry/projects/{projectID}/stats` — object-type
    /// statistics. Returns the subset the Dashboard shows.
    func schemaStats(projectID: String, accessToken: String) async throws -> SchemaStats? {
        try await request(method: "GET",
                          path: "/api/schema-registry/projects/\(projectID)/stats",
                          bearer: accessToken,
                          projectHeader: projectID,
                          as: SchemaStats.self)
    }

    /// `GET /api/projects/{projectID}/agents` — project agents, decoded from
    /// the `{success, data}` envelope. Missing `data` yields an empty list.
    func agents(projectID: String, accessToken: String) async throws -> [AgentSummary] {
        let response = try await request(method: "GET",
                                         path: "/api/projects/\(projectID)/agents",
                                         bearer: accessToken,
                                         projectHeader: projectID,
                                         as: AgentListResponse.self)
        return response.data ?? []
    }

    /// `GET /api/projects/{projectID}/agent-definitions` — the project's real
    /// agent definitions, decoded from the `{success, data}` envelope. Missing
    /// `data` yields an empty list.
    func agentDefinitions(projectID: String, accessToken: String) async throws -> [AgentDefinitionSummary] {
        let response = try await request(method: "GET",
                                         path: "/api/projects/\(projectID)/agent-definitions",
                                         bearer: accessToken,
                                         projectHeader: projectID,
                                         as: AgentDefinitionListResponse.self)
        return response.data ?? []
    }

    /// `GET /api/projects/{projectID}/agent-definitions/{id}` — the full agent
    /// definition (including the `tools` whitelist). Decoded from the
    /// `{success, data}` envelope; a missing/null `data` yields nil.
    func agentDefinition(projectID: String,
                         id: String,
                         accessToken: String) async throws -> AgentDefinitionDetail? {
        let response = try await request(method: "GET",
                                         path: "/api/projects/\(projectID)/agent-definitions/\(id)",
                                         bearer: accessToken,
                                         projectHeader: projectID,
                                         as: AgentDefinitionResponse.self)
        return response.data
    }

    /// `GET /api/projects/{projectID}?include_stats=true` — project detail plus
    /// aggregate statistics.
    func projectDetail(projectID: String, accessToken: String) async throws -> ProjectDetail? {
        try await request(method: "GET",
                          path: "/api/projects/\(projectID)?include_stats=true",
                          bearer: accessToken,
                          projectHeader: projectID,
                          as: ProjectDetail.self)
    }

    private struct CountResponse: Decodable {
        let count: Int?
    }

    private struct AgentListResponse: Decodable {
        let success: Bool?
        let data: [AgentSummary]?
    }

    private struct AgentDefinitionListResponse: Decodable {
        let success: Bool?
        let data: [AgentDefinitionSummary]?
    }

    private struct AgentDefinitionResponse: Decodable {
        let success: Bool?
        let data: AgentDefinitionDetail?
    }

    // MARK: - Request plumbing

    private func request<T: Decodable>(method: String,
                                       path: String,
                                       body: Data? = nil,
                                       bearer: String? = nil,
                                       projectHeader: String? = nil,
                                       as type: T.Type) async throws -> T {
        let (data, _) = try await perform(method: method, path: path, body: body,
                                          bearer: bearer, projectHeader: projectHeader)
        do {
            return try JSONDecoder().decode(T.self, from: data)
        } catch {
            throw MemoryAPIError.decoding(error.localizedDescription)
        }
    }

    /// Shared transport: builds the request (auth + project/org headers),
    /// runs it, and maps statuses to `MemoryAPIError`. Returns raw bytes so
    /// callers can decode JSON or hand back image data.
    private func perform(method: String,
                         path: String,
                         body: Data? = nil,
                         bearer: String? = nil,
                         projectHeader: String? = nil) async throws -> (Data, HTTPURLResponse) {
        let effectiveToken = bearer ?? token
        let base = serverURL.trimmingCharacters(in: .whitespacesAndNewlines)
        if base.isEmpty || effectiveToken.isEmpty {
            throw MemoryAPIError.notConfigured
        }
        var trimmedBase = base
        while trimmedBase.hasSuffix("/") { trimmedBase.removeLast() }
        guard let url = URL(string: trimmedBase + path) else {
            throw MemoryAPIError.unreachable("invalid server URL: \(base)")
        }

        var request = URLRequest(url: url)
        request.httpMethod = method
        request.setValue("Bearer \(effectiveToken)", forHTTPHeaderField: "Authorization")
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        let effectiveProject = projectHeader ?? projectID
        if let effectiveProject, !effectiveProject.isEmpty {
            request.setValue(effectiveProject, forHTTPHeaderField: "X-Project-ID")
        }
        if let orgID, !orgID.isEmpty {
            request.setValue(orgID, forHTTPHeaderField: "X-Org-ID")
        }
        if let body {
            request.httpBody = body
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        }

        let data: Data
        let response: URLResponse
        do {
            (data, response) = try await session.data(for: request)
        } catch {
            throw MemoryAPIError.unreachable(error.localizedDescription)
        }

        guard let http = response as? HTTPURLResponse else {
            throw MemoryAPIError.unreachable("non-HTTP response")
        }
        switch http.statusCode {
        case 200..<300:
            break
        case 401, 403:
            throw MemoryAPIError.authFailed
        default:
            throw MemoryAPIError.httpStatus(http.statusCode)
        }
        return (data, http)
    }
}

// MARK: - Tolerant decoding helpers

private extension KeyedDecodingContainer {
    /// Decodes an `Int` from the first matching candidate key, returning nil
    /// when none is present. Callers pass every accepted casing for a field.
    func decodeInt(_ keys: Key...) throws -> Int? {
        for key in keys {
            if let value = try decodeIfPresent(Int.self, forKey: key) { return value }
        }
        return nil
    }

    /// Decodes a `String` from the first matching candidate key.
    func decodeString(_ keys: Key...) throws -> String? {
        for key in keys {
            if let value = try decodeIfPresent(String.self, forKey: key) { return value }
        }
        return nil
    }

    /// Decodes a `Bool` from the first matching candidate key.
    func decodeBool(_ keys: Key...) throws -> Bool? {
        for key in keys {
            if let value = try decodeIfPresent(Bool.self, forKey: key) { return value }
        }
        return nil
    }

    /// Decodes a `[String]` from the first matching candidate key.
    func decodeStringArray(_ keys: Key...) throws -> [String]? {
        for key in keys {
            if let value = try decodeIfPresent([String].self, forKey: key) { return value }
        }
        return nil
    }
}
