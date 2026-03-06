import Foundation

// MARK: - Configuration

/// Configuration for creating an ``EmergentClient``.
public struct EmergentConfig: Sendable {
    /// Full URL of the Emergent server, e.g. `"https://api.emergent-company.ai"`.
    public let serverURL: String

    /// Authentication mode. Supported values: `"apikey"`, `"apitoken"`.
    public let authMode: String

    /// The API key or token used for authentication.
    public let apiKey: String

    /// Optional default organisation ID for API calls.
    public let orgID: String?

    /// Optional default project ID for API calls.
    public let projectID: String?

    public init(
        serverURL: String,
        authMode: String = "apikey",
        apiKey: String,
        orgID: String? = nil,
        projectID: String? = nil
    ) {
        self.serverURL = serverURL
        self.authMode = authMode
        self.apiKey = apiKey
        self.orgID = orgID
        self.projectID = projectID
    }
}

// MARK: - Health

/// Response payload for a health-check operation.
public struct HealthResponse: Codable, Sendable {
    public let status: String
    public let timestamp: String?
    public let uptime: String?
    public let version: String?
}

// MARK: - Search

/// Request payload for a semantic / hybrid search.
public struct SearchRequest: Codable, Sendable {
    /// The search query.
    public let query: String
    /// Maximum number of results to return.
    public let limit: Int
    /// Result type filter: `"graph"`, `"text"`, or `"both"` (default).
    public let resultTypes: String?
    /// Fusion strategy: `"weighted"`, `"rrf"`, `"interleave"`, etc.
    public let fusionStrategy: String?

    enum CodingKeys: String, CodingKey {
        case query, limit
        case resultTypes = "result_types"
        case fusionStrategy = "fusion_strategy"
    }

    public init(
        query: String,
        limit: Int = 10,
        resultTypes: String? = nil,
        fusionStrategy: String? = nil
    ) {
        self.query = query
        self.limit = limit
        self.resultTypes = resultTypes
        self.fusionStrategy = fusionStrategy
    }
}

/// A single result item from a search operation.
public struct SearchResult: Codable, Sendable {
    public let type: String
    public let score: Float
    public let rank: Int
    public let objectID: String?
    public let objectType: String?
    public let key: String?
    public let documentID: String?
    public let chunkID: String?
    public let content: String?

    enum CodingKeys: String, CodingKey {
        case type, score, rank, key, content
        case objectID = "object_id"
        case objectType = "object_type"
        case documentID = "document_id"
        case chunkID = "chunk_id"
    }
}

/// Metadata about a search operation.
public struct SearchMetadata: Codable, Sendable {
    public let total: Int
    public let elapsedMs: Double?

    enum CodingKeys: String, CodingKey {
        case total = "totalResults"
        case elapsedMs = "elapsed_ms"
    }
}

/// Response payload for a search operation.
public struct SearchResponse: Codable, Sendable {
    public let results: [SearchResult]
    public let metadata: SearchMetadata?
}

// MARK: - Chat

/// Request payload for a chat operation.
public struct ChatRequest: Codable, Sendable {
    /// The user's message.
    public let message: String
    /// Optional existing conversation ID. If nil a new conversation is created.
    public let conversationID: String?

    enum CodingKeys: String, CodingKey {
        case message
        case conversationID = "conversation_id"
    }

    public init(message: String, conversationID: String? = nil) {
        self.message = message
        self.conversationID = conversationID
    }
}

/// Response payload for a chat operation.
public struct ChatResponse: Codable, Sendable {
    /// The conversation ID (new or existing).
    public let conversationID: String?
    /// The full assistant response content.
    public let content: String

    enum CodingKeys: String, CodingKey {
        case conversationID = "conversation_id"
        case content
    }
}

// MARK: - Documents

/// A document stored in the Emergent platform.
public struct Document: Codable, Sendable {
    public let id: String
    public let filename: String
    public let sourceType: String?
    public let createdAt: String?
    public let updatedAt: String?

    enum CodingKeys: String, CodingKey {
        case id, filename
        case sourceType = "source_type"
        case createdAt = "created_at"
        case updatedAt = "updated_at"
    }
}

/// Request payload for listing documents.
public struct ListDocumentsRequest: Codable, Sendable {
    public let limit: Int
    public let cursor: String?

    public init(limit: Int = 20, cursor: String? = nil) {
        self.limit = limit
        self.cursor = cursor
    }
}

/// Response payload for listing documents.
public struct ListDocumentsResponse: Codable, Sendable {
    public let documents: [Document]
    public let total: Int
    public let nextCursor: String?

    enum CodingKeys: String, CodingKey {
        case documents, total
        case nextCursor = "next_cursor"
    }
}

// MARK: - Context

/// Request payload for setting the default org/project context.
public struct SetContextRequest: Codable, Sendable {
    public let orgID: String
    public let projectID: String

    enum CodingKeys: String, CodingKey {
        case orgID = "org_id"
        case projectID = "project_id"
    }
}
