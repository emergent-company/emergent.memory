import Foundation

/// The compose sheet's state machine: what the user is sharing, the comment
/// they type, and where the capture went.
///
/// The save path is deliberately split by kind:
///
/// - **Link / note** — posted from the extension straight away, because Memory
///   answers a text remember with `202 {run_id}` as soon as the run exists. A
///   transport failure (offline, server restarting) queues it instead, and the
///   app retries on its next foreground.
/// - **File** — always queued. Uploading from here is not possible: Memory's
///   file remember polls document conversion for up to 120 s before it answers,
///   and a share extension is killed at about 30 s. The app uploads it.
@MainActor
final class ShareComposeModel: ObservableObject {
    /// Where the save got to.
    enum Outcome: Equatable {
        case composing
        case saving
        /// Delivered (or queued for the app to deliver).
        case saved(queued: Bool)
        /// Refused, and retrying the same capture would not help.
        case failed(String)
    }

    let payload: SharedPayload
    let connection: ShareConnection?
    private let queue: ShareQueue?

    @Published var comment = ""
    @Published var outcome: Outcome = .composing

    init(payload: SharedPayload, connection: ShareConnection?, queue: ShareQueue? = ShareQueue()) {
        self.payload = payload
        self.connection = connection
        self.queue = queue
    }

    var isSaving: Bool {
        outcome == .saving
    }

    /// A capture needs content: what was shared, or a comment about it.
    var canSave: Bool {
        !isSaving && (payload.hasContent || !trimmedComment.isEmpty)
    }

    /// Where it will go, for the sheet's footer.
    var destination: String {
        guard let connection else {
            return "Not connected — connect a project in Memory, then this uploads."
        }
        return "Remember in \(connection.displayName)"
    }

    private var trimmedComment: String {
        comment.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    // MARK: - Saving

    func save() async {
        guard canSave else { return }
        outcome = .saving

        let capture = makeCapture()

        if payload.isFile {
            // Never upload from the extension: see the type comment.
            if let message = enqueue(capture, payload: payload.fileData) {
                outcome = .failed(message)
            } else {
                outcome = .saved(queued: true)
            }
            return
        }

        guard let connection, connection.isUsable else {
            // No credential yet: keep it, so connecting later still delivers the
            // share instead of silently dropping it.
            outcome = queueOnly(capture)
            return
        }

        do {
            try await ShareCaptureClient(connection: connection).remember(capture)
            outcome = .saved(queued: false)
        } catch let error as CaptureClientError {
            if error.isRetryable {
                outcome = queueOnly(capture)
            } else {
                outcome = .failed(error.localizedDescription)
            }
        } catch {
            outcome = .failed(error.localizedDescription)
        }
    }

    /// Queues a capture the network could not take, reporting either the queued
    /// state or the queue's own failure.
    private func queueOnly(_ capture: ShareCapture) -> Outcome {
        if let message = enqueue(capture, payload: nil) {
            return .failed(message)
        }
        return .saved(queued: true)
    }

    private func makeCapture() -> ShareCapture {
        var text = payload.text?.trimmingCharacters(in: .whitespacesAndNewlines)
        // A share sheet can hand over a whole page of visible text. The capture
        // is a note, not an archive: keep it to something a document can index.
        if let trimmed = text, trimmed.count > Self.maxTextLength {
            text = String(trimmed.prefix(Self.maxTextLength))
        }
        let link = payload.url.flatMap { url -> String? in
            guard let scheme = url.scheme?.lowercased(), scheme == "http" || scheme == "https" else {
                return nil
            }
            return url.absoluteString
        }
        return ShareCapture(
            kind: payload.isFile ? .file : .link,
            url: link,
            title: payload.title,
            text: text,
            comment: trimmedComment,
            fileName: payload.fileName,
            contentType: payload.contentType,
            byteCount: payload.fileData?.count ?? 0
        )
    }

    /// Writes the capture to the shared queue. Returns nil on success, or the
    /// message to show when the queue could not be written.
    private func enqueue(_ capture: ShareCapture, payload: Data?) -> String? {
        guard let queue else {
            return "Memory is not set up on this device, so this could not be saved."
        }
        do {
            try queue.add(capture, payload: payload)
            ShareLog.capture.info(
                "queued \(capture.kind.rawValue, privacy: .public) capture \(capture.id.uuidString, privacy: .public)"
            )
            return nil
        } catch {
            ShareLog.capture.error("queue write failed: \(error.localizedDescription, privacy: .public)")
            return "This could not be saved: \(error.localizedDescription)"
        }
    }

    /// Longest shared text kept verbatim.
    static let maxTextLength = 20000
}
