import Foundation

// MARK: - Registry entry models

//
// Decodable mirrors of the connector's loopback registry API, which proxies the
// official MCP registry through Memory and returns a flatter shape than the
// upstream registry (the connector unwraps `{server, _meta}` entries and
// aggregates per-package environment metadata). The management API reaches
// `127.0.0.1:8931` only; nothing here talks to the upstream registry directly.

/// A repository the registry entry points back to.
struct RegistryRepository: Codable, Equatable, Sendable {
    var url: String
    /// Hosting source, e.g. `github`.
    var source: String
}

/// One package a registry entry can be run from locally (npm, pypi, …).
struct RegistryPackage: Codable, Equatable, Sendable {
    /// Package ecosystem, e.g. `npm` or `pypi`.
    var registryType: String
    var name: String?
    /// The install identifier (for npm, the package name).
    var identifier: String?
    /// The package transport, e.g. `stdio`.
    var transport: String?

    init(registryType: String,
         name: String? = nil,
         identifier: String? = nil,
         transport: String? = nil)
    {
        self.registryType = registryType
        self.name = name
        self.identifier = identifier
        self.transport = transport
    }
}

/// A remote endpoint the entry exposes (`streamable-http`/`sse`).
struct RegistryRemote: Codable, Equatable, Sendable {
    var type: String
    var url: String
}

/// Metadata for one environment variable the entry (or its package) needs.
struct RegistryEnvVar: Codable, Equatable, Identifiable, Sendable {
    var name: String
    var description: String?
    var isRequired: Bool
    var isSecret: Bool
    /// A placeholder value, when the registry supplies one. May be empty for a
    /// value the user must fill in.
    var value: String?

    var id: String {
        name
    }

    init(name: String,
         description: String? = nil,
         isRequired: Bool = false,
         isSecret: Bool = false,
         value: String? = nil)
    {
        self.name = name
        self.description = description
        self.isRequired = isRequired
        self.isSecret = isSecret
        self.value = value
    }

    enum CodingKeys: String, CodingKey {
        case name, description, isRequired, isSecret, value
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        name = try container.decode(String.self, forKey: .name)
        description = try container.decodeIfPresent(String.self, forKey: .description)
        isRequired = try container.decodeIfPresent(Bool.self, forKey: .isRequired) ?? false
        isSecret = try container.decodeIfPresent(Bool.self, forKey: .isSecret) ?? false
        value = try container.decodeIfPresent(String.self, forKey: .value)
    }
}

/// Metadata for one HTTP header a remote entry needs. Same shape as
/// ``RegistryEnvVar``; kept distinct so env vars and headers stay type-safe.
struct RegistryHeader: Codable, Equatable, Identifiable, Sendable {
    var name: String
    var description: String?
    var isRequired: Bool
    var isSecret: Bool
    var value: String?

    var id: String {
        name
    }

    init(name: String,
         description: String? = nil,
         isRequired: Bool = false,
         isSecret: Bool = false,
         value: String? = nil)
    {
        self.name = name
        self.description = description
        self.isRequired = isRequired
        self.isSecret = isSecret
        self.value = value
    }

    enum CodingKeys: String, CodingKey {
        case name, description, isRequired, isSecret, value
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        name = try container.decode(String.self, forKey: .name)
        description = try container.decodeIfPresent(String.self, forKey: .description)
        isRequired = try container.decodeIfPresent(Bool.self, forKey: .isRequired) ?? false
        isSecret = try container.decodeIfPresent(Bool.self, forKey: .isSecret) ?? false
        value = try container.decodeIfPresent(String.self, forKey: .value)
    }
}

/// One entry in the registry (the `servers` items of the search response, and
/// the `server` of the detail response).
struct RegistryServer: Codable, Equatable, Identifiable, Sendable {
    /// The registry id, e.g. `io.github.ChromeDevTools/chrome-devtools-mcp`.
    var name: String
    var title: String?
    var description: String?
    var version: String?
    var repository: RegistryRepository?
    var packages: [RegistryPackage]
    var remotes: [RegistryRemote]
    var envVars: [RegistryEnvVar]
    var headers: [RegistryHeader]
    var hasPackages: Bool
    var hasRemotes: Bool

