/// One entry the connector skipped during a Claude-config import.
struct HostedMCPImportSkip: Decodable, Equatable, Sendable {
    var name: String
    var reason: String
}

/// One entry that failed to import.
struct HostedMCPImportFailure: Decodable, Equatable, Sendable {
    var name: String
    var error: String
}

/// One non-fatal observation about an imported entry.
struct HostedMCPImportWarning: Decodable, Equatable, Sendable {
    var name: String
    var message: String
}

/// `POST /api/mcp-servers/import` result. Valid entries are applied even when
/// others fail. Warnings is optional because the connector omits an empty list.
struct HostedMCPImportResult: Decodable, Equatable, Sendable {
    var created: [String]
    var updated: [String]
    var skipped: [HostedMCPImportSkip]
    var failed: [HostedMCPImportFailure]
    var warnings: [HostedMCPImportWarning]?
    var dryRun: Bool
}
