import EmergentBridge
import Foundation
import OSLog

/// The primary Swift SDK client for the Emergent platform.
///
/// `EmergentClient` is implemented as a Swift `actor` to guarantee thread-safe
/// access to the underlying Go-side client handle and all internal mutable state.
/// Swift's actor isolation prevents data races when the client is used from
/// multiple concurrent tasks.
///
/// ### Quick start
/// ```swift
/// let client = try await EmergentClient(config: EmergentConfig(
///     serverURL: "https://api.emergent-company.ai",
///     apiKey: "emt_your_key_here",
///     orgID: "org_abc",
///     projectID: "proj_xyz"
/// ))
///
/// let health = try await client.health()
/// print(health.status) // "healthy"
///
/// let results = try await client.search(SearchRequest(query: "machine learning"))
/// ```
public actor EmergentClient: EmergentService {

    // MARK: - Private state

    private let handle: UInt32

    private static let logger = Logger(
        subsystem: "ai.emergent.sdk",
        category: "EmergentClient"
    )

    // MARK: - Initialisation

    /// Creates an `EmergentClient` from the provided configuration.
    ///
    /// - Parameter config: The server URL, credentials, and optional context.
    /// - Throws: ``EmergentError`` if the underlying Go client cannot be created.
    public init(config: EmergentConfig) async throws {
        let payload = ConfigPayload(
            serverURL: config.serverURL,
            authMode: config.authMode,
            apiKey: config.apiKey,
            orgID: config.orgID,
            projectID: config.projectID
        )
        let json = try JSONEncoder().encode(payload).asUTF8String()

        let responseJSON = withBridgeString(json) { ptr -> String in
            let cResult = CreateClient(ptr)
            return consumeBridgeString(cResult)
        }

        let response: CreateClientResponse = try decodeBridgeResponse(responseJSON)
        handle = response.handle

        Self.logger.info("EmergentClient created: handle=\(response.handle)")
    }

    // MARK: - Package-internal init for unit tests

    /// Creates an `EmergentClient` from a pre-built Go client handle.
    ///
    /// This initialiser is **package-internal** and intended for unit tests only.
    /// Pass a fake handle (e.g. `1`) together with a `MockEmergentBridge` set up
    /// via ``EmergentBridgeProtocol`` to exercise client behaviour without
    /// building the real XCFramework.
    ///
    /// ```swift
    /// let client = EmergentClient(_handle: 1)
    /// ```
    init(_handle: UInt32) {
        self.handle = _handle
    }

    // MARK: - Deallocation

    deinit {
        FreeClient(handle)
        EmergentClient.logger.info("EmergentClient freed: handle=\(handle)")
    }

    // MARK: - Context

    /// Sets the default organisation and project context.
    public func setContext(orgID: String, projectID: String) async throws {
        let req = SetContextRequest(orgID: orgID, projectID: projectID)
        let json = try JSONEncoder().encode(req).asUTF8String()
        let responseJSON = withBridgeString(json) { ptr -> String in
            let cResult = SetContext(handle, ptr)
            return consumeBridgeString(cResult)
        }
        let _: AckResponse = try decodeBridgeResponse(responseJSON)
    }

    // MARK: - Health

    /// Performs an async health check against the server.
    ///
    /// - Returns: The server's current health status.
    /// - Throws: ``EmergentError`` on failure or cancellation.
    public func health() async throws -> HealthResponse {
        try await withAsyncCallback { ctx in
            HealthCheck(handle, { opID, jsonPtr, ctx in
                guard let box = releaseContext(ctx) as CallbackBox<HealthResponse>? else { return }
                let json = consumeBridgeString(UnsafeMutablePointer(mutating: jsonPtr))
                do {
                    box.continuation.resume(returning: try decodeBridgeResponse(json))
                } catch {
                    box.continuation.resume(throwing: error)
                }
            }, ctx)
        }
    }

    // MARK: - Search

    /// Performs a hybrid semantic search.
    ///
    /// - Parameter request: The search parameters.
    /// - Returns: Ranked results and metadata.
    /// - Throws: ``EmergentError`` on failure or cancellation.
    public func search(_ request: SearchRequest) async throws -> SearchResponse {
        let json = try JSONEncoder().encode(request).asUTF8String()
        return try await withAsyncCallback { ctx in
            withBridgeString(json) { ptr in
                Search(handle, ptr, { opID, jsonPtr, ctx in
                    guard let box = releaseContext(ctx) as CallbackBox<SearchResponse>? else { return }
                    let json = consumeBridgeString(UnsafeMutablePointer(mutating: jsonPtr))
                    do {
                        box.continuation.resume(returning: try decodeBridgeResponse(json))
                    } catch {
                        box.continuation.resume(throwing: error)
                    }
                }, ctx)
            }
        }
    }

    // MARK: - Chat

    /// Sends a message and returns the complete assistant response.
    ///
    /// Internally calls the streaming endpoint and collects all tokens before
    /// returning — suitable for non-streaming use cases.
    ///
    /// - Parameter request: The chat parameters.
    /// - Returns: The assistant's response and conversation ID.
    /// - Throws: ``EmergentError`` on failure or cancellation.
    public func chat(_ request: ChatRequest) async throws -> ChatResponse {
        let json = try JSONEncoder().encode(request).asUTF8String()
        return try await withAsyncCallback { ctx in
            withBridgeString(json) { ptr in
                Chat(handle, ptr, { opID, jsonPtr, ctx in
                    guard let box = releaseContext(ctx) as CallbackBox<ChatResponse>? else { return }
                    let json = consumeBridgeString(UnsafeMutablePointer(mutating: jsonPtr))
                    do {
                        box.continuation.resume(returning: try decodeBridgeResponse(json))
                    } catch {
                        box.continuation.resume(throwing: error)
                    }
                }, ctx)
            }
        }
    }

    // MARK: - Documents

    /// Lists documents accessible in the current project context.
    ///
    /// - Parameter request: Pagination options.
    /// - Returns: A paginated list of documents.
    /// - Throws: ``EmergentError`` on failure or cancellation.
    public func listDocuments(_ request: ListDocumentsRequest) async throws -> ListDocumentsResponse {
        let json = try JSONEncoder().encode(request).asUTF8String()
        return try await withAsyncCallback { ctx in
            withBridgeString(json) { ptr in
                ListDocuments(handle, ptr, { opID, jsonPtr, ctx in
                    guard let box = releaseContext(ctx) as CallbackBox<ListDocumentsResponse>? else { return }
                    let json = consumeBridgeString(UnsafeMutablePointer(mutating: jsonPtr))
                    do {
                        box.continuation.resume(returning: try decodeBridgeResponse(json))
                    } catch {
                        box.continuation.resume(throwing: error)
                    }
                }, ctx)
            }
        }
    }

    // MARK: - Async callback bridge

    /// Bridges a C-level callback into a Swift `async throws` function.
    ///
    /// The `body` closure receives the retained Swift context pointer and MUST
    /// return the `UInt64` operation ID produced by the Go bridge function. That
    /// ID is stored in `opIDBox` so that the `onCancel` handler can call
    /// `CancelOperation` with the correct ID if the calling Swift `Task` is
    /// cancelled before the Go goroutine finishes.
    ///
    /// Memory note: the callback's `jsonPtr` is Go-allocated and ownership has
    /// been transferred to Swift — use `consumeBridgeString` (which calls
    /// `FreeString`) to take ownership.
    private func withAsyncCallback<T: Sendable & Decodable>(
        body: @Sendable (UnsafeMutableRawPointer) -> UInt64
    ) async throws -> T {
        let opIDBox = OperationIDBox()
        return try await withTaskCancellationHandler {
            try await withCheckedThrowingContinuation { continuation in
                let box = CallbackBox<T>(continuation)
                let ctx = retainedContext(box)
                let opID = body(ctx)
                opIDBox.set(opID)
                // If the task was already cancelled before body() returned,
                // onCancel may have fired with opID == 0. Re-cancel now to
                // ensure the Go operation is actually signalled.
                if Task.isCancelled {
                    CancelOperation(opID)
                }
            }
        } onCancel: {
            // Called on an arbitrary thread when the Swift Task is cancelled.
            // OperationIDBox is thread-safe; if body() hasn't returned yet
            // (opID still 0), the re-cancel guard inside body() handles it.
            let id = opIDBox.get()
            if id != 0 {
                CancelOperation(id)
            }
        }
    }
}

// MARK: - OperationIDBox

/// A thread-safe box for passing an operation ID between the continuation
/// body and the `withTaskCancellationHandler` `onCancel` closure.
private final class OperationIDBox: @unchecked Sendable {
    private let lock = NSLock()
    private var value: UInt64 = 0

    func set(_ id: UInt64) { lock.withLock { value = id } }
    func get() -> UInt64   { lock.withLock { value } }
}

// MARK: - Internal payload types (not public API)

private struct ConfigPayload: Encodable {
    let serverURL: String
    let authMode: String
    let apiKey: String
    let orgID: String?
    let projectID: String?

    enum CodingKeys: String, CodingKey {
        case serverURL = "server_url"
        case authMode = "auth_mode"
        case apiKey = "api_key"
        case orgID = "org_id"
        case projectID = "project_id"
    }
}

private struct CreateClientResponse: Decodable {
    let handle: UInt32
}

private struct AckResponse: Decodable {
    let ok: Bool?
}

// MARK: - Convenience extensions

private extension Data {
    func asUTF8String() throws -> String {
        guard let s = String(data: self, encoding: .utf8) else {
            throw EmergentError.internal("Failed to encode payload as UTF-8")
        }
        return s
    }
}