    var id: String {
        name
    }

    /// The short, connector-friendly name derived from the registry id:
    /// `io.github.ChromeDevTools/chrome-devtools-mcp` → `chrome-devtools-mcp`.
    var localName: String {
        let trimmed = name.trimmingCharacters(in: .whitespacesAndNewlines)
        guard let slash = trimmed.lastIndex(of: "/") else { return trimmed }
        let tail = String(trimmed[trimmed.index(after: slash)...])
        return tail.isEmpty ? trimmed : tail
    }

    /// Prefer the human title, then the derived local name.
    var displayTitle: String {
        if let title, !title.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            return title
        }
        return localName
    }

    init(name: String,
         title: String? = nil,
         description: String? = nil,
         version: String? = nil,
         repository: RegistryRepository? = nil,
         packages: [RegistryPackage] = [],
         remotes: [RegistryRemote] = [],
         envVars: [RegistryEnvVar] = [],
         headers: [RegistryHeader] = [],
         hasPackages: Bool? = nil,
         hasRemotes: Bool? = nil)
    {
        self.name = name
        self.title = title
        self.description = description
        self.version = version
        self.repository = repository
        self.packages = packages
        self.remotes = remotes
        self.envVars = envVars
        self.headers = headers
        self.hasPackages = hasPackages ?? !packages.isEmpty
        self.hasRemotes = hasRemotes ?? !remotes.isEmpty
    }

    enum CodingKeys: String, CodingKey {
        case name, title, description, version, repository, packages, remotes
        case envVars, headers, hasPackages, hasRemotes
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        name = try container.decode(String.self, forKey: .name)
        title = try container.decodeIfPresent(String.self, forKey: .title)
        description = try container.decodeIfPresent(String.self, forKey: .description)
        version = try container.decodeIfPresent(String.self, forKey: .version)
        repository = try container.decodeIfPresent(RegistryRepository.self, forKey: .repository)
        packages = try container.decodeIfPresent([RegistryPackage].self, forKey: .packages) ?? []
        remotes = try container.decodeIfPresent([RegistryRemote].self, forKey: .remotes) ?? []
        envVars = try container.decodeIfPresent([RegistryEnvVar].self, forKey: .envVars) ?? []
        headers = try container.decodeIfPresent([RegistryHeader].self, forKey: .headers) ?? []
        hasPackages = try container.decodeIfPresent(Bool.self, forKey: .hasPackages) ?? !packages.isEmpty
        hasRemotes = try container.decodeIfPresent(Bool.self, forKey: .hasRemotes) ?? !remotes.isEmpty
    }
}

/// `GET /api/mcp-registry/search` — a page of registry entries.
struct MCPRegistrySearchResult: Codable, Equatable, Sendable {
    var servers: [RegistryServer]
    var nextCursor: String?
    var count: Int

    init(servers: [RegistryServer], nextCursor: String? = nil, count: Int? = nil) {
        self.servers = servers
        self.nextCursor = nextCursor
        self.count = count ?? servers.count
    }

    enum CodingKeys: String, CodingKey {
        case servers, nextCursor, count
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        servers = try container.decodeIfPresent([RegistryServer].self, forKey: .servers) ?? []
        nextCursor = try container.decodeIfPresent(String.self, forKey: .nextCursor)
        count = try container.decodeIfPresent(Int.self, forKey: .count) ?? servers.count
    }
}

/// `GET /api/mcp-registry/servers/{name}` — one entry plus the connector's
/// suggested local configuration.
///
/// `suggested` carries the same shape as the create payload, so it decodes
/// straight into ``HostedMCPServerConfig`` and can be POSTed unchanged to
/// `POST /api/mcp-servers`. It is `nil` when the entry has no local package or
/// remote endpoint the connector can suggest.
struct MCPRegistryServerDetail: Codable, Equatable, Sendable {
    var server: RegistryServer
    var suggested: HostedMCPServerConfig?
    var envVars: [RegistryEnvVar]
    var headers: [RegistryHeader]
    var warnings: [String]

