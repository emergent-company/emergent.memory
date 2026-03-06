import Foundation

/// Protocol defining all operations available in the Emergent Swift SDK.
///
/// `EmergentClient` is the production implementation. Conforming to this
/// protocol in your tests lets you inject a mock without triggering the
/// real Go C-bridge.
///
/// ### Example — injecting a mock
/// ```swift
/// struct MockClient: EmergentService {
///     func health() async throws -> HealthResponse { .init(status: "ok") }
///     // …
/// }
/// let sut = MyViewModel(client: MockClient())
/// ```
public protocol EmergentService: Sendable {

    // MARK: - Lifecycle

    /// Sets the default organisation and project context for subsequent calls.
    ///
    /// - Parameters:
    ///   - orgID: The organisation ID.
    ///   - projectID: The project ID.
    func setContext(orgID: String, projectID: String) async throws

    // MARK: - Health

    /// Performs a health check against the server.
    ///
    /// - Returns: The server's health status.
    /// - Throws: ``EmergentError`` on failure.
    func health() async throws -> HealthResponse

    // MARK: - Search

    /// Performs a hybrid semantic search.
    ///
    /// - Parameter request: The search parameters.
    /// - Returns: The search results and metadata.
    /// - Throws: ``EmergentError`` on failure.
    func search(_ request: SearchRequest) async throws -> SearchResponse

    // MARK: - Chat

    /// Sends a message and returns the complete assistant response.
    ///
    /// Internally this calls the streaming endpoint and collects all tokens
    /// before returning — suitable for simple use cases.
    ///
    /// - Parameter request: The chat parameters.
    /// - Returns: The assistant's response and conversation ID.
    /// - Throws: ``EmergentError`` on failure.
    func chat(_ request: ChatRequest) async throws -> ChatResponse

    // MARK: - Documents

    /// Lists documents accessible in the current project context.
    ///
    /// - Parameter request: Pagination options.
    /// - Returns: A paginated list of documents.
    /// - Throws: ``EmergentError`` on failure.
    func listDocuments(_ request: ListDocumentsRequest) async throws -> ListDocumentsResponse
}
