import Foundation

/// The readable part of a settled tool's result: what a person actually reads
/// when they open a step, instead of the JSON envelope the tool happened to
/// return.
///
/// A tool result is a JSON document whose shape is the tool's own business, so
/// this holds no per-tool knowledge — ``ToolResultParser`` looks for the few
/// shapes a person can read (a list of records, one record's fields, a body of
/// text) and otherwise leaves the body empty so the payload is shown untouched.
/// Parsing happens once, when the call settles (`ToolCallInfo.init`), never
/// while the chat scrolls.
struct ToolResultPresentation: Equatable {
    /// One record of a list: a search hit, a graph object, a document, a key.
    struct Item: Equatable, Identifiable {
        let id: Int
        let title: String
        let detail: String?
        /// Only http(s) URLs survive; anything else is just part of the detail.
        let url: String?
    }

    /// One field of a single record.
    struct Field: Equatable, Identifiable {
        let id: Int
        let label: String
        let value: String
    }

    enum Body: Equatable {
        /// A list, capped, with how many elements did not fit.
        case items([Item], more: Int)
        /// One record's scalar fields, capped, with how many did not fit.
        case fields([Field], more: Int)
        /// A body of text, capped by line and by width, counting what was cut.
        case text(String, moreLines: Int, moreCharacters: Int)
        /// A result whose list is empty: "No results" is the whole answer, and
        /// the envelope's `total: 0`/`has_more` are noise (#2277).
        case empty
    }

    /// A batch's counts, which the per-record list cannot say: a successful
    /// record has a name, a failed one is skipped as untitled, and the `meta`
    /// tally would otherwise live only in the raw JSON (#2277).
    struct BatchTally: Equatable {
        let created: Int
        let failed: Int
        /// Inline relationship failures: an entity can be created while a
        /// relationship it asked for failed (#401).
        let relationshipsFailed: Int
        let similar: Int
        /// The gateway's wording of the counts, when it sent one. The two web
        /// surfaces print this line, so the app prints it too; a locally parsed
        /// batch falls back to the localized copy below.
        let serverText: String?

        init(
            created: Int,
            failed: Int,
            similar: Int,
            relationshipsFailed: Int = 0,
            serverText: String? = nil
        ) {
            self.created = created
            self.failed = failed
            self.similar = similar
            self.relationshipsFailed = relationshipsFailed
            self.serverText = serverText
        }

        /// A failed item — or a failed inline relationship — makes the batch read
        /// as a failure, however clean the list of created records looks.
        var isFailure: Bool {
            failed > 0 || relationshipsFailed > 0
        }

        /// The server's line when it sent one, else the counts in the caller's
        /// locale, as the body's one-line summary. A server line that does not
        /// name relationship failures gets that clause appended, so a partial
        /// write never reads as clean.
        func text(locale: Locale = .current) -> String {
            let relationshipClause = relationshipFailureClause(locale: locale)
            if let serverText, !serverText.isEmpty {
                if relationshipsFailed == 0 || Self.mentionsRelationships(serverText) {
                    return serverText
                }
                return serverText + ", " + relationshipClause
            }
            var line = String(localized: "sessions.detail.result.batch.created \(created)", locale: locale)
            line += ", " + String(localized: "sessions.detail.result.batch.failed \(failed)", locale: locale)
            if relationshipsFailed > 0 {
                line += ", " + relationshipClause
            }
            if similar > 0 {
                line += ", " + String(localized: "sessions.detail.result.batch.similar \(similar)", locale: locale)
            }
            return line
        }

        private func relationshipFailureClause(locale: Locale) -> String {
            String(
                localized: "sessions.detail.result.batch.relationshipsFailed \(relationshipsFailed)",
                locale: locale
            )
        }

        private static func mentionsRelationships(_ text: String) -> Bool {
            text.range(of: "relationship", options: .caseInsensitive) != nil
        }
    }

    /// Nil when the payload has no shape worth reading; the row then shows the
    /// raw text alone.
    let body: Body?
    /// The gateway's copy for an empty result ("No results"), when it sent one,
    /// so both surfaces print the same words for the same empty list.
    let emptyText: String?
    /// The counts of a batch create, when the payload is one.
    let batch: BatchTally?
    /// The result, pretty-printed and bounded: what the raw disclosure reveals.
    let raw: String
    /// Lines past the raw cap, so the disclosure never pretends to be complete.
    let rawMoreLines: Int
    /// Characters cut from the lines that were kept, for the same reason.
    let rawMoreCharacters: Int

    init(
        body: Body?,
        emptyText: String? = nil,
        batch: BatchTally?,
        raw: String,
        rawMoreLines: Int,
        rawMoreCharacters: Int
    ) {
        self.body = body
        self.emptyText = emptyText
        self.batch = batch
        self.raw = raw
        self.rawMoreLines = rawMoreLines
        self.rawMoreCharacters = rawMoreCharacters
    }

    /// How many records a list shows before it summarizes the rest.
    static let itemLimit = 5
    /// How many fields a single record shows.
    static let fieldLimit = 8
    /// How many lines of body text a result shows.
    static let textLineLimit = 8
    /// How many lines of raw JSON the disclosure reveals.
    static let rawLineLimit = 40
    /// How wide one displayed line may get. Bounds a pretty-printed body that
    /// still carries a huge string on a single line, and a command's output that
    /// arrives as one endless line.
    static let lineWidth = 400
    /// Above this the payload is not parsed at all: a half-megabyte result is a
    /// wall no presentation fixes.
    static let parseLimit = 200_000

    static func parse(_ result: String?) -> ToolResultPresentation? {
        guard let result, !result.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else {
            return nil
        }
        let json = result.count <= parseLimit
            ? try? JSONSerialization.jsonObject(with: Data(result.utf8), options: [.fragmentsAllowed])
            : nil
        let printable = json.flatMap(prettyPrinted) ?? result
        let raw = BoundedText.lines(printable, limit: rawLineLimit, width: lineWidth)
        return ToolResultPresentation(
            body: json.flatMap(ToolResultParser.body(from:)),
            batch: json.flatMap(ToolResultBatch.tally(from:)),
            raw: raw.text,
            rawMoreLines: raw.moreLines,
            rawMoreCharacters: raw.moreCharacters
        )
    }

    private static func prettyPrinted(_ json: Any) -> String? {
        guard
            JSONSerialization.isValidJSONObject(json),
            let data = try? JSONSerialization.data(withJSONObject: json, options: [.prettyPrinted]),
            let string = String(data: data, encoding: .utf8)
        else { return nil }
        return string
    }
}

/// The first lines of a body of text, each shortened to a width, with the counts
/// of what was left out. A shortened line ends in an ellipsis, so a cut never
/// reads as a complete line.
struct BoundedText: Equatable {
    let text: String
    let moreLines: Int
    let moreCharacters: Int

    static func lines(_ text: String, limit: Int, width: Int) -> BoundedText {
        let lines = text.split(separator: "\n", omittingEmptySubsequences: false)
        var kept: [String] = []
        var dropped = 0
        for line in lines.prefix(limit) {
            if line.count > width {
                dropped += line.count - width
                kept.append(String(line.prefix(width)) + "…")
            } else {
                kept.append(String(line))
            }
        }
        return BoundedText(
            text: kept.joined(separator: "\n"),
            moreLines: max(0, lines.count - limit),
            moreCharacters: dropped
        )
    }
}
