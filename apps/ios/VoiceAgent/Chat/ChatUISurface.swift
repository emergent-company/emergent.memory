import Foundation

// MARK: - A2UI wire model (v0.9.1)

/// A dynamic A2UI component prop. Component props are open-ended (each card
/// keeps extra keys beyond its catalog-required ones), so every value decodes
/// into this JSON-shaped enum and is coerced to display text on demand.
enum ChatUIPropValue: Equatable, Sendable {
    case string(String)
    case number(Double)
    case bool(Bool)
    case array([ChatUIPropValue])
    case object([String: ChatUIPropValue])
    case null

    /// The value when it is a plain string (used for ids/actions/labels).
    var stringValue: String? {
        if case let .string(value) = self {
            return value
        }
        return nil
    }

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

    /// The text the web console measures a result's byte size by: a string is
    /// its own text, every other value is `JSON.stringify(value, null, 2)`.
    ///
    /// Foundation's pretty printer is close but not the same text — it writes
    /// `"key" : value` and prints doubles with 17 digits — so the shape is
    /// written here exactly as ECMAScript writes it. `displayText` is a
    /// rendering (an array reads as a sentence), so it must not be used for the
    /// size.
    var wireText: String {
        if case let .string(value) = self {
            return value
        }
        return Self.jsJSON(self, indent: 0)
    }

    private static func jsJSON(_ value: ChatUIPropValue, indent: Int) -> String {
        switch value {
        case let .string(text):
            return jsString(text)
        case let .number(number):
            return jsNumber(number)
        case let .bool(flag):
            return flag ? "true" : "false"
        case .null:
            return "null"
        case let .array(items):
            guard !items.isEmpty else { return "[]" }
            let pad = String(repeating: " ", count: indent + 2)
            let rows = items.map { pad + jsJSON($0, indent: indent + 2) }
            return "[\n" + rows.joined(separator: ",\n") + "\n" + jsClosing(indent: indent, bracket: "]")
        case let .object(object):
            guard !object.isEmpty else { return "{}" }
            let pad = String(repeating: " ", count: indent + 2)
            let rows = object.keys.sorted().map { key in
                pad + jsString(key) + ": " + jsJSON(object[key] ?? .null, indent: indent + 2)
            }
            return "{\n" + rows.joined(separator: ",\n") + "\n" + jsClosing(indent: indent, bracket: "}")
        }
    }

    private static func jsClosing(indent: Int, bracket: String) -> String {
        String(repeating: " ", count: indent) + bracket
    }

    /// A JSON string as ECMAScript writes it: only the mandatory escapes, a
    /// slash stays a slash, and control characters become `\u00xx`.
    private static func jsString(_ text: String) -> String {
        var out = "\""
        for scalar in text.unicodeScalars {
            switch scalar.value {
            case 0x22: out += "\\\""
            case 0x5C: out += "\\\\"
            case 0x08: out += "\\b"
            case 0x0C: out += "\\f"
            case 0x0A: out += "\\n"
            case 0x0D: out += "\\r"
            case 0x09: out += "\\t"
            case 0x00 ... 0x1F: out += String(format: "\\u%04x", scalar.value)
            default: out.unicodeScalars.append(scalar)
            }
        }
        return out + "\""
    }

    /// A number as ECMAScript writes it: the shortest round-trip digits, plain
    /// notation from 1e-6 up to (not including) 1e21, and exponent notation
    /// without zero padding outside that range.
    private static func jsNumber(_ value: Double) -> String {
        guard value.isFinite else { return "null" }
        guard value != 0 else { return "0" }
        let magnitude = abs(value)
        guard magnitude >= 1e-6, magnitude < 1e21 else {
            return exponentNumber(value)
        }
        var text = String(value)
        var exponent = 0
        if let index = text.firstIndex(where: { $0 == "e" || $0 == "E" }) {
            exponent = Int(text[text.index(after: index)...]) ?? 0
            text = String(text[..<index])
        }
        var sign = ""
        if text.hasPrefix("-") {
            sign = "-"
            text.removeFirst()
        }
        var digits = text.replacingOccurrences(of: ".", with: "")
        let pointIndex = text.firstIndex(of: ".").map { text.distance(from: text.startIndex, to: $0) } ?? text.count
        // Value as `0.<digits> × 10^scale`, the form plain notation expands.
        let scale = pointIndex + exponent
        while digits.count > 1, digits.hasSuffix("0") {
            digits.removeLast()
        }
        if scale <= 0 {
            return sign + "0." + String(repeating: "0", count: -scale) + digits
        }
        if scale >= digits.count {
            return sign + digits + String(repeating: "0", count: scale - digits.count)
        }
        let split = digits.index(digits.startIndex, offsetBy: scale)
        return sign + String(digits[..<split]) + "." + String(digits[split...])
    }

