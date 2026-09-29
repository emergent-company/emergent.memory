import Foundation

/// One knowledge-graph object from the gateway's device object browser
/// (`GET /api/agents/:id/objects`).
///
/// Mirrors the gateway `GraphObject` (gateway/memory_graph.go). Decoding is
/// defensive: unknown fields are ignored and every field the server might omit
/// or return as `null` falls back to a default, so a single odd record never
/// breaks the whole list.
struct GraphObject: Identifiable, Codable, Hashable, Sendable {
    let id: String
    /// Stable identity across versions (may be absent).
    let canonicalID: String?
    /// Branch the object lives on (may be absent).
    let branchID: String?
    /// Object type, e.g. `person`, `note`.
    let type: String
    /// Human-facing key (may be empty).
    let key: String
    /// Lifecycle status, e.g. `active` (may be empty).
    let status: String
    /// Arbitrary object content written by the agent. Decoded as ``JSONValue``
    /// so any JSON shape renders without a fixed schema; nil when absent/null.
    let properties: [String: JSONValue]?
    let labels: [String]?
    /// RFC3339 creation time (may be absent).
    let createdAt: String?

    enum CodingKeys: String, CodingKey {
        case id, type, key, status, properties, labels
        case canonicalID = "canonical_id"
        case branchID = "branch_id"
        case createdAt = "created_at"
    }

    init(
        id: String,
        canonicalID: String? = nil,
        branchID: String? = nil,
        type: String,
        key: String,
        status: String = "",
        properties: [String: JSONValue]? = nil,
        labels: [String]? = nil,
        createdAt: String? = nil
    ) {
        self.id = id
        self.canonicalID = canonicalID
        self.branchID = branchID
        self.type = type
        self.key = key
        self.status = status
        self.properties = properties
        self.labels = labels
        self.createdAt = createdAt
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = (try? container.decodeIfPresent(String.self, forKey: .id)) ?? ""
        canonicalID = (try? container.decodeIfPresent(String.self, forKey: .canonicalID)) ?? nil
        branchID = (try? container.decodeIfPresent(String.self, forKey: .branchID)) ?? nil
        type = (try? container.decodeIfPresent(String.self, forKey: .type)) ?? ""
        key = (try? container.decodeIfPresent(String.self, forKey: .key)) ?? ""
        status = (try? container.decodeIfPresent(String.self, forKey: .status)) ?? ""
        properties = (try? container.decodeIfPresent([String: JSONValue].self, forKey: .properties)) ?? nil
        labels = (try? container.decodeIfPresent([String].self, forKey: .labels)) ?? nil
        createdAt = (try? container.decodeIfPresent(String.self, forKey: .createdAt)) ?? nil
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(id, forKey: .id)
        try container.encodeIfPresent(canonicalID, forKey: .canonicalID)
        try container.encodeIfPresent(branchID, forKey: .branchID)
        try container.encode(type, forKey: .type)
        try container.encode(key, forKey: .key)
        try container.encode(status, forKey: .status)
        try container.encodeIfPresent(properties, forKey: .properties)
        try container.encodeIfPresent(labels, forKey: .labels)
        try container.encodeIfPresent(createdAt, forKey: .createdAt)
    }

    /// List-row title: the object key, falling back to the id when empty.
    var displayTitle: String {
        key.isEmpty ? id : key
    }

    /// Client-side search match over the fields the UI shows: key, type,
    /// labels, and the string leaves of `properties`. Empty text matches all.
    func matches(_ text: String) -> Bool {
        let needle = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !needle.isEmpty else { return true }
        if key.localizedCaseInsensitiveContains(needle) {
            return true
        }
        if type.localizedCaseInsensitiveContains(needle) {
            return true
        }
        if labels?.contains(where: { $0.localizedCaseInsensitiveContains(needle) }) == true {
            return true
        }
        if let properties {
            if properties.values.contains(where: { $0.contains(needle) }) {
                return true
            }
        }
        return false
    }
}

/// A flexible JSON value so arbitrary object `properties` decode and render
/// safely. ``display`` is plain text: SwiftUI renders it verbatim and never
/// interprets markup, which is what keeps untrusted agent content inert.
enum JSONValue: Codable, Hashable, Sendable {
    case string(String)
    case number(Double)
    case bool(Bool)
    case object([String: JSONValue])
    case array([JSONValue])
    case null

    /// Plain-text rendering: strings raw, everything else compact JSON.
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
