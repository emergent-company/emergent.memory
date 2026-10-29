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
    }

    /// Nil when the payload has no shape worth reading; the row then shows the
    /// raw text alone.
    let body: Body?
    /// The result, pretty-printed and bounded: what the raw disclosure reveals.
    let raw: String
    /// Lines past the raw cap, so the disclosure never pretends to be complete.
    let rawMoreLines: Int
    /// Characters cut from the lines that were kept, for the same reason.
    let rawMoreCharacters: Int

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
