import Foundation

/// One grounded reference behind an assistant reply: a graph object, or a
/// relationship (which points at its source object's page).
///
/// Mirrors memory's `citations.Citation` (`{kind, id, type, label, url, key}`,
/// `apps/server/domain/chat/citations/citations.go`). It arrives on the live
/// `citations` stream frame and on recorded assistant turns (`/api/session`).
///
/// Decoding never throws: a citation rides inside stream frames and session
/// records, and one odd element must not drop the frame (or the record) that
/// carries it. A citation with nothing to name it is skipped when rendered,
/// exactly as the web Sources footer skips it (`sourceRows`).
struct ChatCitation: Codable, Hashable, Sendable {
    /// `object` or `relationship`.
    var kind: String
    var id: String
    /// The object's schema type, or the relationship type.
    var type: String
    var label: String
    /// `/objects/<id>`; a relationship's source object page, optionally with
    /// its `#relationship-<id>` fragment.
    var url: String
    /// The object's human key when the answer referenced it by key.
    var key: String

    init(kind: String = "object", id: String = "", type: String = "", label: String = "", url: String = "", key: String = "") {
        self.kind = kind
        self.id = id
        self.type = type
        self.label = label
        self.url = url
        self.key = key
    }

    private enum CodingKeys: String, CodingKey {
        case kind, id, type, label, url, key
    }

    init(from decoder: Decoder) throws {
        let container = try? decoder.container(keyedBy: CodingKeys.self)
        func string(_ key: CodingKeys) -> String {
            (try? container?.decodeIfPresent(String.self, forKey: key)) ?? ""
        }
        kind = string(.kind)
        id = string(.id)
        type = string(.type)
        label = string(.label)
        url = string(.url)
        key = string(.key)
    }

    /// The name a Sources row shows: the label, else the key, else the id —
    /// the web's `sourceName` order. Empty when the citation names nothing.
    var displayName: String {
        for candidate in [label, key, id] {
            let trimmed = candidate.trimmingCharacters(in: .whitespacesAndNewlines)
            if !trimmed.isEmpty {
                return trimmed
            }
        }
        return ""
    }

    /// The object this citation opens. The cited URL wins when it is an object
    /// link (it is how a relationship points at its source object); otherwise
    /// an object citation opens its own id. A relationship id is never opened
    /// as if it were an object.
    var objectReference: ObjectReference? {
        if !url.isEmpty, let parsed = URL(string: url), let reference = ObjectReference(url: parsed) {
            return reference
        }
        let objectID = id.trimmingCharacters(in: .whitespacesAndNewlines)
        guard kind != "relationship", !objectID.isEmpty, !objectID.contains("/") else { return nil }
        return ObjectReference(ref: objectID)
    }

    /// Drops citations that name nothing and repeats of the same object, in
    /// first-seen order. Used when merged turns carry overlapping lists.
    static func deduplicated(_ citations: [ChatCitation]) -> [ChatCitation] {
        var seen: Set<String> = []
        var out: [ChatCitation] = []
        for citation in citations where !citation.displayName.isEmpty {
            let identity = citation.objectReference?.id ?? "\(citation.kind):\(citation.id):\(citation.displayName)"
            if seen.insert(identity).inserted {
                out.append(citation)
            }
        }
        return out
    }
}

/// Decodes a citation list leniently: a missing, null, or non-array value is
/// an empty list, and each element decodes on its own.
struct LenientCitations: Decodable {
    let items: [ChatCitation]

    init(from decoder: Decoder) throws {
        var items: [ChatCitation] = []
        if var container = try? decoder.unkeyedContainer() {
            while !container.isAtEnd {
                // A null element is skipped (decodeNil advances past it); a
                // keyed decode of null would throw without advancing.
                if (try? container.decodeNil()) == true {
                    continue
                }
                if let citation = try? container.decode(ChatCitation.self) {
                    items.append(citation)
                } else if (try? container.decode(DiscardedValue.self)) == nil {
                    // Unreadable and unskippable: stop rather than loop forever.
                    break
                }
            }
        }
        self.items = items
    }

    /// Consumes one element of any shape so the unkeyed cursor advances.
    private struct DiscardedValue: Decodable {
        init(from _: Decoder) throws {}
    }
}
