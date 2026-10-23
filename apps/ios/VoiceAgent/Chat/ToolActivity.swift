import SwiftUI

// MARK: - Category

/// The kind of work a tool call does, in the app's own vocabulary. A tool's
/// wire name is matched to one of these; the row then reports the category's
/// verb and icon instead of the identifier (`entity-search` reads as
/// "Used memory"). Tools the table does not know fall back to `.tool`, so
/// a new tool family degrades to a generic row rather than a broken one.
///
/// The taxonomy is deliberately coarser than one category per tool (~120 of
/// them): the point is a legible verb. Adding a family means adding one row to
/// `prefixes` or `exact`; adding a *category* also means copy for both tenses,
/// which the exhaustive switches below will not compile without.
enum MemoryToolCategory: String, CaseIterable, Sendable {
    case memory
    case search
    case schemas
    case documents
    case web
    case agents
    case skills
    case blueprints
    case projects
    case session
    case connector
    case tool

    /// Names that no prefix describes. Matched before the prefix table.
    private static let exact: [String: MemoryToolCategory] = [
        "remember": .memory,
        "forget": .memory,
        "memory-guide": .memory,
        "lookup": .search,
        "classify-document": .schemas,
        "finalize-discovery": .schemas,
        "list-installed-schemas": .schemas,
        "call_llm": .agents,
        "execute_tool": .agents,
        "trigger_agent": .agents,
    ]

    /// Prefixes in match order: a more specific family is listed before the
    /// family it sits inside (`project-record-` before `project-`).
    private static let prefixes: [(String, MemoryToolCategory)] = [
        ("project-record-", .memory),
        ("graph-", .memory),
        ("entity-", .memory),
        ("tag-", .memory),
        ("journal-", .memory),
        ("search-", .search),
        ("schema-", .schemas),
        ("migration-", .schemas),
        ("document-", .documents),
        ("embedding-", .documents),
        ("extraction-", .documents),
        ("web-", .web),
        ("ts_web_fetch", .web),
        ("agent-", .agents),
        ("adk-", .agents),
        ("skill", .skills),
        ("blueprint-", .blueprints),
        ("project-", .projects),
        ("org-", .projects),
        ("user-", .projects),
        ("session-", .session),
        ("trace-", .session),
        ("account-key-", .connector),
        ("token-", .connector),
        ("mcp-", .connector),
        ("mcp_", .connector),
        ("provider-", .connector),
        ("a2a-", .connector),
    ]

    init(toolName: String) {
        let name = toolName
            .trimmingCharacters(in: .whitespacesAndNewlines)
            .lowercased()
        if let match = Self.exact[name] {
            self = match
        } else if let match = Self.prefixes.first(where: { name.hasPrefix($0.0) }) {
            self = match.1
        } else {
            self = .tool
        }
    }

    /// The glyph that carries the category. Colour is reserved for state
    /// (see `ToolActivityStyle`), so the shape has to do the distinguishing.
    var symbol: String {
        switch self {
        case .memory: "brain"
        case .search: "magnifyingglass"
        case .schemas: "square.stack.3d.up"
        case .documents: "doc.text"
        case .web: "globe"
        case .agents: "person.2"
        case .skills: "book"
        case .blueprints: "square.on.square"
        case .projects: "folder"
        case .session: "clock.arrow.circlepath"
        case .connector: "puzzlepiece.extension"
        case .tool: "wrench.and.screwdriver"
        }
    }

    /// Key for what the row says while the call is running.
    var runningKey: String {
        switch self {
        case .memory: "chat.tool.memory.running"
        case .search: "chat.tool.search.running"
        case .schemas: "chat.tool.schemas.running"
        case .documents: "chat.tool.documents.running"
        case .web: "chat.tool.web.running"
        case .agents: "chat.tool.agents.running"
        case .skills: "chat.tool.skills.running"
        case .blueprints: "chat.tool.blueprints.running"
        case .projects: "chat.tool.projects.running"
        case .session: "chat.tool.session.running"
        case .connector: "chat.tool.connector.running"
        case .tool: "chat.tool.generic.running"
        }
    }

    /// Key for what the row says once the call has settled.
    var doneKey: String {
        switch self {
        case .memory: "chat.tool.memory.done"
        case .search: "chat.tool.search.done"
        case .schemas: "chat.tool.schemas.done"
        case .documents: "chat.tool.documents.done"
        case .web: "chat.tool.web.done"
        case .agents: "chat.tool.agents.done"
        case .skills: "chat.tool.skills.done"
        case .blueprints: "chat.tool.blueprints.done"
        case .projects: "chat.tool.projects.done"
        case .session: "chat.tool.session.done"
        case .connector: "chat.tool.connector.done"
        case .tool: "chat.tool.generic.done"
        }
    }

    var runningPhrase: LocalizedStringKey {
        LocalizedStringKey(runningKey)
    }

    var donePhrase: LocalizedStringKey {
        LocalizedStringKey(doneKey)
    }
}

