import Foundation
@testable import Memory
import Testing

/// The connection the app mirrors into the App Group and the extension reads.
@MainActor
struct ShareConnectionTests {
    private func scratchDefaults(_ name: String = UUID().uuidString) -> UserDefaults {
        let defaults = UserDefaults(suiteName: "share-tests.\(name)")!
        defaults.removePersistentDomain(forName: "share-tests.\(name)")
        return defaults
    }

    private let connection = ShareConnection(
        apiBaseURL: "http://gateway.local:8080",
        apiKey: "emt_dev_key",
        projectID: "p1",
        projectName: "Home"
    )

    @Test func mirrorsAndLoadsTheActiveConnection() {
        let defaults = scratchDefaults()
        ShareConnection.mirror(connection, to: defaults)
        #expect(ShareConnection.load(from: defaults) == connection)
        #expect(ShareConnection.load(from: defaults)?.displayName == "Home")
    }

    /// A capture needs all three of gateway, credential and project: a legacy
    /// identity-less connection cannot scope a remember call, so it must read as
    /// "not connected" rather than producing a request to a broken URL.
    @Test func anIncompleteConnectionIsUnusable() {
        let incomplete = ShareConnection(
            apiBaseURL: "http://gateway.local:8080",
            apiKey: "emt_dev_key",
            projectID: "",
            projectName: ""
        )
        #expect(!incomplete.isUsable)
        let defaults = scratchDefaults()
        ShareConnection.mirror(incomplete, to: defaults)
        #expect(ShareConnection.load(from: defaults) == nil)
    }

    /// Disconnecting the last project must clear the group, or the extension
    /// keeps capturing with a credential the user removed.
    @Test func mirroringNilClearsTheGroup() {
        let defaults = scratchDefaults()
        ShareConnection.mirror(connection, to: defaults)
        ShareConnection.mirror(nil, to: defaults)
        #expect(ShareConnection.load(from: defaults) == nil)
        #expect(defaults.string(forKey: ShareConnectionKey.apiKey) == nil)
    }

    @Test func displayNameFallsBackToTheProjectID() {
        let unnamed = ShareConnection(
            apiBaseURL: "http://h:1", apiKey: "k", projectID: "p1", projectName: "  "
        )
        #expect(unnamed.displayName == "p1")
    }
}

/// The on-disk queue: what the extension writes and the app drains.
@MainActor
struct ShareQueueTests {
    private func scratchQueue() -> ShareQueue {
        let directory = FileManager.default.temporaryDirectory
            .appending(path: "share-queue-\(UUID().uuidString)", directoryHint: .isDirectory)
        return ShareQueue(directory: directory)!
    }

    private func capture(
        _ kind: ShareCapture.Kind = .link,
        createdAt: Date = Date(),
        comment: String = "read later"
    ) -> ShareCapture {
        ShareCapture(
            createdAt: createdAt,
            kind: kind,
            url: kind == .link ? "https://example.com/article" : nil,
            title: "An article",
            comment: comment,
            fileName: kind == .file ? "page.jpg" : nil,
            contentType: kind == .file ? "image/jpeg" : nil,
            byteCount: kind == .file ? 4096 : 0
        )
    }

    @Test func roundTripsACaptureAndItsPayload() throws {
        let queue = scratchQueue()
        let link = capture()
        let file = capture(.file, createdAt: Date().addingTimeInterval(1))
        let payload = Data("jpeg-bytes".utf8)

        try queue.add(link)
        try queue.add(file, payload: payload)

        let pending = queue.pending()
        try #require(pending.count == 2)
        // Oldest first: a drain delivers in the order the user shared.
        #expect(pending[0].id == link.id)
        #expect(pending[0].comment == "read later")
        #expect(pending[1].id == file.id)
        #expect(pending[1].fileName == "page.jpg")
        #expect(queue.payload(for: file.id) == payload)
    }

