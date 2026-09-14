import Foundation
import Security

/// A pre-CLI, app-owned OIDC session recovered from the legacy storage (file
/// or Keychain) so it can be imported into the connector CLI.
struct LegacySession: Equatable, Sendable {
    var accessToken: String
    var refreshToken: String?
    var expiresAt: Date?
    var issuer: String?
    var clientID: String?
}

/// Read side of the legacy session migration source.
protocol LegacySessionReading: Sendable {
    func read() -> LegacySession?
}

/// Clear side: removes the legacy file and Keychain items after a successful
/// import. Best-effort; missing items are not an error.
protocol LegacySessionClearing: Sendable {
    func clear()
}

/// Minimal Keychain seam for the legacy OIDC items, so the reader and clearer
/// can be tested without touching the real Keychain.
protocol LegacyKeychainReading: Sendable {
    func load(account: String) throws -> String?
    func delete(account: String) throws
}

/// Security-framework implementation of `LegacyKeychainReading`, scoped to the
/// connector's legacy OIDC service.
struct SystemLegacyKeychain: LegacyKeychainReading {
    static let service = "com.emergent.memory.connector"

    func load(account: String) throws -> String? {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: Self.service,
            kSecAttrAccount as String: account,
            kSecReturnData as String: true,
            kSecMatchLimit as String: kSecMatchLimitOne,
        ]
        var result: CFTypeRef?
        let status = SecItemCopyMatching(query as CFDictionary, &result)
        switch status {
        case errSecSuccess:
            guard let data = result as? Data else { return nil }
            return String(data: data, encoding: .utf8)
        case errSecItemNotFound:
            return nil
        default:
            throw LegacyKeychainError.unexpectedStatus(status)
        }
    }

    func delete(account: String) throws {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: Self.service,
            kSecAttrAccount as String: account,
        ]
        let status = SecItemDelete(query as CFDictionary)
        guard status == errSecSuccess || status == errSecItemNotFound else {
            throw LegacyKeychainError.unexpectedStatus(status)
        }
    }
}

/// Errors surfaced by the Security-framework Keychain seam.
enum LegacyKeychainError: Error, Equatable {
    case unexpectedStatus(OSStatus)
}

/// Reads the legacy `OIDCSession` from, in order:
///
///  1. the file-backed `~/.config/memory-connector/session.json` (identity
///     independent, so preferred);
///  2. the legacy Keychain account `oidc.session` (same JSON shape), then the
///     older split `oidc.access` (+ `oidc.accessExpiry`, `oidc.refresh`,
///     `oidc.id`) with the issuer/client id mirrored in `UserDefaults`.
///
/// `UserDefaults` is not `Sendable`, so this is an `@unchecked Sendable` class;
/// it performs only thread-safe reads and owns no mutable state.
final class DefaultLegacySessionReader: LegacySessionReading, @unchecked Sendable {
    static let sessionAccount = "oidc.session"
    static let accessAccount = "oidc.access"
    static let accessExpiryAccount = "oidc.accessExpiry"
    static let refreshAccount = "oidc.refresh"
    static let idAccount = "oidc.id"

    static let issuerKey = "connector.oidc.issuer"
    static let clientIDKey = "connector.oidc.clientId"

    private let defaults: UserDefaults
    private let secrets: AppSecretStore
    private let keychain: LegacyKeychainReading

    init(defaults: UserDefaults = .standard,
         secrets: AppSecretStore = AppSecretStore(),
         keychain: LegacyKeychainReading = SystemLegacyKeychain()) {
        self.defaults = defaults
        self.secrets = secrets
        self.keychain = keychain
    }

    func read() -> LegacySession? {
        if let data = secrets.readData(AppSecretStore.sessionFileName),
           let stored = try? JSONDecoder().decode(StoredLegacySession.self, from: data) {
            return stored.legacy
        }
        return readKeychain()
    }

