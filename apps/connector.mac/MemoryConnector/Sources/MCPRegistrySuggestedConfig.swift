import Foundation

/// Lenient decoder for the connector's `suggested` config payload.
///
/// The management contract spells the fields the way the create payload does
/// (lowercase, with `disabled_tools`) so the result POSTs straight to
/// `POST /api/mcp-servers`; the normalized value is therefore the existing
/// ``HostedMCPServerConfig``. A direct `mcphost.ServerConfig` JSON marshal would
/// be PascalCase, so both spellings are accepted here.
struct MCPRegistrySuggestedConfig: Decodable {
    private var name = ""
    private var transport: MCPTransport?
    private var enabled = true
    private var command: String?
    private var args: [String]?
    private var env: [String: String]?
    private var url: String?
    private var headers: [String: String]?
    private var disabledTools: [String]?

    enum CodingKeys: String, CodingKey {
        case name, transport, enabled, command, args, env, url, headers
        case disabledTools = "disabled_tools"
        // PascalCase spellings (a direct mcphost.ServerConfig marshal) are
        // accepted too. SwiftLint requires lowercase Swift case names, so the
        // JSON key is carried in the raw value.
        case namePascal = "Name"
        case transportPascal = "Transport"
        case enabledPascal = "Enabled"
        case commandPascal = "Command"
        case argsPascal = "Args"
        case envPascal = "Env"
        case urlPascal = "URL"
        case headersPascal = "Headers"
        case disabledToolsPascal = "DisabledTools"
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        name = try container.decodeIfPresent(String.self, forKey: .name)
            ?? container.decodeIfPresent(String.self, forKey: .namePascal) ?? ""
        transport = try container.decodeIfPresent(MCPTransport.self, forKey: .transport)
            ?? container.decodeIfPresent(MCPTransport.self, forKey: .transportPascal)
        enabled = try container.decodeIfPresent(Bool.self, forKey: .enabled)
            ?? container.decodeIfPresent(Bool.self, forKey: .enabledPascal) ?? true
        command = try container.decodeIfPresent(String.self, forKey: .command)
            ?? container.decodeIfPresent(String.self, forKey: .commandPascal)
        args = try container.decodeIfPresent([String].self, forKey: .args)
            ?? container.decodeIfPresent([String].self, forKey: .argsPascal)
        env = try container.decodeIfPresent([String: String].self, forKey: .env)
            ?? container.decodeIfPresent([String: String].self, forKey: .envPascal)
        url = try container.decodeIfPresent(String.self, forKey: .url)
            ?? container.decodeIfPresent(String.self, forKey: .urlPascal)
        headers = try container.decodeIfPresent([String: String].self, forKey: .headers)
            ?? container.decodeIfPresent([String: String].self, forKey: .headersPascal)
        disabledTools = try container.decodeIfPresent([String].self, forKey: .disabledTools)
            ?? container.decodeIfPresent([String].self, forKey: .disabledToolsPascal)
    }

    /// The create payload, or `nil` when the payload carries no transport (an
    /// entry the connector cannot map locally).
    func config() -> HostedMCPServerConfig? {
        guard let transport else { return nil }
        return HostedMCPServerConfig(name: name,
                                     transport: transport,
                                     enabled: enabled,
                                     command: command,
                                     args: args,
                                     env: env,
                                     url: url,
                                     headers: headers,
                                     disabledTools: disabledTools)
    }
}
