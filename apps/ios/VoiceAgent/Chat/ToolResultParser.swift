import Foundation

/// Detects the readable shape of a tool payload and builds its body.
///
/// Two shapes are handled explicitly because the server produces them for most
/// tools: the MCP envelope (`{"ok": true, "data": …}`, whose `data` is the
/// payload) and a list element that wraps its record (`{"object": {…}, "score":
/// …}` from a graph search). Everything else is found by key, so a tool added
/// tomorrow that returns `{"data": […records…]}` reads as a list on day one.
enum ToolResultParser {
    /// Keys that commonly wrap a list, in the order they are tried.
    private static let listKeys = [
        "documents", "data", "results", "entities", "items", "objects", "rows", "keys",
    ]
    /// Keys whose value is the payload itself (the MCP envelope and the agent's
    /// `result` wrapper for an array or scalar).
    private static let unwrapKeys = ["data", "result"]
    /// Keys that commonly wrap a record inside a list element.
    private static let wrapperKeys = ["object", "entity", "document", "item", "record", "node", "data", "result"]
    /// Keys that commonly hold a record's readable name.
    private static let titleKeys = ["title", "name", "filename", "display_name", "label"]
    /// Keys that name a record when nothing more human is present.
    private static let fallbackTitleKeys = ["key", "summary", "id"]
    /// Keys holding a record's property bag. Graph objects carry the readable
    /// name and description there, above the slug in `key`.
    private static let bagKeys = ["fields", "properties", "attributes", "metadata"]
    /// Keys that commonly hold a record's one-line detail.
    private static let detailKeys = ["description", "snippet", "mimeType", "mime_type", "type", "object_type", "canonical_id"]
    /// Keys that commonly hold a body of text.
    private static let textKeys = ["content", "text", "body", "markdown", "output", "stdout", "result", "message", "data"]
    /// Below this a "long text" is just another field.
    private static let textMinimumLength = 120
    /// How much of a single field value a field list shows.
    private static let fieldValueWidth = 240

    static func body(from json: Any) -> ToolResultPresentation.Body? {
        // A failed envelope carries the message a person needs, and it stays the
        // answer even when the tool also returned a partial payload beside it.
        if let envelope = json as? [String: Any], let message = errorMessage(in: envelope) {
            return textBody(message)
        }
        let payload = unwrapped(json)
        if let text = payload as? String {
            return textBody(text)
        }
        if let array = payload as? [Any] {
            return array.isEmpty ? .empty : items(from: array)
        }
        guard let record = payload as? [String: Any] else { return nil }
        if let message = errorMessage(in: record) {
            return textBody(message)
        }
        // A single record may wrap itself too: `{"entity": {…}, "message": …}`
        // from a create, `{"document": {…}}` from a read.
        let view = flattened(record)
        if let array = list(in: view), let items = items(from: array) {
            return items
        }
        if let text = longText(in: view) {
            return textBody(text)
        }
        // Only after a readable text has had its chance: an empty list beside a
        // real body of text is not the answer, but an empty list on its own
        // means "nothing matched" and reads better than the envelope (#2277).
        if hasEmptyList(in: view) {
            return .empty
        }
        return fields(from: view)
    }

    /// Peels the envelope off a payload: `{"ok": true, "data": …}` and the
    /// agent's `{"ok": true, "result": …}` wrapper both carry the real thing one
    /// level down. Bounded, so a payload cannot make this loop.
    private static func unwrapped(_ json: Any) -> Any {
        var current = json
        for _ in 0 ..< 3 {
            guard let record = current as? [String: Any] else { return current }
            guard let inner = unwrapKeys.lazy.compactMap({ record[$0] }).first(where: isPayload) else {
                return current
            }
            current = inner
        }
        return current
    }

    private static func isPayload(_ value: Any) -> Bool {
        value is [String: Any] || value is [Any] || value is String
    }

    private static func list(in record: [String: Any]) -> [Any]? {
        for key in listKeys {
            if let array = record[key] as? [Any], !array.isEmpty {
                return array
            }
        }
        return nil
    }

    /// Whether the record carries an empty list under one of the known list
    /// keys. Non-empty lists are handled by `list(in:)`; this only answers the
    /// "nothing matched" case.
    private static func hasEmptyList(in record: [String: Any]) -> Bool {
        for key in listKeys {
            if let array = record[key] as? [Any], array.isEmpty {
                return true
            }
        }
        return false
    }

    private static func items(from array: [Any]) -> ToolResultPresentation.Body? {
        guard !array.isEmpty else { return nil }
        // A list of plain strings is a list of keys or ids: readable as is.
        if array.allSatisfy({ $0 is String }) {
            let titles = array.compactMap { $0 as? String }.filter { !$0.trimmingCharacters(in: .whitespaces).isEmpty }
            guard !titles.isEmpty else { return nil }
            let shown = titles.prefix(ToolResultPresentation.itemLimit).enumerated().map { index, title in
                ToolResultPresentation.Item(id: index, title: title, detail: nil, url: nil)
            }
            return .items(Array(shown), more: max(0, titles.count - ToolResultPresentation.itemLimit))
        }

        var items: [ToolResultPresentation.Item] = []
        for element in array {
            // An element with no readable name is left out rather than
            // abandoning the whole list; the remainder count still covers it.
            guard let record = element as? [String: Any] else { continue }
            let view = flattened(record)
            guard let title = title(of: view) else { continue }
            items.append(
                ToolResultPresentation.Item(
                    id: items.count,
                    title: title,
                    detail: detail(of: view, title: title),
                    url: link(of: view)
                )
            )
        }
        guard !items.isEmpty else { return nil }
        let shown = Array(items.prefix(ToolResultPresentation.itemLimit))
        return .items(shown, more: max(0, array.count - shown.count))
    }

