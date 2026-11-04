import SwiftUI

// MARK: - Category

/// The kind of work a tool call does, in the app's own vocabulary. A tool's
/// wire name is matched to one of these; the row then reports the category's
/// verb and icon instead of the identifier (`entity-search` reads as
/// "Used memory"). Tools the table does not know fall back to `.tool`, so
/// a new tool family degrades to a generic row rather than a broken one.
///
/// The taxonomy is deliberately coarser than one category per tool (~140 of
/// them): the point is a legible verb. Adding a family means adding one row to
/// `prefixes` or `exact`; adding a *category* also means copy for both tenses,
/// which the exhaustive switches below will not compile without.
///
/// A category's verb has to be true of every tool in it, so a family that both
/// reads and writes gets neutral copy ("Worked in the session") and only a
/// read-only family gets a reading verb ("Read the trace").
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
    case trace
    case workspace
    case connector
    case tool

    /// Names that no prefix describes. Matched before the prefix table.
    private static let exact: [String: MemoryToolCategory] = [
        "remember": .memory,
        "forget": .memory,
        "memory-guide": .memory,
        "lookup": .search,
        "search_entities": .search,
        "set_session_title": .session,
        "classify-document": .schemas,
        "finalize-discovery": .schemas,
        "list-installed-schemas": .schemas,
        "call_llm": .agents,
        "execute_tool": .agents,
        "trigger_agent": .agents,
        "update_agent": .agents,
        "update_agent_definition": .agents,
        "search_mcp_registry": .connector,
        "sync_mcp_server_tools": .connector,
        "toggle_mcp_server_tool": .connector,
        "update_mcp_server": .connector,
        "run_python": .workspace,
        "run_go": .workspace,
    ]

    /// Prefixes in match order: a more specific family is listed before the
    /// family it sits inside (`project-record-` before `project-`,
    /// `adk-session-` before `adk-`).
    private static let prefixes: [(String, MemoryToolCategory)] = [
        ("project-record-", .memory),
        ("graph-", .memory),
        ("entity-", .memory),
        ("tag-", .memory),
        ("journal-", .memory),
        ("relationship-", .memory),
        ("search-", .search),
        ("schema-", .schemas),
        ("migration-", .schemas),
        ("document-", .documents),
        ("embedding-", .documents),
        ("extraction-", .documents),
        ("queue-reextraction", .documents),
        ("web-", .web),
        ("ts_web_fetch", .web),
        ("adk-session-", .session),
        ("agent-", .agents),
        ("adk-", .agents),
        ("skill", .skills),
        ("blueprint-", .blueprints),
        ("project-", .projects),
        ("org-", .projects),
        ("user-", .projects),
        ("session-", .session),
        ("trace-", .trace),
        ("workspace_", .workspace),
        ("account-key-", .connector),
        ("service-token-", .connector),
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
        case .trace: "waveform.path"
        case .workspace: "terminal"
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
        case .trace: "chat.tool.trace.running"
        case .workspace: "chat.tool.workspace.running"
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
        case .trace: "chat.tool.trace.done"
        case .workspace: "chat.tool.workspace.done"
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
    /// Whether a span is long enough to read at all: a value that is not a
    /// finite number, or one under a second, reports no time.
    ///
    /// The rule is numeric, never textual, so it reads the same in every locale.
    /// The summary line decides whether to draw a span with this, and the
    /// formatter below refuses to format anything it rejects; a locale can
    /// therefore never leave the model claiming a span the view declines to draw
    /// (#2192).
    static func isReadable(_ seconds: TimeInterval?) -> Bool {
        guard let seconds else { return false }
        return seconds.isFinite && seconds >= 1
    }

    /// "2s", "1m 5s"; nil under a second, which reads as no time at all.
    static func text(_ seconds: TimeInterval?, locale: Locale = .current) -> String? {
        formatted(seconds, width: .narrow, locale: locale)
    }

    /// The same span in words ("2 seconds"), for VoiceOver.
    static func spoken(_ seconds: TimeInterval?, locale: Locale = .current) -> String? {
        formatted(seconds, width: .wide, locale: locale)
    }

    /// From the first step's start to the last one's end: how long a run of
    /// steps occupied the turn. Nil unless every step is timed, so a run never
    /// reports a length it cannot account for.
    static func span(of tools: [ToolCallInfo]) -> TimeInterval? {
        guard !tools.isEmpty else { return nil }
        var earliest: Date?
        var latest: Date?
        for tool in tools {
            guard let start = tool.startedAt, let duration = tool.duration else { return nil }
            let end = start.addingTimeInterval(duration)
            earliest = min(earliest ?? start, start)
            latest = max(latest ?? end, end)
        }
        guard let earliest, let latest else { return nil }
        return latest.timeIntervalSince(earliest)
    }

    private static func formatted(
        _ seconds: TimeInterval?,
        width: Duration.UnitsFormatStyle.UnitWidth,
        locale: Locale
    ) -> String? {
        guard isReadable(seconds), let seconds else { return nil }
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
