import Foundation
@testable import Memory
import Testing

/// A `URLProtocol` stub local to the share-capture client tests, with its own
/// handler so it cannot collide with the other suites' stubs.
final nonisolated class ShareCaptureStubProtocol: URLProtocol {
    nonisolated(unsafe) static var handler: (@Sendable (URLRequest) throws -> (URLResponse, Data))?

    override static func canInit(with _: URLRequest) -> Bool {
        true
    }

    override static func canonicalRequest(for request: URLRequest) -> URLRequest {
        request
    }

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

/// The HTTP half: where a capture is posted and how Memory's answer is read.
@Suite(.serialized) @MainActor
struct ShareCaptureClientTests {
    private final class RequestRecorder: @unchecked Sendable {
        nonisolated(unsafe) var requests: [URLRequest] = []
        nonisolated(unsafe) var bodies: [Data] = []
    }

    private let connection = ShareConnection(
        apiBaseURL: "http://gateway.local:8080",
        apiKey: "emt_dev_key",
        projectID: "p1",
        projectName: "Home"
    )

    private func makeClient(
        recorder: RequestRecorder,
        status: Int = 202,
        body: String = #"{"run_id":"run-1","status":"queued"}"#
    ) -> ShareCaptureClient {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [ShareCaptureStubProtocol.self]
        ShareCaptureStubProtocol.handler = { request in
            recorder.requests.append(request)
            recorder.bodies.append(requestBodyData(request) ?? Data())
            let response = HTTPURLResponse(
                url: request.url!, statusCode: status, httpVersion: nil, headerFields: nil
            )!
            return (response, Data(body.utf8))
        }
        return ShareCaptureClient(
            connection: connection, session: URLSession(configuration: configuration)
        )
    }

    @Test func rememberURLScopesToTheProject() throws {
        let client = ShareCaptureClient(connection: connection)
        #expect(
            try client.rememberURL().absoluteString
                == "http://gateway.local:8080/api/projects/p1/remember"
        )
        #expect(
            try client.rememberURL(appending: "file").absoluteString
                == "http://gateway.local:8080/api/projects/p1/remember/file"
        )
    }

    @Test func rememberPostsTheCredentialAndTheFramedBody() async throws {
        let recorder = RequestRecorder()
        let client = makeClient(recorder: recorder)
        let capture = ShareCapture(
            kind: .link,
            url: "https://example.com/a",
            title: "An article",
            text: "selected words",
            comment: "for the reading list"
        )

        let runID = try await client.remember(capture)

        #expect(runID == "run-1")
        let request = try #require(recorder.requests.first)
        #expect(request.httpMethod == "POST")
        #expect(request.url?.path == "/api/projects/p1/remember")
        #expect(request.value(forHTTPHeaderField: "X-API-Key") == "emt_dev_key")
        #expect(request.value(forHTTPHeaderField: "Content-Type") == "application/json")

        let bodyData = try #require(recorder.bodies.first)
        let object = try JSONSerialization.jsonObject(with: bodyData)
        let body = try #require(object as? [String: Any])
        #expect(body["url"] as? String == "https://example.com/a")
        #expect(body["title"] as? String == "An article")
        #expect(body["text"] as? String == "selected words")
        #expect(body["comment"] as? String == "for the reading list")
    }

    /// The file upload carries the comment in the gateway's `comment` part,
    /// which the gateway maps onto Memory's `guide` — what fills the document
    /// when the parser finds no text in a photo.
    @Test func rememberFilePostsMultipartWithTheComment() async throws {
        let recorder = RequestRecorder()
        let client = makeClient(recorder: recorder)
        let capture = ShareCapture(
            kind: .file,
            comment: "receipt for the train",
            fileName: "IMG_0001.jpg",
            contentType: "image/jpeg"
        )

        _ = try await client.rememberFile(capture, payload: Data("jpeg-bytes".utf8))

        let request = try #require(recorder.requests.first)
        #expect(request.url?.path == "/api/projects/p1/remember/file")
        #expect(request.value(forHTTPHeaderField: "X-API-Key") == "emt_dev_key")
        let contentType = try #require(request.value(forHTTPHeaderField: "Content-Type"))
        #expect(contentType.hasPrefix("multipart/form-data; boundary="))

        let bodyData = try #require(recorder.bodies.first)
        let body = try #require(String(data: bodyData, encoding: .utf8))
        #expect(body.contains(#"name="file"; filename="IMG_0001.jpg""#))
        #expect(body.contains("Content-Type: image/jpeg"))
        #expect(body.contains("jpeg-bytes"))
        #expect(body.contains(#"name="comment""#))
        #expect(body.contains("receipt for the train"))
    }

    /// An empty comment must not produce an empty `comment` part: the gateway
    /// would forward it as an empty Memory `guide` and store a blank document
    /// instead of the `no_content` refusal that tells the user why.
    @Test func multipartOmitsAnEmptyComment() {
        let body = ShareCaptureClient.multipartBody(
            boundary: "B",
            fileName: "a.pdf",
            contentType: "application/pdf",
            payload: Data("pdf".utf8),
            comment: "   "
        )
        let text = String(data: body, encoding: .utf8) ?? ""
        #expect(!text.contains(#"name="comment""#))
        #expect(text.contains(#"name="file""#))
    }

    /// A filename from a provider can contain quotes or newlines, which would
    /// end the part header early and corrupt the multipart body.
    @Test func multipartSanitisesTheFilename() {
        let body = ShareCaptureClient.multipartBody(
            boundary: "B",
            fileName: "we\"ird\r\nname.jpg",
            contentType: "image/jpeg",
            payload: Data("x".utf8),
            comment: ""
        )
        let text = String(data: body, encoding: .utf8) ?? ""
        #expect(text.contains(#"filename="we_ird__name.jpg""#))
        #expect(!text.contains("\r\nname.jpg"))
    }

    /// Memory's own refusal is what the user must see: `no_content` for a photo
    /// with no text and no comment, `unsupported_media_type` for a HEIC.
    @Test func surfacesMemorysRefusal() async {
        let recorder = RequestRecorder()
        let client = makeClient(
            recorder: recorder,
            status: 422,
            body: #"{"error":{"code":"no_content","message":"Nothing to remember."}}"#
        )
        let capture = ShareCapture(kind: .file, fileName: "a.heic")

        let error = await captureError {
            try await client.rememberFile(capture, payload: Data("x".utf8))
        }
        guard case let .rejected(status, message)? = error else {
            Issue.record("expected a rejection, got \(String(describing: error))")
            return
        }
        #expect(status == 422)
        #expect(message == "Nothing to remember.")
        // A 4xx is Memory's answer about this item, not a reason to stall the
        // whole queue.
        #expect(!CaptureClientError.rejected(status: 422, message: "").haltsDrain)
        #expect(CaptureClientError.rejected(status: 503, message: "").haltsDrain)
    }

    @Test func surfacesAPlainGatewayError() async {
        let recorder = RequestRecorder()
        let client = makeClient(
            recorder: recorder,
            status: 401,
            body: #"{"error":"invalid api key"}"#
        )
        let capture = ShareCapture(kind: .link, url: "https://example.com")

        let error = await captureError { try await client.remember(capture) }
        guard case let .rejected(status, message)? = error else {
            Issue.record("expected a rejection, got \(String(describing: error))")
            return
        }
        #expect(status == 401)
        #expect(message == "invalid api key")
    }

    @Test func errorMessageFallsBackToTheStatus() {
        #expect(
            ShareCaptureClient.errorMessage(from: Data("not json".utf8), status: 500)
                == "Memory returned HTTP 500."
        )
    }

    /// A dead network is not item-specific: the drain stops rather than burning
    /// one timeout per queued capture.
    @Test func aTransportFailureHaltsTheDrain() {
        let error = CaptureClientError.transport("offline")
        #expect(error.isRetryable)
        #expect(error.haltsDrain)
        #expect(!CaptureClientError.configuration("no connection").isRetryable)
        #expect(CaptureClientError.configuration("no connection").haltsDrain)
    }

    @Test func anUnusableConnectionFailsBeforeTheNetwork() async {
        let client = ShareCaptureClient(
            connection: ShareConnection(
                apiBaseURL: "http://h:1", apiKey: "k", projectID: "", projectName: ""
            )
        )
        let error = await captureError {
            try await client.remember(ShareCapture(kind: .link, url: "https://example.com"))
        }
        guard case .configuration? = error else {
            Issue.record("expected a configuration failure")
            return
        }
    }
}

/// Runs `body`, returning the error it threw as a ``CaptureClientError`` when it
/// is one (nil when it threw something else or nothing at all).
@MainActor
private func captureError(
    from body: () async throws -> String?
) async -> CaptureClientError? {
    do {
        _ = try await body()
        return nil
    } catch let error as CaptureClientError {
        return error
    } catch {
        return nil
    }
}

/// URLProtocol exposes a request body as a stream, not as `httpBody`; read it so
/// request-shape tests can assert the multipart payload.
private nonisolated func requestBodyData(_ request: URLRequest) -> Data? {
    if let body = request.httpBody {
        return body
    }
    guard let stream = request.httpBodyStream else { return nil }
    stream.open()
    defer { stream.close() }
    var data = Data()
    let size = 4096
    let buffer = UnsafeMutablePointer<UInt8>.allocate(capacity: size)
    defer { buffer.deallocate() }
    while stream.hasBytesAvailable {
        let read = stream.read(buffer, maxLength: size)
        if read <= 0 {
            break
        }
        data.append(buffer, count: read)
    }
    return data
}