    init(server: RegistryServer,
         suggested: HostedMCPServerConfig? = nil,
         envVars: [RegistryEnvVar] = [],
         headers: [RegistryHeader] = [],
         warnings: [String] = [])
    {
        self.server = server
        self.suggested = suggested
        self.envVars = envVars
        self.headers = headers
        self.warnings = warnings
    }

    enum CodingKeys: String, CodingKey {
        case server, suggested, envVars, headers, warnings
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        server = try container.decode(RegistryServer.self, forKey: .server)
        // `suggested` is decoded leniently: the contract is the lowercase create
        // shape, and an empty/transport-less object is treated as "unsupported"
        // rather than failing the whole detail decode.
        suggested = (try? container.decode(MCPRegistrySuggestedConfig.self, forKey: .suggested))?.config()
        // The connector aggregates the declarations on the entry; also accept
        // them at the top level (the management contract) and fall back to the
        // entry. Both an object array and a bare name array are tolerated so a
        // thin proxy cannot break the whole detail decode.
        let topLevelEnv: [RegistryEnvVar] = if let objects = try? container.decode([RegistryEnvVar].self, forKey: .envVars) {
            objects
        } else if let names = try? container.decode([String].self, forKey: .envVars) {
            names.map { RegistryEnvVar(name: $0) }
        } else {
            []
        }
        envVars = topLevelEnv.isEmpty ? server.envVars : topLevelEnv

        let topLevelHeaders: [RegistryHeader] = if let objects = try? container.decode([RegistryHeader].self, forKey: .headers) {
            objects
        } else if let names = try? container.decode([String].self, forKey: .headers) {
            names.map { RegistryHeader(name: $0) }
        } else {
            []
        }
        headers = topLevelHeaders.isEmpty ? server.headers : topLevelHeaders

        warnings = try container.decodeIfPresent([String].self, forKey: .warnings) ?? []
    }
}

// MARK: - Install notes

/// Required/secret metadata surfaced alongside a prefilled registry suggestion.
/// Pure data (no view concerns) so the labels the Add sheet renders are
/// unit-testable.
struct MCPRegistryNotes: Equatable, Sendable {
    var warnings: [String]
    var requiredEnv: [String]
    var requiredHeaders: [String]
    var secretEnv: [String]
    var secretHeaders: [String]

    init(warnings: [String] = [],
         requiredEnv: [String] = [],
         requiredHeaders: [String] = [],
         secretEnv: [String] = [],
         secretHeaders: [String] = [])
    {
        self.warnings = warnings
        self.requiredEnv = requiredEnv
        self.requiredHeaders = requiredHeaders
        self.secretEnv = secretEnv
        self.secretHeaders = secretHeaders
    }

    init(detail: MCPRegistryServerDetail) {
        warnings = detail.warnings
        requiredEnv = detail.envVars.filter(\.isRequired).map(\.name)
        requiredHeaders = detail.headers.filter(\.isRequired).map(\.name)
        secretEnv = detail.envVars.filter(\.isSecret).map(\.name)
        secretHeaders = detail.headers.filter(\.isSecret).map(\.name)
    }

    var isEmpty: Bool {
        warnings.isEmpty
            && requiredEnv.isEmpty
            && requiredHeaders.isEmpty
            && secretEnv.isEmpty
            && secretHeaders.isEmpty
    }

    /// Required env var names, with `(secret)` appended to the secret ones.
    var requiredEnvLabels: [String] {
        requiredEnv.map { secretEnv.contains($0) ? "\($0) (secret)" : $0 }
    }

    /// Required header names, with `(secret)` appended to the secret ones.
    var requiredHeaderLabels: [String] {
        requiredHeaders.map { secretHeaders.contains($0) ? "\($0) (secret)" : $0 }
    }
}

// MARK: - Form prefill

extension MCPServerForm {
    /// Seeds an editable form from the connector's suggested config. Every field
    /// stays editable; this only sets the starting values. `fallbackName` is
    /// used when the suggestion carries no name (derived from the registry id).
    init(suggested: HostedMCPServerConfig, fallbackName: String? = nil) {
        self.init(config: suggested)
        if name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
           let fallbackName,
           !fallbackName.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        {
            name = fallbackName
        }
    }
}
