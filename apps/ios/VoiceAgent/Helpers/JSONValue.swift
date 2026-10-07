import Foundation

/// One flexible JSON value, shared by every surface that decodes open-ended
/// JSON: object `properties`, tool-call arguments, and (in a later pass) A2UI
/// props. Decoding is total — any JSON shape becomes one of these cases — so
/// arbitrary/unknown payloads render safely without a fixed schema.
///
/// `Codable` for round-tripping, `Hashable` so containers like
/// `GraphObject` (which is `Hashable`) keep their synthesized conformance,
/// and `Sendable` for Swift 6 strict concurrency.
enum JSONValue: Codable, Hashable, Sendable {
    case string(String)
    case number(Double)
    case bool(Bool)
    case object([String: JSONValue])
    case array([JSONValue])
    case null

    /// Plain-text rendering: strings raw, everything else compact JSON.
    /// SwiftUI renders this verbatim and never interprets markup, which is
    /// what keeps untrusted agent content inert.
    var display: String {
        switch self {
        case let .string(text):
            text
        case let .number(value):
            Self.numberString(value)
        case let .bool(value):
            value ? "true" : "false"
        case .null:
            "null"
        case .object, .array:
            (try? JSONEncoder().encode(self)).flatMap { String(data: $0, encoding: .utf8) } ?? ""
        }
    }

    /// True when any nested string leaf contains `needle` (case-insensitive).
    func contains(_ needle: String) -> Bool {
        switch self {
        case let .string(text):
            text.localizedCaseInsensitiveContains(needle)
        case let .object(values):
            values.values.contains { $0.contains(needle) }
        case let .array(values):
            values.contains { $0.contains(needle) }
        case .number, .bool, .null:
            false
        }
    }

    /// Renders a number as an integer when it is integral, else as-is. Keeps
    /// `42` from showing up as `42.0`.
    private static func numberString(_ value: Double) -> String {
        if value.rounded() == value, value.magnitude < 1e15 {
            return String(Int(value))
        }
        return String(value)
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.singleValueContainer()
        if container.decodeNil() {
            self = .null
            return
        }
        if let value = try? container.decode(String.self) {
            self = .string(value)
            return
        }
        if let value = try? container.decode(Bool.self) {
            self = .bool(value)
            return
        }
        if let value = try? container.decode(Double.self) {
            self = .number(value)
            return
        }
        if let value = try? container.decode([JSONValue].self) {
            self = .array(value)
            return
        }
        if let value = try? container.decode([String: JSONValue].self) {
            self = .object(value)
            return
        }
        throw DecodingError.typeMismatch(
            JSONValue.self,
            DecodingError.Context(
                codingPath: decoder.codingPath,
                debugDescription: "Unsupported JSON value"
            )
        )
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.singleValueContainer()
        switch self {
        case let .string(value): try container.encode(value)
        case let .number(value): try container.encode(value)
        case let .bool(value): try container.encode(value)
        case let .object(value): try container.encode(value)
        case let .array(value): try container.encode(value)
        case .null: try container.encodeNil()
        }
    }
}

// MARK: - FlexJSON-compatible salvage

extension JSONValue {
    /// Compact JSON text for display; plain strings pass through raw. Preserves
    /// the `%g` number formatting the former `FlexJSON.jsonString` used.
    var jsonString: String? {
        switch self {
        case let .string(text):
            text
        case let .number(number):
            String(format: "%g", locale: Locale(identifier: "en_US_POSIX"), number)
        case let .bool(value):
            value ? "true" : "false"
        case .null:
            "null"
        case .object, .array:
            (try? JSONEncoder().encode(self)).flatMap { String(data: $0, encoding: .utf8) }
        }
    }
}

// MARK: - A2UI display helpers

extension JSONValue {
    /// Coerces any value to display text (objects render as pretty JSON),
    /// mirroring the web renderer's `a2uiText`.
    var displayText: String {
        switch self {
        case let .string(value): return value
        case let .number(value):
            if value.rounded() == value, abs(value) < 1e15 {
                return String(Int(value))
            }
            return String(value)
        case let .bool(value): return value ? "true" : "false"
        case .null: return ""
        case let .array(items): return items.map(\.displayText).joined(separator: ", ")
        case let .object(object): return Self.prettyJSON(object)
        }
    }

    /// The `[[label, value], ...]` pairs of an array of `{label, value}` rows.
    var rows: [(String, String)] {
        guard case let .array(items) = self else { return [] }
        return items.compactMap { item in
            guard case let .object(object) = item else { return nil }
            let label = (object["label"] ?? object["key"] ?? object["title"])?.displayText ?? ""
            let value = (object["value"] ?? object["text"] ?? object["body"])?.displayText ?? ""
            return (label, value)
        }
    }

    private static func prettyJSON(_ object: [String: JSONValue]) -> String {
        guard
            let data = try? JSONEncoder().encode(object),
            let any = try? JSONSerialization.jsonObject(with: data),
            let pretty = try? JSONSerialization.data(withJSONObject: any, options: [.prettyPrinted, .sortedKeys]),
            let text = String(data: pretty, encoding: .utf8)
        else { return "" }
        return text
    }
}
