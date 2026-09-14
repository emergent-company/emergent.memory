import Foundation

/// Parsed `memory-connector status` output.
///
/// The engine emits either the human-readable text form (legacy) or, with
/// `--json`, a stable machine document (`schema_version`, `hub_state`, ...).
/// `parse` prefers the JSON document when present and falls back to the text
/// parser, so the app works against both engine versions.
struct StatusSnapshot: Equatable {
    enum HubState: Equatable {
        case connected
        case notConnected
        case authFailed
        case unreachable
        case missingConfig
        case unknown
    }

    struct DisabledTool: Equatable {
        let name: String
        let reason: String
    }

    let instanceID: String
    let version: String
    let toolNames: [String]
    let hubLine: String
    let hubState: HubState
    let disabledTools: [DisabledTool]

    init(instanceID: String,
         version: String,
         toolNames: [String],
         hubLine: String,
         hubState: HubState,
         disabledTools: [DisabledTool] = []) {
        self.instanceID = instanceID
        self.version = version
        self.toolNames = toolNames
        self.hubLine = hubLine
        self.hubState = hubState
        self.disabledTools = disabledTools
    }

    var toolCount: Int { toolNames.count }

    /// Parses engine `status` stdout. A non-zero exit (missing/broken config,
    /// engine binary problem) maps to `.missingConfig`. A JSON document is used
    /// when present; otherwise the legacy text form is parsed.
    static func parse(stdout: String, exitCode: Int32) -> StatusSnapshot {
        guard exitCode == 0 else {
            return StatusSnapshot(
                instanceID: "",
                version: "",
                toolNames: [],
                hubLine: "Engine did not report status (exit \(exitCode))",
                hubState: .missingConfig
            )
        }
        if let json = parseJSON(stdout) { return json }
        return parseText(stdout)
    }

    /// Static fixture for an engine that is not running (menu placeholder).
    static var engineNotRunning: StatusSnapshot {
        StatusSnapshot(instanceID: "", version: "", toolNames: [],
                       hubLine: "Engine not running", hubState: .unknown)
    }

    // MARK: - JSON (`status --json`)

    private struct JSONDisabledTool: Decodable {
        let name: String
        let reason: String
    }

    private struct JSONHubDetail: Decodable {
        let hubToolCount: Int?
        let localToolCount: Int?
        let sessionCount: Int?
        let error: String?

        enum CodingKeys: String, CodingKey {
            case hubToolCount = "hub_tool_count"
            case localToolCount = "local_tool_count"
            case sessionCount = "session_count"
            case error
        }
    }

    private struct JSONDocument: Decodable {
        let instanceID: String?
        let version: String?
        let tools: [String]?
        let hubState: String?
        let hubDetail: JSONHubDetail?
        let disabledTools: [JSONDisabledTool]?

        enum CodingKeys: String, CodingKey {
            case instanceID = "instance_id"
            case version
            case tools
            case hubState = "hub_state"
            case hubDetail = "hub_detail"
            case disabledTools = "disabled_tools"
        }
    }

    private static func parseJSON(_ stdout: String) -> StatusSnapshot? {
        let trimmed = stdout.trimmingCharacters(in: .whitespacesAndNewlines)
        guard trimmed.hasPrefix("{") else { return nil }
        guard let data = trimmed.data(using: .utf8),
              let doc = try? JSONDecoder().decode(JSONDocument.self, from: data)
        else { return nil }

        let state = hubState(fromWire: doc.hubState)
        return StatusSnapshot(
            instanceID: doc.instanceID ?? "",
            version: doc.version ?? "",
            toolNames: doc.tools ?? [],
            hubLine: hubLine(for: state, detail: doc.hubDetail),
            hubState: state,
            disabledTools: (doc.disabledTools ?? []).map {
                DisabledTool(name: $0.name, reason: $0.reason)
            }
        )
    }

    private static func hubState(fromWire wire: String?) -> HubState {
        switch wire {
        case "connected": return .connected
        case "not_connected": return .notConnected
        case "auth_failed": return .authFailed
        case "unreachable": return .unreachable
        case "missing_config": return .missingConfig
        default: return .unknown
        }
    }

    /// Rebuilds the human hub line from the structured fields, matching the
    /// engine's text rendering so the UI copy is unchanged.
    private static func hubLine(for state: HubState, detail: JSONHubDetail?) -> String {
        switch state {
        case .connected:
            let hub = detail?.hubToolCount ?? 0
            let local = detail?.localToolCount ?? 0
            if hub != local {
                return "connected — hub shows \(hub) tool(s) for this instance but \(local) local (mismatch; restart 'relay' to re-register)"
            }
            return "connected — hub shows \(hub) tool(s), matches local set"
        case .notConnected:
            return "not connected — instance not found among \(detail?.sessionCount ?? 0) hub session(s)"
        case .authFailed:
            return "authentication failed — the server rejected the token"
        case .unreachable:
            return "unreachable — " + (detail?.error ?? "")
        case .missingConfig:
            return "missing config — run 'memory-connector init' first"
        case .unknown:
            return ""
        }
    }

    // MARK: - Text (legacy)

    /// Pure parser for the engine status stdout shape:
    /// `instance: <id>`, `version: 0.1.0`,
    /// `tools (n): a, b, c` | `tools: none (…note…)`,
    /// `hub: <connected — … | not connected — … | authentication failed … | unreachable — …>`.
    private static func parseText(_ stdout: String) -> StatusSnapshot {
        var instanceID = ""
        var version = ""
        var toolNames: [String] = []
        var hubLine = ""
        var hubState: HubState = .unknown

        for rawLine in stdout.split(separator: "\n", omittingEmptySubsequences: false) {
            let line = rawLine.trimmingCharacters(in: .whitespacesAndNewlines)
            if line.isEmpty { continue }
            if let value = value(after: "instance: ", in: line) {
                instanceID = value
            } else if let value = value(after: "version: ", in: line) {
                version = value
            } else if line.hasPrefix("tools") {
                toolNames = Self.parseToolNames(line)
            } else if let value = value(after: "hub: ", in: line) {
                hubLine = value
                hubState = Self.hubState(fromText: value)
            }
        }
        return StatusSnapshot(instanceID: instanceID, version: version,
                              toolNames: toolNames, hubLine: hubLine, hubState: hubState)
    }

    // MARK: - Pure helpers

    private static func value(after marker: String, in line: String) -> String? {
        guard line.hasPrefix(marker) else { return nil }
        let rest = line.dropFirst(marker.count)
        let trimmed = rest.trimmingCharacters(in: .whitespaces)
        return trimmed.isEmpty ? nil : trimmed
    }

    private static func parseToolNames(_ line: String) -> [String] {
        if line.contains(": none") { return [] }
        guard let colon = line.firstIndex(of: ":") else { return [] }
        let rest = line[line.index(after: colon)...]
        return rest
            .split(separator: ",")
            .map { $0.trimmingCharacters(in: .whitespaces) }
            .filter { !$0.isEmpty }
    }

    private static func hubState(fromText hubLine: String) -> HubState {
        if hubLine.hasPrefix("connected") { return .connected }
        if hubLine.hasPrefix("not connected") { return .notConnected }
        if hubLine.hasPrefix("authentication failed") { return .authFailed }
        if hubLine.hasPrefix("unreachable") { return .unreachable }
        return .unknown
    }
}