    /// Exponent notation: ECMAScript keeps the sign but drops a leading zero
    /// (`1e+21`, `1e-7`).
    private static func exponentNumber(_ value: Double) -> String {
        let text = String(value)
        guard let index = text.firstIndex(where: { $0 == "e" || $0 == "E" }) else {
            return text
        }
        var exponent = String(text[text.index(after: index)...])
        var sign = ""
        if exponent.hasPrefix("-") {
            sign = "-"
            exponent.removeFirst()
        } else if exponent.hasPrefix("+") {
            sign = "+"
            exponent.removeFirst()
        }
        while exponent.count > 1, exponent.hasPrefix("0") {
            exponent.removeFirst()
        }
        return String(text[..<index]) + "e" + sign + exponent
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

    private static func prettyJSON(_ object: [String: ChatUIPropValue]) -> String {
        guard
            let data = try? JSONEncoder().encode(object),
            let any = try? JSONSerialization.jsonObject(with: data),
            let pretty = try? JSONSerialization.data(withJSONObject: any, options: [.prettyPrinted, .sortedKeys]),
            let text = String(data: pretty, encoding: .utf8)
        else { return "" }
        return text
    }
}

extension ChatUIPropValue: Decodable {
    init(from decoder: Decoder) throws {
        let container = try decoder.singleValueContainer()
        if container.decodeNil() {
            self = .null
        } else if let value = try? container.decode(Bool.self) {
            self = .bool(value)
        } else if let value = try? container.decode(Double.self) {
            self = .number(value)
        } else if let value = try? container.decode(String.self) {
            self = .string(value)
        } else if let value = try? container.decode([ChatUIPropValue].self) {
            self = .array(value)
        } else if let value = try? container.decode([String: ChatUIPropValue].self) {
            self = .object(value)
        } else {
            throw DecodingError.dataCorruptedError(in: container, debugDescription: "Unsupported A2UI prop value")
        }
    }
}

extension ChatUIPropValue: Encodable {
    func encode(to encoder: Encoder) throws {
        var container = encoder.singleValueContainer()
        switch self {
        case let .string(value): try container.encode(value)
        case let .number(value): try container.encode(value)
        case let .bool(value): try container.encode(value)
        case let .array(value): try container.encode(value)
        case let .object(value): try container.encode(value)
        case .null: try container.encodeNil()
        }
    }
}

/// One A2UI card instance: `id` + catalog `component` are identity, every other
/// key is preserved as a flat prop.
struct ChatUIComponent: Identifiable, Equatable {
    let id: String
    let component: String
    let props: [String: ChatUIPropValue]

    /// A prop's raw value, or `nil` when absent.
    func value(_ key: String) -> ChatUIPropValue? {
        props[key]
    }

    /// A prop's display text, or `nil` when absent.
    func text(_ key: String) -> String? {
        props[key]?.displayText
    }

    /// Every prop except identity, in stable key order (summary fallback).
    var extraProps: [(String, String)] {
        props
            .filter { $0.key != "id" && $0.key != "component" }
            .sorted { $0.key < $1.key }
            .map { ($0.key, $0.value.displayText) }
    }

    /// Rows from an array-of-objects prop (entity properties/relationships).
    func rows(_ key: String) -> [(String, String)] {
        props[key]?.rows ?? []
    }
}

extension ChatUIComponent: Decodable {
    private struct DynamicKey: CodingKey {
        let stringValue: String
        init?(stringValue: String) {
            self.stringValue = stringValue
        }

        var intValue: Int? {
            nil
        }

        init?(intValue _: Int) {
            nil
        }
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: DynamicKey.self)
        var id = ""
        var component = ""
        var props: [String: ChatUIPropValue] = [:]
        for key in container.allKeys {
            switch key.stringValue {
            case "id":
                id = (try? container.decode(String.self, forKey: key)) ?? ""
            case "component":
                component = (try? container.decode(String.self, forKey: key)) ?? ""
            default:
                if let value = try? container.decode(ChatUIPropValue.self, forKey: key) {
                    props[key.stringValue] = value
                }
            }
        }
        self.id = id
        self.component = component
        self.props = props
    }
}

/// The four A2UI envelopes. A message carries at most one; unknown members are
/// ignored so newer envelope kinds degrade safely.
struct ChatUIMessage: Equatable, Decodable {
    let createSurface: ChatUICreateSurface?
    let updateComponents: ChatUIUpdateComponents?
    let updateDataModel: ChatUIUpdateDataModel?
    let deleteSurface: ChatUIDeleteSurface?
}

struct ChatUICreateSurface: Equatable, Decodable {
    let surfaceId: String
    let catalogId: String?
}

struct ChatUIUpdateComponents: Equatable, Decodable {
    let surfaceId: String
    let components: [ChatUIComponent]
}

struct ChatUIUpdateDataModel: Equatable, Decodable {
    let surfaceId: String
    let path: String?
    let value: ChatUIPropValue?
}

struct ChatUIDeleteSurface: Equatable, Decodable {
    let surfaceId: String
}

/// A `ui` event: one surface update carrying catalog-validated A2UI messages.
struct ChatUISurfaceEvent: Equatable, Decodable {
    let surfaceId: String
    let messages: [ChatUIMessage]
}

// MARK: - Client → server surface action

/// A user action on a surface component, sent back to the worker. Mirrors the
/// web renderer's `{ componentId, response }` action payload.
struct ChatUIAction: Equatable, Encodable {
    let componentId: String
    let response: ChatUIPropValue

    init(componentId: String, response: ChatUIPropValue) {
        self.componentId = componentId
        self.response = response
    }

    init(componentId: String, response: String) {
        self.init(componentId: componentId, response: .string(response))
    }
}

// MARK: - Accumulated surface state (store)

/// A live A2UI surface: components merged from `updateComponents` in arrival
/// order, plus the actions the user already submitted (rendered answered).
struct ChatUISurface: Identifiable, Equatable {
    let id: String
    var catalogId: String?
    var components: [ChatUIComponent]
    var submittedActions: [String: ChatUIPropValue]

    init(id: String, catalogId: String?, components: [ChatUIComponent] = [], submittedActions: [String: ChatUIPropValue] = [:]) {
        self.id = id
        self.catalogId = catalogId
        self.components = components
        self.submittedActions = submittedActions
    }

    var hasContent: Bool {
        !components.isEmpty
    }
}
