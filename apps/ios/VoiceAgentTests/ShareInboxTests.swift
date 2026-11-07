import Foundation
@testable import Memory
import Testing

/// A stub for the app-side drain suite, separate from the client suite's stub:
/// `@Suite(.serialized)` serializes *within* a suite, not across suites, so two
/// suites sharing one static handler would overwrite each other mid-drain.
final nonisolated class ShareInboxStubProtocol: URLProtocol {
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

/// The app-side drain: the only path that can deliver a file capture, and the
/// retry path for anything shared while offline.
@Suite(.serialized) @MainActor
struct ShareInboxTests {
    private final class Recorder: @unchecked Sendable {
        nonisolated(unsafe) var paths: [String] = []
    }

    private let connection = ShareConnection(
        apiBaseURL: "http://gateway.local:8080",
        apiKey: "emt_dev_key",
        projectID: "p1",
        projectName: "Home"
    )

    private func scratchQueue() -> ShareQueue {
        let directory = FileManager.default.temporaryDirectory
            .appending(path: "share-inbox-\(UUID().uuidString)", directoryHint: .isDirectory)
        return ShareQueue(directory: directory)!
    }

    private func session(
        recorder: Recorder,
        status: Int = 202,
        body: String = #"{"run_id":"run-1"}"#
    ) -> URLSession {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [ShareInboxStubProtocol.self]
        ShareInboxStubProtocol.handler = { request in
            recorder.paths.append(request.url?.path ?? "")
            let response = HTTPURLResponse(
                url: request.url!, statusCode: status, httpVersion: nil, headerFields: nil
            )!
            return (response, Data(body.utf8))
        }
        return URLSession(configuration: configuration)
    }

    @Test func drainsAQueuedLinkAndAQueuedFile() async throws {
        let queue = scratchQueue()
        let recorder = Recorder()
        // Explicit, distinct timestamps: the drain order is the shared order.
        try queue.add(
            ShareCapture(
                createdAt: Date(timeIntervalSince1970: 1_700_000_000),
                kind: .link, url: "https://example.com/a", comment: "note"
            )
        )
        try queue.add(
            ShareCapture(
                createdAt: Date(timeIntervalSince1970: 1_700_000_001),
                kind: .file, fileName: "scan.jpg", contentType: "image/jpeg"
            ),
            payload: Data("jpeg".utf8)
        )
        let inbox = ShareInbox(
            queue: queue, session: session(recorder: recorder), connection: { connection }
        )
        #expect(inbox.pendingCount == 2)

        await inbox.drain()

        #expect(inbox.pendingCount == 0)
        #expect(recorder.paths == [
            "/api/projects/p1/remember",
            "/api/projects/p1/remember/file",
        ])
    }

    /// A refusal of one item marks it failed (so the inbox can show the reason)
    /// and does not stop the next, unrelated capture from being delivered.
    @Test func aRejectionIsKeptAndTheDrainContinues() async throws {
        let queue = scratchQueue()
        let recorder = Recorder()
        try queue.add(
            ShareCapture(
                createdAt: Date(timeIntervalSince1970: 1_700_000_000),
                kind: .link, url: "https://example.com/a"
            )
        )
        try queue.add(
            ShareCapture(
                createdAt: Date(timeIntervalSince1970: 1_700_000_001),
                kind: .link, url: "https://example.com/b"
            )
        )
        ShareInboxStubProtocol.handler = { request in
            recorder.paths.append(request.url?.path ?? "")
            // The first capture is refused, the second accepted.
            let refused = recorder.paths.count == 1
            let response = HTTPURLResponse(
                url: request.url!, statusCode: refused ? 422 : 202, httpVersion: nil,
                headerFields: nil
            )!
            let body = refused
                ? #"{"error":{"code":"no_content","message":"Nothing to remember."}}"#
                : #"{"run_id":"run-1"}"#
            return (response, Data(body.utf8))
        }
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [ShareInboxStubProtocol.self]
        let inbox = ShareInbox(
            queue: queue, session: URLSession(configuration: configuration),
            connection: { connection }
        )

        await inbox.drain()

        #expect(recorder.paths.count == 2)
        try #require(inbox.pendingCount == 1)
        #expect(inbox.captures.first?.state == .failed)
        #expect(inbox.captures.first?.lastError == "Nothing to remember.")
        #expect(inbox.hasFailures)
    }

    /// Nothing is attempted while the device is not connected: the captures wait
    /// instead of being marked permanently failed.
    @Test func anUnconnectedDeviceLeavesTheQueueAlone() async throws {
        let queue = scratchQueue()
        let recorder = Recorder()
        try queue.add(ShareCapture(kind: .link, url: "https://example.com/a"))
        let inbox = ShareInbox(
            queue: queue, session: session(recorder: recorder), connection: { nil }
        )

        await inbox.drain()

        #expect(inbox.pendingCount == 1)
        #expect(inbox.captures.first?.state == .queued)
        #expect(recorder.paths.isEmpty)
    }

    @Test func discardRemovesWithoutUploading() throws {
        let queue = scratchQueue()
        let recorder = Recorder()
        let capture = ShareCapture(kind: .link, url: "https://example.com/a")
        try queue.add(capture)
        let inbox = ShareInbox(
            queue: queue, session: session(recorder: recorder), connection: { connection }
        )

        inbox.discard(capture.id)

        #expect(inbox.pendingCount == 0)
        #expect(recorder.paths.isEmpty)
    }
}
