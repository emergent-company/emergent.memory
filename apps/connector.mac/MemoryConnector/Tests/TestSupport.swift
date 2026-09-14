import Foundation
@testable import MemoryConnector

/// Shared test helpers.
enum HTTPBodyReader {
    /// Reads a URLRequest body as a string, whether it is carried as
    /// `httpBody` or as an `httpBodyStream` (URLProtocol sees the latter).
    /// Nonisolated and stateless so any test class can use it under Swift 6.
    static func string(from request: URLRequest) -> String {
        if let data = request.httpBody {
            return String(data: data, encoding: .utf8) ?? ""
        }
        guard let stream = request.httpBodyStream else { return "" }
        stream.open()
        defer { stream.close() }
        var data = Data()
        let size = 1024
        let buffer = UnsafeMutablePointer<UInt8>.allocate(capacity: size)
        defer { buffer.deallocate() }
        while stream.hasBytesAvailable {
            let read = stream.read(buffer, maxLength: size)
            if read <= 0 { break }
            data.append(buffer, count: read)
        }
        return String(data: data, encoding: .utf8) ?? ""
    }
}

/// Fake authenticator: runs `handler` on the URL it was asked to open. Tests
/// set the handler before calling; it is only touched on the main actor.
final class FakeAuthenticator: BrowserAuthenticator, @unchecked Sendable {
    var handler: ((URL) -> Result<URL, Error>) = { _ in .failure(OIDCError.cancelled) }
    private(set) var openedURLs: [URL] = []

    func authenticate(url: URL, callbackURLScheme: String) async throws -> URL {
        openedURLs.append(url)
        return try handler(url).get()
    }
}

/// Locked per-path request counter for stub handlers.
final class RequestCounter: @unchecked Sendable {
    private let lock = NSLock()
    private var counts: [String: Int] = [:]

    func increment(_ path: String) {
        lock.withLock { counts[path, default: 0] += 1 }
    }

    func count(_ path: String) -> Int {
        lock.withLock { counts[path] ?? 0 }
    }
}

/// Resets connector preference state between hosted tests.
///
/// Secrets are owned by the connector CLI now, so tests only need to clear the
/// non-secret `UserDefaults` keys the app still writes. Kept as a named helper
/// so every test calls the same reset.
enum ConnectorKeychainCleanup {
    /// Non-secret preference keys owned by the connector stores.
    static let preferenceKeys: [String] = []

    static func clearAll(defaults: UserDefaults? = nil) {
        if let defaults {
            for key in preferenceKeys {
                defaults.removeObject(forKey: key)
            }
        }
    }
}