    private func readKeychain() -> LegacySession? {
        if let json = try? keychain.load(account: Self.sessionAccount),
           let data = json.data(using: .utf8),
           let stored = try? JSONDecoder().decode(StoredLegacySession.self, from: data) {
            return stored.legacy
        }

        guard let accessToken = try? keychain.load(account: Self.accessAccount),
              !accessToken.isEmpty else {
            return nil
        }
        let expiresAt = (try? keychain.load(account: Self.accessExpiryAccount))
            .flatMap { TimeInterval($0) }
            .map { Date(timeIntervalSince1970: $0) }
        return LegacySession(
            accessToken: accessToken,
            refreshToken: Self.nonEmpty(try? keychain.load(account: Self.refreshAccount)),
            expiresAt: expiresAt,
            issuer: Self.nonEmpty(defaults.string(forKey: Self.issuerKey)),
            clientID: Self.nonEmpty(defaults.string(forKey: Self.clientIDKey))
        )
    }

    private static func nonEmpty(_ value: String?) -> String? {
        guard let value, !value.isEmpty else { return nil }
        return value
    }
}

/// Deletes the legacy session file and Keychain items. Best-effort: every
/// failure is swallowed so a clear can never block migration completion.
struct DefaultLegacySessionClearer: LegacySessionClearing {
    static let legacyAccounts = [
        DefaultLegacySessionReader.sessionAccount,
        DefaultLegacySessionReader.accessAccount,
        DefaultLegacySessionReader.accessExpiryAccount,
        DefaultLegacySessionReader.refreshAccount,
        DefaultLegacySessionReader.idAccount,
    ]

    private let secrets: AppSecretStore
    private let keychain: LegacyKeychainReading

    init(secrets: AppSecretStore = AppSecretStore(),
         keychain: LegacyKeychainReading = SystemLegacyKeychain()) {
        self.secrets = secrets
        self.keychain = keychain
    }

    func clear() {
        secrets.delete(AppSecretStore.sessionFileName)
        for account in Self.legacyAccounts {
            try? keychain.delete(account: account)
        }
    }
}

// MARK: - Legacy wire shape

/// Decodes the old `OIDCSession`. `expiry` is accepted both as a JSON number
/// (Foundation's default `Date` encoding: `timeIntervalSinceReferenceDate`) and
/// as an ISO-8601 string, and every field except `accessToken` is optional.
private struct StoredLegacySession: Decodable {
    var accessToken: String
    var refreshToken: String?
    var idToken: String?
    var expiry: Date?
    var issuer: String?
    var clientID: String?

    enum CodingKeys: String, CodingKey {
        case accessToken, refreshToken, idToken, expiry, issuer, clientID
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        accessToken = try container.decode(String.self, forKey: .accessToken)
        refreshToken = try container.decodeIfPresent(String.self, forKey: .refreshToken)
        idToken = try container.decodeIfPresent(String.self, forKey: .idToken)
        issuer = try container.decodeIfPresent(String.self, forKey: .issuer)
        clientID = try container.decodeIfPresent(String.self, forKey: .clientID)

        if let interval = try? container.decode(Double.self, forKey: .expiry) {
            expiry = Date(timeIntervalSinceReferenceDate: interval)
        } else if let iso = try? container.decode(String.self, forKey: .expiry) {
            expiry = LegacyDateParser.parse(iso)
        } else {
            expiry = nil
        }
    }

    var legacy: LegacySession {
        LegacySession(accessToken: accessToken,
                      refreshToken: refreshToken,
                      expiresAt: expiry,
                      issuer: issuer,
                      clientID: clientID)
    }
}

/// Parses ISO-8601 timestamps with or without fractional seconds.
enum LegacyDateParser {
    static func parse(_ string: String) -> Date? {
        let fractional = ISO8601DateFormatter()
        fractional.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        if let date = fractional.date(from: string) { return date }

        let plain = ISO8601DateFormatter()
        plain.formatOptions = [.withInternetDateTime]
        return plain.date(from: string)
    }
}
