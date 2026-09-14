import Foundation
import Testing
@testable import Memory

/// A `URLProtocol` stub that lets the transport tests drive `URLSession`
/// without hitting the network.
nonisolated final class GatewayHTTPStubProtocol: URLProtocol {
    nonisolated(unsafe) static var handler: (@Sendable (URLRequest) throws -> (URLResponse, Data))?

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        guard let handler = Self.handler else {
            client?.urlProtocol(self, didFailWithError: URLError(.unsupportedURL))
            return
        }
        do {
            let (response, data) = try handler(request)
            client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
            client?.urlProtocol(self, didLoad: data)
            client?.urlProtocolDidFinishLoading(self)
        } catch {
            client?.urlProtocol(self, didFailWithError: error)
        }
    }

    override func stopLoading() {}
}

/// Shared `handler` is mutated per test, so run the suite serialized.
@Suite(.serialized) struct GatewayHTTPTests {
    private func makeSession() -> URLSession {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [GatewayHTTPStubProtocol.self]
        return URLSession(configuration: configuration)
    }

    @Test func twoXXReturnsBodyAndResponse() async throws {
        GatewayHTTPStubProtocol.handler = { request in
            let response = HTTPURLResponse(
                url: request.url!,
                statusCode: 200,
                httpVersion: nil,
                headerFields: nil
            )!
            return (response, Data("ok".utf8))
        }
        defer { GatewayHTTPStubProtocol.handler = nil }

        let (data, response) = try await GatewayHTTP.send(
            method: "GET",
            url: URL(string: "http://example.test/api")!,
            apiKey: "secret",
            session: makeSession()
        )
        #expect(response.statusCode == 200)
        #expect(String(decoding: data, as: UTF8.self) == "ok")
    }

    @Test func requestCarriesAPIKeyHeader() async throws {
        GatewayHTTPStubProtocol.handler = { request in
            #expect(request.value(forHTTPHeaderField: "X-API-Key") == "secret")
            let response = HTTPURLResponse(
                url: request.url!,
                statusCode: 204,
                httpVersion: nil,
                headerFields: nil
            )!
            return (response, Data())
        }
        defer { GatewayHTTPStubProtocol.handler = nil }

        _ = try await GatewayHTTP.send(
            method: "GET",
            url: URL(string: "http://example.test/api")!,
            apiKey: "secret",
            session: makeSession()
        )
    }

    @Test func nonHTTPResponseThrowsTransportError() async {
        GatewayHTTPStubProtocol.handler = { request in
            let response = URLResponse(
                url: request.url!,
                mimeType: nil,
                expectedContentLength: 0,
                textEncodingName: nil
            )
            return (response, Data())
        }
        defer { GatewayHTTPStubProtocol.handler = nil }

        do {
            _ = try await GatewayHTTP.send(
                method: "GET",
                url: URL(string: "http://example.test/api")!,
                apiKey: "secret",
                session: makeSession()
            )
            Issue.record("Expected a GatewayHTTP.TransportError")
        } catch let error as GatewayHTTP.TransportError {
            guard case .noHTTPResponse = error.reason else {
                Issue.record("Expected .noHTTPResponse, got \(error.reason)")
                return
            }
        } catch {
            Issue.record("Unexpected error: \(error)")
        }
    }
}
