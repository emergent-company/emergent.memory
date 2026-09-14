import Foundation

/// `Decodable` mirrors of the `memory-connector` CLI's `--json` documents.
///
/// The connector emits snake_case keys; these types use idiomatic Swift names
/// with explicit `CodingKeys`, and decode the exact wire keys below. All types
/// are `Sendable` so decoded values can cross actor boundaries.

/// `memory-connector auth status --json` (also `auth complete --json`).
struct CLIAuthStatus: Decodable, Equatable, Sendable {
    let schemaVersion: Int
    let server: String?
    let signedIn: Bool
    let email: String?
    let issuer: String?
    let expiresAt: String?
    let expired: Bool?

    enum CodingKeys: String, CodingKey {
        case schemaVersion = "schema_version"
        case server
        case signedIn = "signed_in"
        case email
        case issuer
        case expiresAt = "expires_at"
        case expired
    }
}

/// `memory-connector auth start --json`.
struct CLIPKCEStart: Decodable, Equatable, Sendable {
    let schemaVersion: Int
    let loginID: String
    let authorizeURL: String
    let state: String
    let expiresAt: String

    enum CodingKeys: String, CodingKey {
        case schemaVersion = "schema_version"
        case loginID = "login_id"
        case authorizeURL = "authorize_url"
        case state
        case expiresAt = "expires_at"
    }
}

/// `memory-connector auth access-token --json`.
///
/// Carries only the short-lived access token and its expiry; the refresh token
/// is never part of this contract.
struct CLIAccessToken: Decodable, Equatable, Sendable {
    let schemaVersion: Int
    let serverURL: String
    let accessToken: String
    let expiresAt: String

    enum CodingKeys: String, CodingKey {
        case schemaVersion = "schema_version"
        case serverURL = "server_url"
        case accessToken = "access_token"
        case expiresAt = "expires_at"
    }
}

/// One project row in `memory-connector projects list --json`.
struct CLIProject: Decodable, Equatable, Sendable {
    let id: String
    let name: String
    /// Owning organisation id, when the CLI reports one (`org_id`). Older CLI
    /// builds omit it, so it stays optional.
    let orgId: String?
    let active: Bool

    enum CodingKeys: String, CodingKey {
        case id
        case name
        case orgId = "org_id"
        case active
    }
}

/// `memory-connector projects list --json`.
struct CLIProjectsList: Decodable, Equatable, Sendable {
    let schemaVersion: Int
    let server: String
    let projects: [CLIProject]

    enum CodingKeys: String, CodingKey {
        case schemaVersion = "schema_version"
        case server
        case projects
    }
}

/// Project reference embedded in `projects use --json`.
struct CLIProjectRef: Decodable, Equatable, Sendable {
    let id: String
    let name: String
}

/// `memory-connector projects use --json`.
struct CLIProjectUse: Decodable, Equatable, Sendable {
    let schemaVersion: Int
    let server: String
    let project: CLIProjectRef
    let config: String

    enum CodingKeys: String, CodingKey {
        case schemaVersion = "schema_version"
        case server
        case project
        case config
    }
}
