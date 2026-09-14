import Foundation

// MARK: - API models (only the fields the app uses; unknown keys ignored)

/// `GET /api/auth/me` — project-scoped token identity. A sandbox token has no
/// (or an empty) `user_id`.
struct AuthMe: Decodable, Equatable, Sendable {
    let userID: String?
    let email: String?
    let scopes: [String]?
    let type: String?
    let projectID: String?
    let projectName: String?
    let orgID: String?
    let tokenID: String?
    let tokenName: String?

    enum CodingKeys: String, CodingKey {
        case userID = "user_id"
        case email
        case scopes
        case type
        case projectID = "project_id"
        case projectName = "project_name"
        case orgID = "org_id"
        case tokenID = "token_id"
        case tokenName = "token_name"
    }
}

/// `GET /api/user/profile`.
struct UserProfile: Decodable, Equatable, Sendable {
    let id: String?
    let subjectID: String?
    let zitadelUserID: String?
    let firstName: String?
    let lastName: String?
    let displayName: String?
    let phoneE164: String?
    let avatarObjectKey: String?
    let email: String?

    enum CodingKeys: String, CodingKey {
        case id
        case subjectID = "subjectId"
        case zitadelUserID = "zitadelUserId"
        case firstName
        case lastName
        case displayName
        case phoneE164
        case avatarObjectKey
        case email
    }
}

/// Project summary from `GET /api/projects/current` (`project` object).
struct ProjectInfo: Decodable, Equatable, Sendable {
    let id: String
    let name: String?
    let orgID: String?

    enum CodingKeys: String, CodingKey {
        case id
        case name
        case orgID = "orgId"
    }
}

/// Organisation summary from `GET /api/orgs`.
struct OrgInfo: Decodable, Equatable, Sendable {
    let id: String
    let name: String?
}

/// Raw response of `GET /api/projects/current`; `project` is null with a
/// message for account-level tokens.
struct CurrentProjectResponse: Decodable, Equatable, Sendable {
    let project: ProjectInfo?
    let message: String?
}

// MARK: - Identity snapshot for the UI

/// Combines the three identity sources with sensible fallbacks so the UI can
/// render a project/account card even when only part of the data exists.
struct IdentitySnapshot: Equatable, Sendable {
    var authMe: AuthMe? = nil
    var profile: UserProfile? = nil
    var project: ProjectInfo? = nil
    /// Resolved organisation (matched by id against `GET /api/orgs`); nil when
    /// the list is unavailable or has no match.
    var organization: OrgInfo? = nil
    /// Server-provided explanation when `project` is null (account-level token).
    var projectMessage: String? = nil

    var hasIdentity: Bool { authMe != nil || profile != nil }

    /// Project display name: `/api/projects/current` first, then `/api/auth/me`.
    var projectName: String? {
        (project?.name).cleaned ?? (authMe?.projectName).cleaned
    }

    /// Project id: `/api/projects/current` first, then `/api/auth/me`.
    var projectID: String? {
        (project?.id).cleaned ?? (authMe?.projectID).cleaned
    }

    /// Organisation display name when resolved (empty names collapse to nil).
    var organizationName: String? {
        (organization?.name).cleaned
    }

    /// Organisation id: resolved org first, then the project's `orgId`, then
    /// `/api/auth/me`'s `org_id`.
    var organizationID: String? {
        (organization?.id).cleaned ?? (project?.orgID).cleaned ?? (authMe?.orgID).cleaned
    }

    /// Display name precedence: profile display name → profile first+last →
    /// auth email local part → project name → token name.
    var displayName: String {
        if let name = profile?.displayName?.trimmed, !name.isEmpty { return name }
        if let combined = profile?.fullName.trimmed, !combined.isEmpty { return combined }
        if let local = authMe?.email?.localPart, !local.isEmpty { return local }
        if let name = authMe?.projectName?.trimmed, !name.isEmpty { return name }
        if let name = authMe?.tokenName?.trimmed, !name.isEmpty { return name }
        return ""
    }

    /// Email precedence: profile email → auth email.
    var email: String {
        if let email = profile?.email?.trimmed, !email.isEmpty { return email }
        return authMe?.email?.trimmed ?? ""
    }

    var initials: String { MemoryIdentity.initials(from: displayName) }
}

// MARK: - Pure helpers

enum MemoryIdentity {
    /// 1–2 uppercase initials from a display name. Empty/whitespace input
    /// yields ""; a single word yields its first character; a compound name
    /// yields the first character of the first two words (matching the Memory
    /// web UI). Uses `Character` (grapheme/rune-safe) uppercasing.
    static func initials(from displayName: String) -> String {
        let words = displayName
            .split(whereSeparator: { $0.isWhitespace })
            .map(String.init)
            .filter { !$0.isEmpty }
        guard let first = words.first, let firstChar = first.first else { return "" }
        let firstInitial = String(firstChar).uppercased()
        guard words.count > 1, let second = words.dropFirst().first,
              let secondChar = second.first else {
            return firstInitial
        }
        return firstInitial + String(secondChar).uppercased()
    }
}

private extension UserProfile {
    var fullName: String {
        [firstName?.trimmed, lastName?.trimmed]
            .compactMap { $0 }
            .filter { !$0.isEmpty }
            .joined(separator: " ")
    }
}

extension String {
    /// Whitespace/newline-trimmed copy.
    var trimmed: String { trimmingCharacters(in: .whitespacesAndNewlines) }
}

extension Optional where Wrapped == String {
    /// Trimmed non-empty value, else nil (collapses empty/whitespace strings).
    var cleaned: String? {
        guard let value = self?.trimmingCharacters(in: .whitespacesAndNewlines),
              !value.isEmpty else { return nil }
        return value
    }
}

private extension String {
    /// Local part of an email-like string (everything before `@`); the whole
    /// string when there is no `@`.
    var localPart: String {
        guard let at = firstIndex(of: "@") else { return self }
        return String(self[..<at])
    }
}
