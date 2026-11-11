import Foundation

/// One object's read-only card: the native twin of the web chat's object
/// preview drawer.
///
/// Decoded from the gateway's `GET /api/objects/:id/card`
/// (`apps/web-ui/gateway/object_card.go`), which builds every label, row and
/// section with the same helpers the drawer renders with. The app therefore
/// draws what it is given and never re-derives schema labels, status tones or
/// field order, so the two surfaces cannot drift.
///
/// Decoding is defensive like ``GraphObject``: every field the server might
/// omit falls back to a default, so one odd card never fails to open.
struct ObjectCard: Decodable, Equatable, Sendable {
    let id: String
    /// The identifier the copy action yields: the canonical id, not the version
    /// row that happened to be fetched.
    let copyID: String
    /// Card heading: the key, else type plus a short id.
    let title: String
    let key: String
    let version: Int
    let type: ObjectCardType
    /// The header badge for a type without a workflow.
    let status: ObjectCardStatus?
    /// The work strip of a workflow-declaring type (status + assignee).
    let workflow: ObjectCardWorkflow?
    /// "works at" when the reference pointed at a relationship.
    let referencedVia: String?
    /// "Properties", then "<Type> Properties" when the type has any.
    let sections: [ObjectCardSection]
    let labels: [String]

    private enum CodingKeys: String, CodingKey {
        case id, title, key, version, type, status, workflow, sections, labels
        case copyID = "copy_id"
        case referencedVia = "referenced_via"
    }

    init(
        id: String,
        copyID: String,
        title: String,
        key: String = "",
        version: Int = 0,
        type: ObjectCardType,
        status: ObjectCardStatus? = nil,
        workflow: ObjectCardWorkflow? = nil,
        referencedVia: String? = nil,
        sections: [ObjectCardSection] = [],
        labels: [String] = []
    ) {
        self.id = id
        self.copyID = copyID
        self.title = title
        self.key = key
        self.version = version
        self.type = type
        self.status = status
        self.workflow = workflow
        self.referencedVia = referencedVia
        self.sections = sections
        self.labels = labels
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = (try? container.decodeIfPresent(String.self, forKey: .id)) ?? ""
        let copy = (try? container.decodeIfPresent(String.self, forKey: .copyID)) ?? ""
        copyID = copy.isEmpty ? id : copy
        let heading = (try? container.decodeIfPresent(String.self, forKey: .title)) ?? ""
        title = heading.isEmpty ? id : heading
        key = (try? container.decodeIfPresent(String.self, forKey: .key)) ?? ""
        version = (try? container.decodeIfPresent(Int.self, forKey: .version)) ?? 0
        type = (try? container.decodeIfPresent(ObjectCardType.self, forKey: .type)) ?? ObjectCardType()
        status = (try? container.decodeIfPresent(ObjectCardStatus.self, forKey: .status)) ?? nil
        workflow = (try? container.decodeIfPresent(ObjectCardWorkflow.self, forKey: .workflow)) ?? nil
        let via = (try? container.decodeIfPresent(String.self, forKey: .referencedVia)) ?? ""
        referencedVia = via.isEmpty ? nil : via
        sections = (try? container.decodeIfPresent([ObjectCardSection].self, forKey: .sections)) ?? []
        labels = (try? container.decodeIfPresent([String].self, forKey: .labels)) ?? []
    }

    /// Whether the workflow strip is editable on the phone. The server reports
    /// a work-item key — the two write routes require one — and the type's
    /// allowed statuses; both must be present for the pickers to mean anything.
    var canEditWorkflow: Bool {
        !key.isEmpty && !(workflow?.allowedStatuses.isEmpty ?? true)
    }
}

/// The type chip and icon tile of the card header.
struct ObjectCardType: Decodable, Equatable, Sendable {
    /// Raw schema name ("person").
    var name: String = ""
    /// Display label ("Person"), falling back to the name server-side.
    var label: String = ""
    /// Normalized Lucide icon name the web draws ("user"; "box" by default).
    var icon: String = ""
    /// A declared emoji icon, drawn as text instead of a symbol.
    var glyph: String = ""
    /// The schema-declared CSS color, verbatim ("#3b82f6"); empty when none.
    var color: String = ""

