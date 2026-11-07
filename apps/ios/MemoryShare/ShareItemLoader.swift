import Foundation
import UIKit
import UniformTypeIdentifiers

/// What the share sheet handed us, normalised.
///
/// The share sheet is deliberately loose: Safari hands over a URL plus the page
/// title, Photos hands over a file-backed image (often HEIC), Files hands over a
/// security-scoped file of any type, and some apps only supply text. This
/// flattens all of that into the one thing the compose sheet can render.
struct SharedPayload {
    enum Kind: Equatable {
        /// A link (optionally with selected text and a page title).
        case link
        /// A file to upload.
        case file
        /// Plain text that is not a link.
        case text
    }

    var kind: Kind
    var url: URL?
    var title: String?
    var text: String?
    /// File bytes, copied out of the provider's temporary location.
    var fileData: Data?
    var fileName: String?
    var contentType: String?

    var isFile: Bool {
        kind == .file
    }

    var hasContent: Bool {
        switch kind {
        case .link: url != nil
        case .file: fileData != nil
        case .text: !(text ?? "").trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        }
    }

    /// What the compose sheet shows above the comment field.
    var previewTitle: String {
        switch kind {
        case .link:
            if let title, !title.isEmpty {
                return title
            }
            return url?.host() ?? url?.absoluteString ?? ""
        case .file:
            return fileName ?? "Shared file"
        case .text:
            return (text ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        }
    }

    var previewDetail: String {
        switch kind {
        case .link:
            var parts: [String] = []
            if let host = url?.host() {
                parts.append(host)
            }
            if let text, !text.isEmpty {
                parts.append(text.count > 120 ? String(text.prefix(120)) + "…" : text)
            }
            return parts.joined(separator: " · ")
        case .file:
            let size = ByteCountFormatter.string(
                fromByteCount: Int64(fileData?.count ?? 0), countStyle: .file
            )
            return [contentType ?? "file", size].joined(separator: " · ")
        case .text:
            let text = text ?? ""
            return text.count > 160 ? String(text.prefix(160)) + "…" : text
        }
    }

    var symbolName: String {
        switch kind {
        case .link: "link"
        case .file: "doc"
        case .text: "text.quote"
        }
    }
}

/// Reads a share sheet's `NSExtensionItem`s into a ``SharedPayload``.
///
/// Every provider callback is bridged to async. The file path reads the bytes
/// **inside** `loadFileRepresentation`'s completion handler: the URL it supplies
/// is a temporary file that is deleted as soon as the handler returns, so
/// deferring the read is the classic way to get an empty upload.
enum ShareItemLoader {
    /// Largest file the extension will accept. Matches the gateway's capture
    /// cap; the app re-checks it before upload.
    static let maxFileBytes = 25 * 1024 * 1024

    static func load(from items: [NSExtensionItem]) async -> SharedPayload? {
        let attachments = items.flatMap { $0.attachments ?? [] }
        guard !attachments.isEmpty else {
            // No attachments at all: some apps only set the content text.
            if let text = items.compactMap({ $0.attributedContentText?.string }).first,
               !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
            {
                return SharedPayload(kind: .text, title: title(from: items), text: text)
            }
            return nil
        }

        let title = title(from: items)

        if let urlAttachment = attachments.first(where: { isURL($0) }),
           let url = await loadURL(from: urlAttachment)
        {
            return await SharedPayload(
                kind: .link,
                url: url,
                title: title,
                text: loadText(from: attachments.first(where: isPureText))
            )
        }

        // Selected text is checked before the file path: plain text conforms to
        // `public.data`, so a text-first share would otherwise be uploaded as a
        // file with no name.
        if let textAttachment = attachments.first(where: isPureText),
           let text = await loadText(from: textAttachment)
        {
            if let url = URL(string: text), url.scheme?.hasPrefix("http") == true {
                return SharedPayload(kind: .link, url: url, title: title)
            }
            return SharedPayload(kind: .text, title: title, text: text)
        }

        if let fileAttachment = attachments.first(where: isFileLike),
           let file = await loadFile(from: fileAttachment)
        {
            return SharedPayload(
                kind: .file,
                title: title,
                fileData: file.data,
                fileName: file.name,
                contentType: file.contentType
            )
        }

        return nil
    }

    // MARK: - Type predicates

    private static func isURL(_ attachment: NSItemProvider) -> Bool {
        attachment.hasItemConformingToTypeIdentifier(UTType.url.identifier)
    }

    private static func isText(_ attachment: NSItemProvider) -> Bool {
        attachment.hasItemConformingToTypeIdentifier(UTType.plainText.identifier)
    }

    /// Text that is not also a document/image: a selection or a note.
    private static func isPureText(_ attachment: NSItemProvider) -> Bool {
        guard isText(attachment) else { return false }
        let media: [UTType] = [.image, .pdf, .audio]
        return !media.contains { attachment.hasItemConformingToTypeIdentifier($0.identifier) }
    }

    private static func isFileLike(_ attachment: NSItemProvider) -> Bool {
        let candidates: [UTType] = [.image, .pdf, .audio, .data]
        return candidates.contains { attachment.hasItemConformingToTypeIdentifier($0.identifier) }
    }

