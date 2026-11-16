import Foundation

// `Decodable` mirrors of the `memory-connector` CLI's `--json` documents.
//
// The connector emits snake_case keys; these types use idiomatic Swift names
// with explicit `CodingKeys`, and decode the exact wire keys below. All types
// are `Sendable` so decoded values can cross actor boundaries.

/// `memory-connector auth status --json` (also `auth complete --json`).
struct CLIAuthStatus: Decodable, Equatable, Sendable {
    let schemaVersion: Int
    let server: String?
    let signedIn: Bool
    let email: String?
    let issuer: String?
    let expiresAt: String?
    let expired: Bool?
    /// True when a refresh token is stored, i.e. the access token can be
    /// renewed without user interaction. Older CLI builds omit it.
    let canRefresh: Bool?
    /// True when the provider has already rejected the stored refresh token:
    /// the session exists but cannot be renewed.
    let needsReauth: Bool?

    enum CodingKeys: String, CodingKey {
        case schemaVersion = "schema_version"
        case server
        case signedIn = "signed_in"
        case email
        case issuer
        case expiresAt = "expires_at"
        case expired
        case canRefresh = "can_refresh"
        case needsReauth = "needs_reauth"
    }
}

/// `memory-connector auth refresh --json`.
///
/// Reports whether a renewal actually happened and the session state afterwards.
/// Like `auth access-token`, the refresh token itself is never part of this
/// contract.
struct CLIAuthRefresh: Decodable, Equatable, Sendable {
    let schemaVersion: Int
    let serverURL: String
    let refreshed: Bool
    let expiresAt: String?
    let canRefresh: Bool
    let needsReauth: Bool?

    enum CodingKeys: String, CodingKey {
        case schemaVersion = "schema_version"
        case serverURL = "server_url"
        case refreshed
        case expiresAt = "expires_at"
        case canRefresh = "can_refresh"
        case needsReauth = "needs_reauth"
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
