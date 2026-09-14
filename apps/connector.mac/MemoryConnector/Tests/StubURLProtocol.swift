import Foundation
@testable import MemoryConnector

/// Result a stub handler returns for a request.
struct StubResult: Sendable {
    let statusCode: Int
    let data: Data
    let error: URLError?

    static func ok(_ json: String) -> StubResult {
        StubResult(statusCode: 200, data: Data(json.utf8), error: nil)
    }

    static func status(_ code: Int, json: String = "{}") -> StubResult {
        StubResult(statusCode: code, data: Data(json.utf8), error: nil)
    }

    static func failure(_ error: URLError) -> StubResult {
        StubResult(statusCode: 0, data: Data(), error: error)
    }
}

/// URLProtocol stub: no real network. Handler + captured request live in a
/// lock-protected registry so background URLProtocol callbacks stay safe.
final class StubURLProtocol: URLProtocol {

    final class Registry: @unchecked Sendable {
        private let lock = NSLock()
        private var handler: ((URLRequest) -> StubResult)?
        private var lastRequest: URLRequest?
        private var allRequests: [URLRequest] = []

        func setHandler(_ handler: @escaping (URLRequest) -> StubResult) {
            lock.lock(); self.handler = handler; lock.unlock()
        }

        func currentHandler() -> ((URLRequest) -> StubResult)? {
            lock.lock(); defer { lock.unlock() }; return handler
        }

        func record(_ request: URLRequest) {
            lock.lock(); lastRequest = request; allRequests.append(request); lock.unlock()
        }

        var capturedRequest: URLRequest? {
            lock.lock(); defer { lock.unlock() }; return lastRequest
        }

        /// Every request seen this test, in order (for multi-step flows).
        var capturedRequests: [URLRequest] {
            lock.lock(); defer { lock.unlock() }; return allRequests
        }

        func reset() {
            lock.lock(); handler = nil; lastRequest = nil; allRequests = []; lock.unlock()
        }
    }

    static let registry = Registry()

    /// A URLSession whose only protocol is this stub.
    static func makeSession() -> URLSession {
        let config = URLSessionConfiguration.ephemeral
        config.protocolClasses = [StubURLProtocol.self]
        return URLSession(configuration: config)
    }

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        guard let handler = StubURLProtocol.registry.currentHandler() else {
            client?.urlProtocol(self, didFailWithError: URLError(.unsupportedURL))
            return
        }
        StubURLProtocol.registry.record(request)
        let result = handler(request)
        if let error = result.error {
            client?.urlProtocol(self, didFailWithError: error)
            return
        }
        guard let response = HTTPURLResponse(
            url: request.url ?? URL(fileURLWithPath: "/"),
            statusCode: result.statusCode,
            httpVersion: "HTTP/1.1",
            headerFields: ["Content-Type": "application/json"]
        ) else {
            client?.urlProtocol(self, didFailWithError: URLError(.badServerResponse))
            return
        }
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: result.data)
        client?.urlProtocolDidFinishLoading(self)
    }

    override func stopLoading() {}
}