    // MARK: - Titles

    /// The page title the sharing app supplied. Safari puts it in
    /// `attributedTitle`; some apps only set `attributedContentText`.
    private static func title(from items: [NSExtensionItem]) -> String? {
        for item in items {
            if let title = item.attributedTitle?.string.trimmingCharacters(in: .whitespacesAndNewlines),
               !title.isEmpty
            {
                return title
            }
        }
        return nil
    }

    // MARK: - Loading

    private static func loadURL(from attachment: NSItemProvider) async -> URL? {
        await withCheckedContinuation { continuation in
            attachment.loadItem(forTypeIdentifier: UTType.url.identifier) { item, _ in
                switch item {
                case let url as URL:
                    continuation.resume(returning: url)
                case let url as NSURL:
                    continuation.resume(returning: url as URL)
                case let data as Data:
                    continuation.resume(returning: URL(string: String(data: data, encoding: .utf8) ?? ""))
                case let string as String:
                    continuation.resume(returning: URL(string: string))
                default:
                    continuation.resume(returning: nil)
                }
            }
        }
    }

    private static func loadText(from attachment: NSItemProvider?) async -> String? {
        guard let attachment else { return nil }
        return await withCheckedContinuation { continuation in
            attachment.loadItem(forTypeIdentifier: UTType.plainText.identifier) { item, _ in
                switch item {
                case let string as String:
                    continuation.resume(returning: string)
                case let data as Data:
                    continuation.resume(returning: String(data: data, encoding: .utf8))
                default:
                    continuation.resume(returning: nil)
                }
            }
        }
    }

    private struct LoadedFile {
        let name: String
        let contentType: String
        let data: Data
    }

    /// Copies the provider's file into memory, transcoding images to JPEG on
    /// the way (Memory cannot convert HEIC, and iPhone photos are HEIC).
    private static func loadFile(from attachment: NSItemProvider) async -> LoadedFile? {
        let type = fileType(for: attachment)
        guard let raw = await loadFileData(from: attachment, type: type) else { return nil }

        if type.conforms(to: .image) || ShareImage.isConvertibleImageType(type.preferredMIMEType) {
            guard let jpeg = ShareImage.jpegData(from: raw) else { return nil }
            let base = (attachment.suggestedName.flatMap { $0.isEmpty ? nil : $0 } ?? "shared-photo")
                .deletingPathExtension
            return LoadedFile(
                name: "\(base.isEmpty ? "shared-photo" : base).jpg",
                contentType: "image/jpeg",
                data: jpeg
            )
        }

        guard raw.count <= maxFileBytes else { return nil }
        let name = attachment.suggestedName.flatMap { $0.isEmpty ? nil : $0 }
            ?? "shared-capture.\(type.preferredFilenameExtension ?? "bin")"
        return LoadedFile(
            name: name,
            contentType: type.preferredMIMEType ?? "application/octet-stream",
            data: raw
        )
    }

    /// The attachment's most specific type we recognise, so the upload keeps the
    /// real extension (`invoice.pdf`, not `public.data`).
    private static func fileType(for attachment: NSItemProvider) -> UTType {
        let preferred: [UTType] = [.image, .pdf, .audio]
        for type in preferred where attachment.hasItemConformingToTypeIdentifier(type.identifier) {
            // Use the concrete registered identifier when the provider names
            // one (image/jpeg, com.adobe.pdf), falling back to the family.
            if let concrete = attachment.registeredTypeIdentifiers
                .compactMap({ UTType($0) })
                .first(where: { $0.conforms(to: type) })
            {
                return concrete
            }
            return type
        }
        return .data
    }

    private static func loadFileData(from attachment: NSItemProvider, type: UTType) async -> Data? {
        // `loadFileRepresentation` gives a temporary file path for anything the
        // provider has on disk, which is the common case for photos and
        // documents.
        let fromFile: Data? = await withCheckedContinuation { continuation in
            attachment.loadFileRepresentation(forTypeIdentifier: type.identifier) { url, _ in
                guard let url, let data = try? Data(contentsOf: url) else {
                    continuation.resume(returning: nil)
                    return
                }
                continuation.resume(returning: data)
            }
        }
        if let fromFile {
            return fromFile
        }

        // Fall back to `loadItem`: some providers only vend in-memory objects.
        return await withCheckedContinuation { continuation in
            attachment.loadItem(forTypeIdentifier: type.identifier) { item, _ in
                switch item {
                case let data as Data:
                    continuation.resume(returning: data)
                case let url as URL:
                    continuation.resume(returning: try? Data(contentsOf: url))
                case let url as NSURL:
                    continuation.resume(returning: try? Data(contentsOf: url as URL))
                case let image as UIImage:
                    continuation.resume(returning: image.jpegData(compressionQuality: 0.9))
                default:
                    continuation.resume(returning: nil)
                }
            }
        }
    }
}

private extension String {
    var deletingPathExtension: String {
        (self as NSString).deletingPathExtension
    }
}
