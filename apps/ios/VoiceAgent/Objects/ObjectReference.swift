import Foundation

/// A reference to one graph object from chat content: an agent's inline link
/// (`[Name](/objects/<id>)`, or `/objects/<src>#relationship-<rel>` for a cited
/// relationship) or a row of a reply's Sources footer.
///
/// Parsing follows the web chat's `parseRef` (`object-preview.js`), so a link
/// opens the object card on iOS exactly when it opens the preview drawer on the
/// web. The agent is told to cite in this form (`citationInstruction`,
/// `apps/server/domain/agents/executor.go`).
struct ObjectReference: Identifiable, Hashable, Sendable {
    /// The object id (or, from older answers, its key) as written after
    /// `/objects/`.
    let ref: String
    /// The relationship the link pointed at (`#relationship-<id>`), which the
    /// card turns into a "Referenced via …" line.
    let relationshipID: String?

    var id: String {
        "\(ref)#\(relationshipID ?? "")"
    }

    init(ref: String, relationshipID: String? = nil) {
        self.ref = ref
        self.relationshipID = relationshipID
    }

    /// Parses a link destination. Accepts a relative `/objects/<ref>` path (the
    /// form the agent writes) or any URL whose path contains `/objects/<ref>`
    /// (a scoped or absolute gateway link). Returns nil for anything else, and
    /// for a ref containing `/` — a human key the single-segment card route
    /// cannot address — so those links keep their default behaviour, as on the
    /// web.
    init?(url: URL) {
        // Reject links that clearly point elsewhere: another scheme (mailto:,
        // tel:) or a different web page. A scheme-less relative link and an
        // http(s) link both pass; the path decides.
        if let scheme = url.scheme?.lowercased(), scheme != "http", scheme != "https" {
            return nil
        }
        guard let components = URLComponents(url: url, resolvingAgainstBaseURL: false) else { return nil }
        self.init(path: components.percentEncodedPath, fragment: components.percentEncodedFragment)
    }

    /// Parses the `{path}` and `{fragment}` halves of a reference.
    init?(path: String, fragment: String?) {
        guard let range = path.range(of: "/objects/") else { return nil }
        let raw = String(path[range.upperBound...])
        let ref = raw.removingPercentEncoding ?? raw
        guard !ref.isEmpty, !ref.contains("/") else { return nil }
        var relationship: String?
        if let fragment, fragment.hasPrefix("relationship-") {
            let rel = String(fragment.dropFirst("relationship-".count))
            relationship = rel.isEmpty ? nil : (rel.removingPercentEncoding ?? rel)
        }
        self.init(ref: ref, relationshipID: relationship)
    }

    /// The link form of this reference, so a Sources row can route through the
    /// same `openURL` handler an inline link does.
    var url: URL? {
        var components = URLComponents()
        components.path = "/objects/\(ref)"
        if let relationshipID {
            components.fragment = "relationship-\(relationshipID)"
        }
        return components.url
    }
}
