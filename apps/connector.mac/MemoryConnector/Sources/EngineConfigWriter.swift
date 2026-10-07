import Foundation

/// Materializes the engine's YAML config file from explicit values.
///
/// The token arrives as a parameter; this writer never reads a secret store.
/// Config is normally written by the connector CLI (`projects use`); this type
/// also parses config files (`read`/`readDetailed`) for the lifecycle policy
/// and first-run import.
///
/// Pure and testable: callers pass an explicit config URL and values; no
/// singletons, no Keychain access inside.
enum EngineConfigWriter {
    /// A validated connection profile ready to materialize.
    struct Values: Equatable {
        let serverURL: String
        let token: String
        let instanceID: String
        let projectID: String
        let disabledTools: [String]

        init(serverURL: String, token: String, instanceID: String,
             projectID: String = "", disabledTools: [String] = [])
        {
            self.serverURL = serverURL
            self.token = token
            self.instanceID = instanceID
            self.projectID = projectID
            self.disabledTools = disabledTools
        }
    }

    /// Builds the YAML document the engine parses. Emission mirrors the
    /// engine's own config format (connector/internal/config): plain scalars,
    /// project_id omitted when empty, disabled_tools omitted when empty.
    ///
    /// Every scalar is emitted through ``scalar(_:)`` so a value containing a
    /// colon, quote, leading indicator, or newline is double-quoted/escaped
    /// instead of corrupting the document.
    static func yaml(for values: Values) -> String {
        var lines: [String] = []
        lines.append("server_url: \(scalar(values.serverURL))")
        lines.append("token: \(scalar(values.token))")
        if !values.projectID.isEmpty {
            lines.append("project_id: \(scalar(values.projectID))")
        }
        lines.append("instance_id: \(scalar(values.instanceID))")
        if !values.disabledTools.isEmpty {
            lines.append("disabled_tools:")
            for name in values.disabledTools {
                lines.append("  - \(scalar(name))")
            }
        }
        return lines.joined(separator: "\n") + "\n"
    }

    /// Emits one scalar safely: plain when it cannot be misread by a YAML
    /// parser, otherwise a double-quoted, escaped string.
    private static func scalar(_ value: String) -> String {
        needsQuoting(value) ? quoted(value) : value
    }

    /// Whether `value` must be double-quoted to survive a YAML read unchanged.
    private static func needsQuoting(_ value: String) -> Bool {
        if value.isEmpty {
            return true
        }
        if value.first?.isWhitespace == true || value.last?.isWhitespace == true {
            return true
        }
        // `: ` is the mapping separator and a trailing `:` starts a block; both
        // would swallow the rest of the scalar.
        if value.contains(": ") || value.hasSuffix(":") {
            return true
        }
        // ` #` starts a comment, discarding the remainder.
        if value.contains(" #") {
            return true
        }
        // Quotes and control characters break a plain scalar.
        if value.contains("\"") || value.contains("'") {
            return true
        }
        // Leading indicator characters begin a document/list/alias construct.
        let leadingIndicators = Set("-?:,[]{}#&*!|>%@`")
        if let first = value.first, leadingIndicators.contains(first) {
            return true
        }
        return value.unicodeScalars.contains { $0.value < 0x20 || $0.value == 0x7F }
    }

    /// Double-quotes `value`, escaping characters the YAML reader would decode
    /// (quotes, backslash, common whitespace, and any other control character).
    private static func quoted(_ value: String) -> String {
        var out = "\""
        for scalar in value.unicodeScalars {
            switch scalar.value {
            case 0x22: out += "\\\"" // "
            case 0x5C: out += "\\\\" // backslash
            case 0x0A: out += "\\n"
            case 0x0D: out += "\\r"
            case 0x09: out += "\\t"
            case 0x00: out += "\\0"
            default:
                if scalar.value < 0x20 || scalar.value == 0x7F {
                    out += String(format: "\\u%04X", scalar.value)
                } else {
                    out.unicodeScalars.append(scalar)
                }
            }
        }
        out += "\""
        return out
    }

    /// Decodes a scalar read from disk: a double-quoted string is unescaped;
    /// a plain (legacy) scalar is returned unchanged.
    private static func unquote(_ raw: String) -> String {
        guard raw.count >= 2, raw.hasPrefix("\""), raw.hasSuffix("\"") else {
            return raw
        }
        let chars = Array(raw.dropFirst().dropLast())
        var result = ""
        var index = 0
        while index < chars.count {
            let character = chars[index]
            if character == "\\", index + 1 < chars.count {
                let escaped = chars[index + 1]
                index += 2
                let (replacement, consumed) = decodeEscape(escaped, chars: chars, index: index)
                result += replacement
                index += consumed
            } else {
                result += String(character)
                index += 1
            }
        }
        return result
    }

