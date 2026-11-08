import Foundation

/// One thing the user shared into Memory from another app.
///
/// A capture is either a **link** (a URL, optionally with the page title and
/// selected text) or a **file** (a photo, a scanned PDF, an audio memo). The
/// user's comment rides along on both. Links are posted from the extension
/// straight away; files are written to the shared queue and uploaded by the app
/// (uploading inside an extension has a ~30 s budget, and Memory's file
/// remember polls document conversion for up to 120 s before it answers).
struct ShareCapture: Codable, Identifiable, Equatable, Sendable {
    enum Kind: String, Codable, Sendable {
        /// A shared link, selection or note — posted as JSON.
        case link
        /// A shared photo/PDF/audio file — uploaded as multipart.
        case file
    }

    enum State: String, Codable, Sendable {
        /// Waiting for the app to upload it.
        case queued
        /// An upload attempt failed; the capture is kept so the user can retry.
        case failed
    }

    let id: UUID
    let createdAt: Date
    var kind: Kind
    /// The shared link, when there is one (`https://…` only).
    var url: String?
    /// The page title the sharing app supplied (Safari sends one).
    var title: String?
    /// Shared text that is not the link itself (a selection, a note from the
    /// share sheet).
    var text: String?
    /// The comment the user wrote in the compose sheet. Never nil, possibly
    /// empty.
    var comment: String
    /// Original filename of the shared file (`IMG_0001.jpg`), used so Memory's
    /// parser sees the real extension.
    var fileName: String?
    /// MIME type of the payload, as sent to Memory.
    var contentType: String?
    /// Payload size in bytes, for the inbox row.
    var byteCount: Int
    var state: State
    /// How many upload attempts have failed, so the row can say so instead of
    /// looking permanently stuck.
    var attempts: Int
    /// The last refusal, shown in the inbox.
    var lastError: String?

    init(
        id: UUID = UUID(),
        createdAt: Date = Date(),
        kind: Kind,
        url: String? = nil,
        title: String? = nil,
        text: String? = nil,
        comment: String = "",
        fileName: String? = nil,
        contentType: String? = nil,
        byteCount: Int = 0,
        state: State = .queued,
        attempts: Int = 0,
        lastError: String? = nil
    ) {
        self.id = id
        self.createdAt = createdAt
        self.kind = kind
        self.url = url
        self.title = title
        self.text = text
        self.comment = comment
        self.fileName = fileName
        self.contentType = contentType
        self.byteCount = byteCount
        self.state = state
        self.attempts = attempts
        self.lastError = lastError
    }

    /// Whether there is anything to remember. A comment alone is enough: the
    /// user's note is the content.
    var hasContent: Bool {
        !(url ?? "").trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
            || !(text ?? "").trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
            || !comment.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
            || kind == .file
    }

    /// A one-line description for the inbox row: the page title, the link host,
    /// the filename, or the text itself.
    var summary: String {
        if let title, !title.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            return title
        }
        if let url, let host = URL(string: url)?.host(), !host.isEmpty {
            return host
        }
        if let fileName, !fileName.isEmpty {
            return fileName
        }
        if let text, !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            return text
        }
        return "Shared item"
    }

    /// `2.4 MB`-style payload size, empty for link captures.
    var sizeLabel: String {
        guard byteCount > 0 else { return "" }
        return ByteCountFormatter.string(fromByteCount: Int64(byteCount), countStyle: .file)
    }
}
