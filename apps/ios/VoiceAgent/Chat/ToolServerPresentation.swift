import Foundation

// MARK: - Server-side tool presentation

/// The category the gateway classified a tool call into, carried on the wire so
/// this client describes a call the same way the web console does instead of
/// re-classifying it from its own table.
///
/// The gateway's `toolresult` package owns the vocabulary (family per wire name,
/// a verb per tense, a glyph per family). The app keeps its own copy of the
/// copy so the row can be localized, but which family a call belongs to is the
/// server's answer — one table, not two.
struct ChatToolCategory: Equatable, Decodable {
    /// The family id (`memory`, `search`, `web`, …). Unknown ids degrade to the
    /// generic `.tool` family rather than dropping the event.
    let family: String
    /// What the settled step says, e.g. "Searched memory".
    let verb: String
    /// What the in-flight step says, e.g. "Searching memory".
    let runningVerb: String
    /// The gateway's lucide icon class. The app draws its own SF Symbol per
    /// family, so the value is kept for diagnostics only.
    let icon: String

    /// The app's family for this server family, or nil when the id is one this
    /// build does not know (the caller then falls back to its own table).
    var category: MemoryToolCategory? {
        MemoryToolCategory(rawValue: family)
    }
}

/// The gateway's readable presentation of a settled tool result
/// (`toolresult.Presentation` on the wire). The parsing rules — which shapes are
/// readable, how many records, fields, lines and characters are shown, and what
/// an empty result says — live in the gateway, so the app draws its answer
/// instead of deriving a second one.
///
/// Every count is optional because the gateway omits zero values; `kind` is what
/// says whether there is a body at all.
struct ServerToolView: Equatable, Decodable {
    struct Item: Equatable, Decodable {
        let title: String
        let detail: String?
        let link: String?
    }

    struct Field: Equatable, Decodable {
        let label: String
        let value: String
    }

    struct Batch: Equatable, Decodable {
        let created: Int
        let failed: Int
        let similar: Int
    }

    let kind: String
    let items: [Item]?
    let fields: [Field]?
    let text: String?
    let batch: Batch?
    let moreRecords: Int?
    let moreFields: Int?
    let moreLines: Int?
    let moreCharacters: Int?
    let error: String?
    /// The one line the collapsed step shows, derived server-side so the two
    /// surfaces cannot word the same result differently.
    let outcome: String?
    let raw: String?
    let rawMoreLines: Int?
    let rawMoreCharacters: Int?

    /// The app's presentation model, built from the server's answer. The raw
    /// payload comes from the server too: it is the same bounded text the web
    /// console puts behind its disclosure.
    var presentation: ToolResultPresentation {
        ToolResultPresentation(
            body: body,
            batch: batch.map {
                ToolResultPresentation.BatchTally(
                    created: $0.created,
                    failed: $0.failed,
                    similar: $0.similar
                )
            },
            raw: raw ?? "",
            rawMoreLines: rawMoreLines ?? 0,
            rawMoreCharacters: rawMoreCharacters ?? 0
        )
    }

    private var body: ToolResultPresentation.Body? {
        switch kind {
        case "records":
            let mapped = (items ?? []).enumerated().map { index, item in
                ToolResultPresentation.Item(
                    id: index,
                    title: item.title,
                    detail: item.detail,
                    url: Self.httpURL(item.link)
                )
            }
            return .items(mapped, more: moreRecords ?? 0)
        case "fields":
            let mapped = (fields ?? []).enumerated().map { index, field in
                ToolResultPresentation.Field(id: index, label: field.label, value: field.value)
            }
            return .fields(mapped, more: moreFields ?? 0)
        case "text":
            return .text(
                text ?? "",
                moreLines: moreLines ?? 0,
                moreCharacters: moreCharacters ?? 0
            )
        case "empty":
            // The gateway's copy for an empty list; the app's own string is the
            // fallback for a payload that carried none.
            return .empty
        default:
            return nil
        }
    }

    /// Only http(s) links become links; anything else stays part of the detail,
    /// exactly as `toolresult.Item` decides it.
    private static func httpURL(_ link: String?) -> String? {
        guard let link, let url = URL(string: link), let scheme = url.scheme?.lowercased() else {
            return nil
        }
        return scheme == "http" || scheme == "https" ? link : nil
    }
}

// MARK: - Wire size

/// The response size the expanded step reports, worded as the web console words
/// it ("512 B", "1.2 KB", "3.4 MB").
enum ToolResultSize {
    static func text(bytes: Int?) -> String? {
        guard let bytes, bytes >= 0 else { return nil }
        if bytes < 1024 {
            return "\(bytes) B"
        }
        let kilobytes = Double(bytes) / 1024
        if kilobytes < 1024 {
            return "\(rounded(kilobytes)) KB"
        }
        let megabytes = kilobytes / 1024
        if megabytes < 1024 {
            return "\(rounded(megabytes)) MB"
        }
        return "\(rounded(megabytes / 1024)) GB"
    }

    /// The UTF-8 size of a payload, which is the wire size the web console
    /// measures (not the UTF-16 length Swift strings carry).
    static func byteCount(of text: String?) -> Int? {
        guard let text else { return nil }
        return text.utf8.count
    }

    private static func rounded(_ value: Double) -> String {
        let scaled = (value * 10).rounded() / 10
        return scaled == scaled.rounded()
            ? String(Int(scaled))
            : String(format: "%.1f", scaled)
    }
}