    /// Decodes one escape sequence. `escape` is the character after the
    /// backslash; `chars[index]` is the first hex digit for `\x`/`\u`. Returns
    /// the decoded replacement plus the number of extra characters consumed.
    private static func decodeEscape(_ escape: Character,
                                     chars: [Character],
                                     index: Int) -> (replacement: String, consumed: Int)
    {
        if escape == "x" {
            if let scalar = unicodeEscape(chars[index ..< min(index + 2, chars.count)]) {
                return (String(scalar), 2)
            }
            return ("", 0)
        }
        if escape == "u" {
            if let scalar = unicodeEscape(chars[index ..< min(index + 4, chars.count)]) {
                return (String(scalar), 4)
            }
            return ("", 0)
        }
        let simple: [Character: String] = [
            "\"": "\"", "\\": "\\", "n": "\n", "r": "\r", "t": "\t", "0": "\0",
        ]
        return (simple[escape] ?? String(escape), 0)
    }

    private static func unicodeEscape(_ hex: ArraySlice<Character>) -> Character? {
        guard let value = UInt32(String(hex), radix: 16), let scalar = UnicodeScalar(value) else {
            return nil
        }
        return Character(scalar)
    }

    /// Writes the config file at configURL (creating the parent directory with
    /// mode 0700), then sets the file's permissions to 0600.
    static func write(configURL: URL, values: Values) throws {
        let content = yaml(for: values)
        try write(configURL: configURL, content: content)
    }

    /// A parsed config plus provenance needed by import: whether the source
    /// file actually contained a `disabled_tools` block. This distinguishes an
    /// explicit (possibly empty) set — which must be honored — from "the file
    /// never configured tools", which must NOT silently turn tools back on.
    struct ParsedConfig: Equatable {
        let values: Values
        let hasDisabledTools: Bool
    }

    /// Reads a config file written in the engine's format (the same shape
    /// `yaml(for:)` emits, and the shape the engine CLI's `init` writes).
    /// Line-based and tolerant: unknown lines are ignored. Returns nil when
    /// the file is missing/unreadable or `server_url` is absent/empty.
    ///
    /// This backs first-run import of an existing CLI-created config into the
    /// app's own state (UserDefaults + Keychain).
    static func read(configURL: URL) -> Values? {
        readDetailed(configURL: configURL)?.values
    }

    /// Like `read`, but also reports whether the file declared a
    /// `disabled_tools` block.
    static func readDetailed(configURL: URL) -> ParsedConfig? {
        guard let content = try? String(contentsOf: configURL, encoding: .utf8) else {
            return nil
        }

        var serverURL = ""
        var token = ""
        var instanceID = ""
        var projectID = ""
        var disabledTools: [String] = []
        var inDisabledTools = false
        var hasDisabledTools = false

        for rawLine in content.split(separator: "\n", omittingEmptySubsequences: false) {
            let line = String(rawLine)

            if inDisabledTools {
                let trimmed = line.trimmingCharacters(in: .whitespaces)
                if trimmed.hasPrefix("- ") {
                    let name = unquote(String(trimmed.dropFirst(2)).trimmingCharacters(in: .whitespaces))
                    if !name.isEmpty {
                        disabledTools.append(name)
                    }
                    continue
                }
                inDisabledTools = false
                // fall through to parse this line normally
            }

            if let value = scalarValue(after: "server_url:", in: line) {
                serverURL = value
            } else if let value = scalarValue(after: "token:", in: line) {
                token = value
            } else if let value = scalarValue(after: "instance_id:", in: line) {
                instanceID = value
            } else if let value = scalarValue(after: "project_id:", in: line) {
                projectID = value
            } else if line.trimmingCharacters(in: .whitespaces) == "disabled_tools:" {
                inDisabledTools = true
                hasDisabledTools = true
            }
            // unknown lines: ignored
        }

        guard !serverURL.isEmpty else { return nil }
        let values = Values(serverURL: serverURL, token: token, instanceID: instanceID,
                            projectID: projectID, disabledTools: disabledTools)
        return ParsedConfig(values: values, hasDisabledTools: hasDisabledTools)
    }

    /// Returns the trimmed scalar after `marker` when the line starts with it,
    /// decoded (unquoted) if it was double-quoted.
    private static func scalarValue(after marker: String, in line: String) -> String? {
        guard line.hasPrefix(marker) else { return nil }
        let value = line.dropFirst(marker.count).trimmingCharacters(in: .whitespaces)
        guard !value.isEmpty else { return nil }
        return unquote(value)
    }

    static func write(configURL: URL, content: String) throws {
        let fileManager = FileManager.default
        let parent = configURL.deletingLastPathComponent()
        if !fileManager.fileExists(atPath: parent.path) {
            try fileManager.createDirectory(at: parent, withIntermediateDirectories: true,
                                            attributes: [.posixPermissions: 0o700])
        }
        let data = Data(content.utf8)
        try data.write(to: configURL, options: .atomic)
        // .atomic replaces the file via a temp rename; enforce the mode after.
        try fileManager.setAttributes([.posixPermissions: 0o600], ofItemAtPath: configURL.path)
    }
}