    @Test func removeDropsTheRecordAndItsPayload() throws {
        let queue = scratchQueue()
        let file = capture(.file)
        try queue.add(file, payload: Data("bytes".utf8))

        queue.remove(file.id)

        #expect(queue.pending().isEmpty)
        #expect(queue.payload(for: file.id) == nil)
    }

    @Test func updateRewritesTheRecord() throws {
        let queue = scratchQueue()
        var file = capture(.file)
        try queue.add(file, payload: Data("bytes".utf8))

        file.state = .failed
        file.attempts = 2
        file.lastError = "no_content"
        try queue.update(file)

        let pending = queue.pending()
        try #require(pending.count == 1)
        #expect(pending[0].state == .failed)
        #expect(pending[0].attempts == 2)
        #expect(pending[0].lastError == "no_content")
    }

    /// A crash between the payload write and the metadata write leaves a
    /// payload with no record. It can never be delivered, so `pending()` must
    /// reclaim the space instead of leaking it.
    @Test func anOrphanPayloadIsReclaimed() throws {
        let queue = scratchQueue()
        let orphan = UUID()
        try FileManager.default.createDirectory(
            at: queue.directory, withIntermediateDirectories: true
        )
        try Data("left behind".utf8).write(to: queue.payloadURL(for: orphan))

        #expect(queue.pending().isEmpty)
        #expect(!FileManager.default.fileExists(atPath: queue.payloadURL(for: orphan).path))
    }

    /// A record written by a future build (or truncated by a kill) must not make
    /// the inbox permanently unreadable: it is dropped, not surfaced as an error.
    @Test func anUndecodableRecordIsDropped() throws {
        let queue = scratchQueue()
        try FileManager.default.createDirectory(
            at: queue.directory, withIntermediateDirectories: true
        )
        let broken = queue.directory.appending(path: "\(UUID().uuidString).json")
        try Data(#"{"kind":"link"}"#.utf8).write(to: broken)

        #expect(queue.pending().isEmpty)
        #expect(!FileManager.default.fileExists(atPath: broken.path))
    }

    /// The queue is a handoff, not an archive: the oldest capture is dropped so a
    /// device that never reconnects cannot grow it without bound.
    @Test func theQueueKeepsTheNewestCaptures() throws {
        let queue = scratchQueue()
        let base = Date(timeIntervalSince1970: 1_700_000_000)
        for index in 0 ... ShareQueue.maxCaptures {
            try queue.add(capture(createdAt: base.addingTimeInterval(Double(index))))
        }

        let pending = queue.pending()
        try #require(pending.count == ShareQueue.maxCaptures)
        // `maxCaptures + 1` were added one at a time, so the oldest survivor is
        // index 1.
        #expect(pending.first?.createdAt == base.addingTimeInterval(1))
    }

    /// Without an App Group there is no shared container, so there is nowhere
    /// safe to queue: callers must see nil rather than writing into a private
    /// container the extension would never read.
    @Test func aMissingDirectoryIsNotAQueue() {
        let queue: ShareQueue? = ShareQueue(directory: nil)
        #expect(queue?.directory == nil)
    }
}

/// The capture model's own presentation rules.
@MainActor
struct ShareCaptureModelTests {
    @Test func summaryPrefersTitleThenHostThenFilename() {
        var capture = ShareCapture(kind: .link, url: "https://example.com/a", title: "A title")
        #expect(capture.summary == "A title")

        capture.title = "  "
        #expect(capture.summary == "example.com")

        capture.url = nil
        capture.fileName = "scan.pdf"
        #expect(capture.summary == "scan.pdf")
    }

    @Test func aCommentAloneIsContent() {
        let note = ShareCapture(kind: .link, comment: "remember to call the plumber")
        #expect(note.hasContent)
        #expect(!ShareCapture(kind: .link, comment: "   ").hasContent)
        // A file capture is content even before its comment exists.
        #expect(ShareCapture(kind: .file).hasContent)
    }
}
