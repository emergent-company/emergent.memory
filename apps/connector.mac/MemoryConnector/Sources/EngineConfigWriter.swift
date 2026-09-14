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
             projectID: String = "", disabledTools: [String] = []) {
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
    static func yaml(for values: Values) -> String {
        var lines: [String] = []
        lines.append("server_url: \(values.serverURL)")
        lines.append("token: \(values.token)")
        if !values.projectID.isEmpty {
            lines.append("project_id: \(values.projectID)")
        }
        lines.append("instance_id: \(values.instanceID)")
        if !values.disabledTools.isEmpty {
            lines.append("disabled_tools:")
            for name in values.disabledTools {
                lines.append("  - \(name)")
            }
        }
        return lines.joined(separator: "\n") + "\n"
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
                    let name = String(trimmed.dropFirst(2)).trimmingCharacters(in: .whitespaces)
                    if !name.isEmpty { disabledTools.append(name) }
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

    /// Returns the trimmed scalar after `marker` when the line starts with it.
    private static func scalarValue(after marker: String, in line: String) -> String? {
        guard line.hasPrefix(marker) else { return nil }
        let value = line.dropFirst(marker.count).trimmingCharacters(in: .whitespaces)
        return value.isEmpty ? nil : value
    }

    static func write(configURL: URL, content: String) throws {
        let fm = FileManager.default
        let parent = configURL.deletingLastPathComponent()
        if !fm.fileExists(atPath: parent.path) {
            try fm.createDirectory(at: parent, withIntermediateDirectories: true,
                                   attributes: [.posixPermissions: 0o700])
        }
        let data = Data(content.utf8)
        try data.write(to: configURL, options: .atomic)
        // .atomic replaces the file via a temp rename; enforce the mode after.
        try fm.setAttributes([.posixPermissions: 0o600], ofItemAtPath: configURL.path)
    }
}
