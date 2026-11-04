import Foundation

// MARK: - Mock bridge protocol (task 6.1)

/// An internal protocol that abstracts the bridge operations used by
/// `EmergentClient`, enabling fast unit tests without the XCFramework.
///
/// The default implementation (used in production) delegates directly to the
/// Go C-bridge functions. In tests you inject a `MockEmergentBridge`.
///
/// This type is intentionally `internal` — it is an implementation detail and
/// must not appear in the public API namespace.
protocol EmergentBridgeProtocol: Sendable {
    func createClient(configJSON: String) throws -> UInt32
    func freeClient(_ handle: UInt32)
    func ping(handle: UInt32, requestJSON: String) throws -> String
    func health(handle: UInt32) async throws -> String
    func search(handle: UInt32, requestJSON: String) async throws -> String
    func chat(handle: UInt32, requestJSON: String) async throws -> String
    func listDocuments(handle: UInt32, requestJSON: String) async throws -> String
    func setContext(handle: UInt32, requestJSON: String) throws -> String
}

// MARK: - Mock implementation

/// A mock bridge for unit testing ``EmergentClient`` without the XCFramework.
///
/// Inject this into ``EmergentClient`` via the internal test initialiser to
/// verify JSON decoding, error mapping, and concurrency behaviour.
///
/// ### Example
/// ```swift
/// let mock = MockEmergentBridge()
/// mock.healthResult = .success(HealthResponse(status: "healthy"))
/// let client = EmergentClient(bridge: mock, handle: 1)
/// let health = try await client.health()
/// XCTAssertEqual(health.status, "healthy")
/// ```
final class MockEmergentBridge: EmergentBridgeProtocol, @unchecked Sendable {

    // Configurable results — set these in your test setUp.
    var healthResult: Result<HealthResponse, Error>?
    var searchResult: Result<SearchResponse, Error>?
    var chatResult: Result<ChatResponse, Error>?
    var listDocumentsResult: Result<ListDocumentsResponse, Error>?

    // Call counts for assertions.
    private(set) var freeClientCallCount = 0

    func createClient(configJSON: String) throws -> UInt32 { 1 }
    func freeClient(_ handle: UInt32) { freeClientCallCount += 1 }
    func ping(handle: UInt32, requestJSON: String) throws -> String { "{\"result\":{\"echo\":\"mock\"}}" }
    func setContext(handle: UInt32, requestJSON: String) throws -> String { "{\"result\":{\"ok\":true}}" }

    func health(handle: UInt32) async throws -> String {
        guard let result = healthResult else { fatalError("MockEmergentBridge.healthResult not set") }
        switch result {
        case .success(let r): return try encodeOK(r)
        case .failure(let e): throw e
        }
    }

    func search(handle: UInt32, requestJSON: String) async throws -> String {
        guard let result = searchResult else { fatalError("MockEmergentBridge.searchResult not set") }
        switch result {
        case .success(let r): return try encodeOK(r)
        case .failure(let e): throw e
        }
    }

    func chat(handle: UInt32, requestJSON: String) async throws -> String {
        guard let result = chatResult else { fatalError("MockEmergentBridge.chatResult not set") }
        switch result {
        case .success(let r): return try encodeOK(r)
        case .failure(let e): throw e
        }
    }

    func listDocuments(handle: UInt32, requestJSON: String) async throws -> String {
        guard let result = listDocumentsResult else { fatalError("MockEmergentBridge.listDocumentsResult not set") }
        switch result {
        case .success(let r): return try encodeOK(r)
        case .failure(let e): throw e
        }
    }

    // Encode a result into the standard bridge envelope format.
    private func encodeOK<T: Encodable>(_ value: T) throws -> String {
        struct Wrapper<T: Encodable>: Encodable { let result: T }
        let data = try JSONEncoder().encode(Wrapper(result: value))
        return String(data: data, encoding: .utf8)!
    }
}