    private enum CodingKeys: String, CodingKey {
        case name, label, icon, glyph, color
    }

    init(name: String = "", label: String = "", icon: String = "", glyph: String = "", color: String = "") {
        self.name = name
        self.label = label
        self.icon = icon
        self.glyph = glyph
        self.color = color
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        name = (try? container.decodeIfPresent(String.self, forKey: .name)) ?? ""
        let display = (try? container.decodeIfPresent(String.self, forKey: .label)) ?? ""
        label = display.isEmpty ? name : display
        icon = (try? container.decodeIfPresent(String.self, forKey: .icon)) ?? ""
        glyph = (try? container.decodeIfPresent(String.self, forKey: .glyph)) ?? ""
        color = (try? container.decodeIfPresent(String.self, forKey: .color)) ?? ""
    }
}

/// A status value with the label and tone the web badge uses.
struct ObjectCardStatus: Decodable, Equatable, Sendable {
    var value: String
    var label: String
    /// success, warning, accent, info, error or neutral.
    var tone: String

    init(value: String, label: String, tone: String = "neutral") {
        self.value = value
        self.label = label
        self.tone = tone
    }

    private enum CodingKeys: String, CodingKey {
        case value, label, tone
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        value = (try? container.decodeIfPresent(String.self, forKey: .value)) ?? ""
        let display = (try? container.decodeIfPresent(String.self, forKey: .label)) ?? ""
        label = display.isEmpty ? value : display
        tone = (try? container.decodeIfPresent(String.self, forKey: .tone)) ?? "neutral"
    }
}

/// The workflow strip: the current status and assignee, plus the option lists a
/// native picker needs. The web drawer's strip is editable when the object is a
/// work item; the card reports enough for the phone to decide the same.
struct ObjectCardWorkflow: Decodable, Equatable, Sendable {
    var status: ObjectCardStatus?
    var assignee: String
    /// The statuses the picker offers, in board order. Empty for a type that
    /// declares no workflow.
    var allowedStatuses: [ObjectCardStatus]
    /// The assignee picker's candidates (runtime agents, then project members).
    /// Empty for a non-work item, whose strip stays read-only.
    var assignees: [ObjectCardAssignee]

    init(
        status: ObjectCardStatus? = nil,
        assignee: String = "",
        allowedStatuses: [ObjectCardStatus] = [],
        assignees: [ObjectCardAssignee] = []
    ) {
        self.status = status
        self.assignee = assignee
        self.allowedStatuses = allowedStatuses
        self.assignees = assignees
    }

    private enum CodingKeys: String, CodingKey {
        case status, assignee, assignees
        case allowedStatuses = "allowed_statuses"
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        status = (try? container.decodeIfPresent(ObjectCardStatus.self, forKey: .status)) ?? nil
        assignee = (try? container.decodeIfPresent(String.self, forKey: .assignee)) ?? ""
        allowedStatuses = (try? container.decodeIfPresent([ObjectCardStatus].self, forKey: .allowedStatuses)) ?? []
        assignees = (try? container.decodeIfPresent([ObjectCardAssignee].self, forKey: .assignees)) ?? []
    }
}

/// One assignee picker option: `value` is the exact string the reassign route
/// expects (an agent name or a member display name).
struct ObjectCardAssignee: Decodable, Equatable, Sendable, Identifiable {
    var value: String
    var label: String

    var id: String {
        value
    }

    init(value: String, label: String = "") {
        self.value = value
        self.label = label.isEmpty ? value : label
    }

    private enum CodingKeys: String, CodingKey {
        case value, label
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        value = (try? container.decodeIfPresent(String.self, forKey: .value)) ?? ""
        let display = (try? container.decodeIfPresent(String.self, forKey: .label)) ?? ""
        label = display.isEmpty ? value : display
    }
}

/// One titled panel of rows.
struct ObjectCardSection: Decodable, Equatable, Sendable, Identifiable {
    var title: String
    var fields: [ObjectCardField]

    var id: String {
        title
    }

    init(title: String, fields: [ObjectCardField]) {
        self.title = title
        self.fields = fields
    }

    private enum CodingKeys: String, CodingKey {
        case title, fields
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        title = (try? container.decodeIfPresent(String.self, forKey: .title)) ?? ""
        fields = (try? container.decodeIfPresent([ObjectCardField].self, forKey: .fields)) ?? []
    }
}