    /// Lifts the record out of a list element that wraps it — a search hit is
    /// `{"object": {…}, "score": …}`, a create result is `{"entity": {…}}`.
    private static func flattened(_ record: [String: Any]) -> [String: Any] {
        for key in wrapperKeys {
            guard let inner = record[key] as? [String: Any] else { continue }
            var merged = inner
            // The wrapper key itself is not a field of the record; copying it
            // back would leave a nested object in a record that reads as fields.
            for (name, value) in record where name != key && merged[name] == nil {
                merged[name] = value
            }
            return merged
        }
        return record
    }

    private static func bags(of record: [String: Any]) -> [[String: Any]] {
        bagKeys.compactMap { record[$0] as? [String: Any] }
    }

    private static func title(of record: [String: Any]) -> String? {
        for key in titleKeys {
            if let value = shortString(record[key]) {
                return value
            }
        }
        for bag in bags(of: record) {
            for key in titleKeys {
                if let value = shortString(bag[key]) {
                    return value
                }
            }
        }
        for key in fallbackTitleKeys {
            if let value = shortString(record[key]) {
                return value
            }
        }
        return nil
    }

    private static func detail(of record: [String: Any], title: String) -> String? {
        // What the record says about itself beats what type it is: a search hit
        // reads better as its description, with the type left to the raw JSON.
        for bag in bags(of: record) {
            for key in ["description", "summary"] {
                if let value = shortString(bag[key]), value != title {
                    return value
                }
            }
        }
        for key in detailKeys {
            if let value = shortString(record[key]), value != title {
                return value
            }
        }
        return nil
    }

    private static func link(of record: [String: Any]) -> String? {
        for key in ["url", "sourceUrl", "source_url"] {
            if let url = httpURL(record[key]) {
                return url
            }
        }
        return nil
    }

    private static func fields(from record: [String: Any]) -> ToolResultPresentation.Body? {
        guard !record.isEmpty else { return nil }
        // A record of nested structures is not a field list; it is raw JSON.
        var scalars: [(String, String)] = []
        for key in record.keys.sorted() {
            guard let value = scalarString(record[key]) else { return nil }
            scalars.append((key, value))
        }
        let fields = scalars.enumerated().map { index, entry in
            ToolResultPresentation.Field(id: index, label: entry.0, value: entry.1)
        }
        let limit = ToolResultPresentation.fieldLimit
        return .fields(Array(fields.prefix(limit)), more: max(0, fields.count - limit))
    }

    private static func errorMessage(in record: [String: Any]) -> String? {
        if let message = shortString(record["error"], limit: 800) {
            return message
        }
        if let succeeded = record["ok"] as? Bool, !succeeded, let message = shortString(record["message"], limit: 800) {
            return message
        }
        return nil
    }

    private static func longText(in record: [String: Any]) -> String? {
        var longest: String?
        for key in textKeys {
            guard let value = record[key] as? String, value.count >= textMinimumLength else { continue }
            if value.count > (longest?.count ?? 0) {
                longest = value
            }
        }
        return longest
    }

    private static func textBody(_ text: String) -> ToolResultPresentation.Body {
        let bounded = BoundedText.lines(
            text,
            limit: ToolResultPresentation.textLineLimit,
            width: ToolResultPresentation.lineWidth
        )
        return .text(bounded.text, moreLines: bounded.moreLines, moreCharacters: bounded.moreCharacters)
    }

    private static func shortString(_ value: Any?, limit: Int = 80) -> String? {
        guard let string = value as? String else { return nil }
        let trimmed = string.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return nil }
        return String(trimmed.prefix(limit))
    }

    private static func scalarString(_ value: Any?) -> String? {
        guard let value else { return nil }
        switch value {
        case let string as String:
            return string.count > fieldValueWidth
                ? String(string.prefix(fieldValueWidth)) + "…"
                : string
        case let number as NSNumber:
            // JSON `true` decodes as a number; read it as a boolean, not as 1.
            return CFGetTypeID(number) == CFBooleanGetTypeID() ? (number.boolValue ? "true" : "false") : number.stringValue
        case is NSNull:
            // A null is a value the record has, not a reason to show raw JSON.
            return "—"
        default:
            return nil
        }
    }

    private static func httpURL(_ value: Any?) -> String? {
        guard
            let string = value as? String,
            let url = URL(string: string),
            let scheme = url.scheme?.lowercased(),
            scheme == "http" || scheme == "https",
            url.host != nil
        else { return nil }
        return string
    }
}
