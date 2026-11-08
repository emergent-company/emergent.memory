import Foundation
import OSLog

/// The on-disk capture queue in the App Group container.
///
/// One directory, one record per capture: `<uuid>.json` holds the metadata and
/// `<uuid>.payload` holds the file bytes. File-per-capture (rather than one
/// manifest) means a process killed mid-write can lose one record, never the
/// queue, and the app and the extension never need a lock: the extension only
/// ever adds, the app only ever updates or deletes.
///
/// The payload is written **before** the metadata: a crash between the two
/// leaves an orphan payload (cleaned by ``pending()``), never a record whose
/// file is missing.
struct ShareQueue: Sendable {
    /// Where the records live inside the group container.
    static let directoryName = "ShareQueue"

    /// Hard cap on queued captures. A queue is a short-lived handoff, not an
    /// archive; without a cap a device that never gets back online grows one
    /// forever.
    static let maxCaptures = 100

    let directory: URL

    /// The queue in the group container, or nil when the group is unavailable
    /// (unprovisioned build) — callers treat nil as "cannot capture here".
    init?(directory: URL? = ShareQueue.defaultDirectory) {
        guard let directory else { return nil }
        self.directory = directory
    }

    /// `<group container>/ShareQueue`.
    static var defaultDirectory: URL? {
        ShareGroup.containerURL?.appending(path: directoryName, directoryHint: .isDirectory)
    }

    // MARK: - Reading

    /// Every queued capture, oldest first. Undecodable records and orphan
    /// payloads are cleaned up rather than shown as corruption.
    func pending() -> [ShareCapture] {
        guard let names = try? FileManager.default.contentsOfDirectory(atPath: directory.path) else {
            return []
        }
        var captures: [ShareCapture] = []
        var known = Set<String>()
        for name in names where name.hasSuffix(".json") {
            let id = String(name.dropLast(".json".count))
            known.insert(id)
            guard let data = try? Data(contentsOf: directory.appending(path: name)),
                  let capture = try? Self.decoder().decode(ShareCapture.self, from: data)
            else {
                // A truncated or future-versioned record: drop it rather than
                // leaving the inbox permanently unhappy.
                try? FileManager.default.removeItem(at: directory.appending(path: name))
                continue
            }
            captures.append(capture)
        }
        // An orphan payload has no record to upload it: it can only be a crash
        // leftover, so reclaim the space.
        for name in names where name.hasSuffix(".payload") {
            let id = String(name.dropLast(".payload".count))
            if !known.contains(id) {
                try? FileManager.default.removeItem(at: directory.appending(path: name))
            }
        }
        return captures.sorted { $0.createdAt < $1.createdAt }
    }

    /// The queued payload for `id`, if the record has one.
    func payload(for id: UUID) -> Data? {
        try? Data(contentsOf: payloadURL(for: id))
    }

    func payloadURL(for id: UUID) -> URL {
        directory.appending(path: "\(id.uuidString).payload")
    }

    // MARK: - Writing

    /// Appends a capture (and its payload, if any).
    ///
    /// When the queue is at ``maxCaptures`` the oldest capture is dropped: a
    /// shared item that could not be delivered for that long is stale, and
    /// silently discarding the *newest* share would be worse.
    func add(_ capture: ShareCapture, payload: Data? = nil) throws {
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)

        if let payload {
            try payload.write(to: payloadURL(for: capture.id), options: .atomic)
        }
        try encode(capture).write(to: metadataURL(for: capture.id), options: .atomic)

        trim()
    }

    /// Rewrites one record (state, attempts, last error).
    func update(_ capture: ShareCapture) throws {
        try encode(capture).write(to: metadataURL(for: capture.id), options: .atomic)
    }

    /// Removes a capture and its payload. Idempotent.
    func remove(_ id: UUID) {
        try? FileManager.default.removeItem(at: metadataURL(for: id))
        try? FileManager.default.removeItem(at: payloadURL(for: id))
    }

    private func metadataURL(for id: UUID) -> URL {
        directory.appending(path: "\(id.uuidString).json")
    }

    private func encode(_ capture: ShareCapture) throws -> Data {
        try Self.encoder().encode(capture)
    }

    /// Encoder and decoder share the date strategy so a record written by one
    /// build always decodes in another (`.iso8601` here, not the encoder's
    /// default of a raw time interval).
    private static func encoder() -> JSONEncoder {
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        return encoder
    }

    private static func decoder() -> JSONDecoder {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        return decoder
    }

    private func trim() {
        let captures = pending()
        guard captures.count > Self.maxCaptures else { return }
        for capture in captures.prefix(captures.count - Self.maxCaptures) {
            remove(capture.id)
        }
    }
}

/// Logging for the share path.
///
/// A separate `Logger` from the app's `Log` helpers (which the extension does
/// not compile), and deliberately narrow: the share extension's log is what a
/// field report of "it says it saved but never appeared" is diagnosed from.
enum ShareLog {
    static let capture = Logger(subsystem: "com.emergent.memory", category: "share")
}