/// One read-only row. Values are agent-written and untrusted: they are shown
/// as plain `Text`, never interpreted as markup.
struct ObjectCardField: Decodable, Equatable, Sendable, Identifiable {
    var label: String
    /// The raw schema property name, when the row is a property.
    var name: String
    /// The schema's description, shown behind an info button.
    var description: String
    var value: String
    /// No value: drawn as a muted dash.
    var isEmpty: Bool
    /// Identifier values (the id) read best monospaced.
    var isMonospaced: Bool

    var id: String {
        name.isEmpty ? label : name
    }

    init(label: String, name: String = "", description: String = "", value: String, isEmpty: Bool = false, isMonospaced: Bool = false) {
        self.label = label
        self.name = name
        self.description = description
        self.value = value
        self.isEmpty = isEmpty
        self.isMonospaced = isMonospaced
    }

    private enum CodingKeys: String, CodingKey {
        case label, name, description, value, empty, mono
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        name = (try? container.decodeIfPresent(String.self, forKey: .name)) ?? ""
        let display = (try? container.decodeIfPresent(String.self, forKey: .label)) ?? ""
        label = display.isEmpty ? name : display
        description = (try? container.decodeIfPresent(String.self, forKey: .description)) ?? ""
        value = (try? container.decodeIfPresent(String.self, forKey: .value)) ?? ""
        isEmpty = (try? container.decodeIfPresent(Bool.self, forKey: .empty)) ?? value.isEmpty
        isMonospaced = (try? container.decodeIfPresent(Bool.self, forKey: .mono)) ?? false
    }
}

/// One entry of the version timeline, newest first.
struct ObjectCardVersion: Decodable, Equatable, Sendable, Identifiable {
    var version: Int
    /// "v3".
    var label: String
    var status: ObjectCardStatus?
    var key: String
    /// RFC3339 creation time of the version.
    var createdAt: String
    var changes: [ObjectCardChange]

    var id: Int {
        version
    }

    init(version: Int, label: String, status: ObjectCardStatus? = nil, key: String = "", createdAt: String = "", changes: [ObjectCardChange] = []) {
        self.version = version
        self.label = label
        self.status = status
        self.key = key
        self.createdAt = createdAt
        self.changes = changes
    }

    private enum CodingKeys: String, CodingKey {
        case version, label, status, key, changes
        case createdAt = "created_at"
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        version = (try? container.decodeIfPresent(Int.self, forKey: .version)) ?? 0
        let display = (try? container.decodeIfPresent(String.self, forKey: .label)) ?? ""
        label = display.isEmpty ? "v\(version)" : display
        status = (try? container.decodeIfPresent(ObjectCardStatus.self, forKey: .status)) ?? nil
        key = (try? container.decodeIfPresent(String.self, forKey: .key)) ?? ""
        createdAt = (try? container.decodeIfPresent(String.self, forKey: .createdAt)) ?? ""
        changes = (try? container.decodeIfPresent([ObjectCardChange].self, forKey: .changes)) ?? []
    }
}

/// One change line of a version: `+ path`, `− path`, `path: from → to`,
/// `path: changed`, or a muted note.
struct ObjectCardChange: Decodable, Equatable, Sendable {
    enum Kind: String, Sendable {
        case add, remove, update, changed, note
    }

    var kind: Kind
    var label: String
    /// The value before the change (`from` on the wire).
    var before: String
    /// The value after the change (`to` on the wire).
    var after: String

    init(kind: Kind, label: String, before: String = "", after: String = "") {
        self.kind = kind
        self.label = label
        self.before = before
        self.after = after
    }

    private enum CodingKeys: String, CodingKey {
        case kind, label
        case before = "from"
        case after = "to"
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        let raw = (try? container.decodeIfPresent(String.self, forKey: .kind)) ?? ""
        // An unknown kind renders as a note rather than vanishing.
        kind = Kind(rawValue: raw) ?? .note
        label = (try? container.decodeIfPresent(String.self, forKey: .label)) ?? ""
        before = (try? container.decodeIfPresent(String.self, forKey: .before)) ?? ""
        after = (try? container.decodeIfPresent(String.self, forKey: .after)) ?? ""
    }
}
