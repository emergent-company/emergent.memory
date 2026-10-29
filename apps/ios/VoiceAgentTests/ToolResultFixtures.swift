import Foundation

// MARK: - Fixtures

/// Payloads shaped like the ones the iOS app actually receives: the MCP
/// envelope (`{"ok": …, "data": …}`) around the server's slim DTOs, plus the
/// plain `wrapResult` payloads for the document and web-search tools. A fixture
/// that no server code produces would let the parser pass against an invented
/// contract, which is exactly how a missing envelope goes unnoticed.
enum Payload {
    private static func dict(_ pairs: (String, Any)...) -> [String: Any] {
        Dictionary(uniqueKeysWithValues: pairs)
    }

    private static func json(_ object: Any) -> String {
        guard let data = try? JSONSerialization.data(withJSONObject: object, options: [.sortedKeys]) else {
            return "{}"
        }
        return String(bytes: data, encoding: .utf8) ?? "{}"
    }

    /// A slim graph object as `slimEntity` emits it.
    private static func entity(
        id: String,
        type: String,
        key: String? = nil,
        name: String? = nil,
        description: String? = nil
    ) -> [String: Any] {
        var properties: [String: Any] = [:]
        if let description {
            properties["description"] = description
        }
        var object: [String: Any] = ["id": id, "type": type]
        if let key {
            object["key"] = key
        }
        if let name {
            object["name"] = name
        }
        if !properties.isEmpty {
            object["properties"] = properties
        }
        return object
    }

    /// `search-hybrid` / `search-semantic`: envelope around `{data: [{object,
    /// score}], total, has_more}`.
    static let search = json(dict(
        ("ok", true),
        ("data", dict(
            ("data", [
                dict(
                    ("object", entity(id: "e1", type: "person", key: "alice", name: "Alice", description: "Engineer")),
                    ("score", 0.91)
                ),
                dict(
                    ("object", entity(id: "e2", type: "topic", key: "retrieval", description: "How answers are found")),
                    ("score", 0.72)
                ),
            ]),
            ("total", 2),
            ("has_more", false)
        ))
    ))

    /// `entity-query`: envelope around the query result with a pagination object.
    static let entities = json(dict(
        ("ok", true),
        ("data", dict(
            ("projectId", "p1"),
            ("entities", [
                dict(
                    ("id", "e1"),
                    ("key", "alice"),
                    ("name", "Alice"),
                    ("type", "person"),
                    ("version", 1),
                    ("properties", ["description": "Engineer"])
                ),
            ]),
            ("pagination", dict(("total", 1), ("limit", 10), ("offset", 0), ("has_more", false)))
        ))
    ))

    /// `entity-update`: envelope around the written record, and nothing else —
    /// production sends only `data.entity`, with no message.
    static let entityUpdate = json(dict(
        ("ok", true),
        ("data", dict(("entity", ["id": "e1", "type": "Note"])))
    ))

    /// `entity-create`: a batch result — `data.results[].entity`, with the
    /// counts in `message` and `meta` (`envelope_registry_test.go`).
    static let entityCreate = json(dict(
        ("ok", true),
        ("data", dict(
            ("results", [
                dict(("ok", true), ("index", 0), ("entity", dict(("id", "e1"), ("type", "Note")))),
            ]),
            ("message", "Batch create completed: 1 created, 0 failed, 0 near-duplicate")
        )),
        ("meta", dict(("created", 1), ("failed", 0), ("total", 1), ("similar_count", 0)))
    ))

    /// `document-get`: the `documents.Document` record, unwrapped, with `ok`
    /// injected by the agent's tool-result conversion.
    static let document = json(dict(
        ("id", "d1"),
        ("filename", "retrieval-notes.md"),
        ("mimeType", "text/markdown"),
        ("content", Array(repeating: "A line of the retrieved document.", count: 20).joined(separator: "\n")),
        ("ok", true)
    ))

    /// `list-documents`: a `documents` list of `Document` records.
    static let documentList = json(dict(
        ("documents", [
            dict(("id", "d1"), ("filename", "notes.md"), ("mimeType", "text/markdown")),
            dict(("id", "d2"), ("filename", "spec.pdf"), ("mimeType", "application/pdf")),
        ]),
        ("total", 2),
        ("next_cursor", NSNull()),
        ("ok", true)
    ))

    /// `web-search-*`: the Brave result record.
    static let web = json(dict(
        ("query", "swiftui"),
        ("resultCount", 1),
        ("results", [
            dict(
                ("title", "SwiftUI"),
                ("url", "https://developer.apple.com/documentation/swiftui"),
                ("description", "Declarative UI"),
                ("age", "2d")
            ),
        ]),
        ("ok", true)
    ))

    /// A record of scalars, including a null: the compact key/value list.
    static let record = json(dict(
        ("ok", true),
        ("status", "ok"),
        ("count", 3),
        ("paused", false),
        ("error", NSNull())
    ))

    /// A failed envelope: no data, just the message a person needs.
    static let failure = json(dict(("ok", false), ("error", "entity not found: e9")))

    /// A payload that is a list of plain strings (an entity key list).
    static let stringList = json(dict(("ok", true), ("data", dict(("keys", ["alpha", "beta"])))))

    /// A search whose first hit carries no readable name: it is counted, not
    /// shown, and the rest of the list survives.
    static let untitled = json(dict(
        ("ok", true),
        ("data", dict(
            ("data", [
                dict(("score", 0.4)),
                dict(("object", entity(id: "e1", type: "note", name: "Only one"))),
            ]),
            ("total", 2)
        ))
    ))

    /// A record of nested structures: no readable shape.
    static let nested = json(dict(
        ("ok", true),
        ("data", dict(
            ("object", dict(("a", dict(("b", 1))))),
            ("extra", [dict(("c", 2))])
        ))
    ))

    /// `count` search hits, for the caps.
    static func hits(_ count: Int) -> String {
        json(dict(
            ("ok", true),
            ("data", dict(
                ("data", (0 ..< count).map { index in
                    dict(("object", entity(id: "e\(index)", type: "note", name: "Result \(index)")), ("score", 0.5))
                }),
                ("total", count),
                ("has_more", false)
            ))
        ))
    }

    /// A document whose body arrives as one endless line (converted plain text).
    static let longLine = json(dict(
        ("id", "d1"),
        ("filename", "dump.txt"),
        ("content", String(repeating: "x", count: 50000)),
        ("ok", true)
    ))

    /// Plain, non-JSON output (a shell body, say).
    static let plainText = "line one\nline two\nline three"
}
