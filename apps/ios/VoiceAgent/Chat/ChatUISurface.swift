import Foundation

// MARK: - A2UI wire model (v0.9.1)

/// A dynamic A2UI component prop. Component props are open-ended (each card
/// keeps extra keys beyond its catalog-required ones), so every value decodes
/// into this JSON-shaped enum and is coerced to display text on demand.
enum ChatUIPropValue: Equatable {
    case string(String)
    case number(Double)
    case bool(Bool)
    case array([ChatUIPropValue])
    case object([String: ChatUIPropValue])
    case null

    /// The value when it is a plain string (used for ids/actions/labels).
    var stringValue: String? {
        if case let .string(value) = self { return value }
        return nil
    }

    /// Coerces any value to display text (objects render as pretty JSON),
    /// mirroring the web renderer's `a2uiText`.
    var displayText: String {
        switch self {
        case let .string(value): return value
        case let .number(value):
            if value.rounded() == value, abs(value) < 1e15 { return String(Int(value)) }
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
    func value(_ key: String) -> ChatUIPropValue? { props[key] }

    /// A prop's display text, or `nil` when absent.
    func text(_ key: String) -> String? { props[key]?.displayText }

    /// Every prop except identity, in stable key order (summary fallback).
    var extraProps: [(String, String)] {
        props
            .filter { $0.key != "id" && $0.key != "component" }
            .sorted { $0.key < $1.key }
            .map { ($0.key, $0.value.displayText) }
    }

    /// Rows from an array-of-objects prop (entity properties/relationships).
    func rows(_ key: String) -> [(String, String)] { props[key]?.rows ?? [] }
}

extension ChatUIComponent: Decodable {
    private struct DynamicKey: CodingKey {
        let stringValue: String
        init?(stringValue: String) { self.stringValue = stringValue }
        var intValue: Int? { nil }
        init?(intValue: Int) { nil }
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

    var hasContent: Bool { !components.isEmpty }
}