// MARK: - Look

/// How a row's step reads right now: quiet while idle, accented while running,
/// serious on failure. Colour encodes state, never category — the app's palette
/// has four semantic colours, and a category ramp would collide with the
/// outcome colours used by the approval and question cards beside the row.
enum ToolActivityStyle {
    static func tint(running: Bool, isError: Bool) -> Color {
        if isError {
            return .fgSerious
        }
        return running ? .fgAccent : .fg2
    }

    /// Faint disc behind the glyph. Running and failed steps sit slightly
    /// stronger so they read as the live part of the transcript.
    static func discOpacity(running: Bool, isError: Bool) -> Double {
        running || isError ? 0.16 : 0.12
    }
}

// MARK: - Subject

/// What a step was about, in one line: the query, identifier, path or URL the
/// work concerns, taken from the call's arguments.
enum ToolActivitySubject {
    /// Specific identifiers first, then the descriptive fields, then free text.
    private static let preferredKeys = [
        "query", "q", "search", "pattern", "url", "path", "file_path", "file",
        "entity_id", "document_id", "schema_id", "project_id", "skill", "tool",
        "server", "name", "title", "id", "prompt", "question", "goal", "command",
        "text", "content", "action",
    ]

    /// Past this, the arguments are not parsed as JSON: tool input can run to
    /// megabytes, and rows render on the main thread while scrolling.
    static let parseLimit = 4096

    /// The longest subject line drawn.
    static let lineLimit = 200

    /// Nil when there is nothing to say, so the row simply omits the line.
    static func line(from arguments: String?) -> String? {
        guard let arguments else { return nil }
        let trimmed = arguments.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return nil }

        if trimmed.utf8.count <= parseLimit,
           trimmed.hasPrefix("{"),
           let object = jsonObject(trimmed)
        {
            return preferredSubject(in: object) ?? anyStringSubject(in: object)
        }
        return oneLine(trimmed)
    }

    /// The first value under a key we know by name, in priority order.
    private static func preferredSubject(in object: [String: Any]) -> String? {
        for key in preferredKeys {
            if let value = object[key] as? String, let line = oneLine(value) {
                return line
            }
        }
        return nil
    }

    /// The first string value under any other key, in a stable order — some
    /// tools take a single free-form argument we have no name for.
    private static func anyStringSubject(in object: [String: Any]) -> String? {
        for key in object.keys.sorted() {
            if let value = object[key] as? String, let line = oneLine(value) {
                return line
            }
        }
        return nil
    }

    /// Collapses a multi-line argument dump to its first line, bounded.
    static func oneLine(_ text: String) -> String? {
        let collapsed = text
            .replacingOccurrences(of: "\n", with: " ")
            .trimmingCharacters(in: .whitespacesAndNewlines)
        guard !collapsed.isEmpty else { return nil }
        return String(collapsed.prefix(lineLimit))
    }

    private static func jsonObject(_ text: String) -> [String: Any]? {
        guard let data = text.data(using: .utf8) else { return nil }
        return (try? JSONSerialization.jsonObject(with: data)) as? [String: Any]
    }
}

// MARK: - Timing

/// How long a step took, in the row's short form and VoiceOver's long one.
enum ToolActivityTiming {
    /// "2s", "1m 5s"; nil under a second, which reads as no time at all.
    static func text(_ seconds: TimeInterval?, locale: Locale = .current) -> String? {
        formatted(seconds, width: .narrow, locale: locale)
    }

    /// The same span in words ("2 seconds"), for VoiceOver.
    static func spoken(_ seconds: TimeInterval?, locale: Locale = .current) -> String? {
        formatted(seconds, width: .wide, locale: locale)
    }

    private static func formatted(
        _ seconds: TimeInterval?,
        width: Duration.UnitsFormatStyle.UnitWidth,
        locale: Locale
    ) -> String? {
        guard let seconds, seconds.isFinite, seconds >= 1 else { return nil }
        return Duration.seconds(seconds.rounded())
            .formatted(
                .units(allowed: [.hours, .minutes, .seconds], width: width)
                    .locale(locale)
            )
    }
}

// MARK: - Row pieces

/// A step's icon: its glyph on a faint disc of its state colour.
struct ToolActivityIcon: View {
    let symbol: String
    let tint: Color
    let discOpacity: Double
    @ScaledMetric(relativeTo: .footnote) private var size: CGFloat = 22

    var body: some View {
        Image(systemName: symbol)
            .font(.system(size: size * 0.5, weight: .semibold))
            .foregroundStyle(tint)
            .frame(width: size, height: size)
            .background(tint.opacity(discOpacity), in: Circle())
            .accessibilityHidden(true)
    }
}

extension View {
    /// An accessibility value only when the row has one to add: an empty value
    /// still makes VoiceOver announce a pause.
    @ViewBuilder
    func accessibilityValueWhenPresent(_ value: LocalizedStringKey?) -> some View {
        if let value {
            accessibilityValue(value)
        } else {
            self
        }
    }
}
