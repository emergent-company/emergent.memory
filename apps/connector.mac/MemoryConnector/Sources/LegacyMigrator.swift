import Foundation

/// One-time migration of a legacy app-owned OIDC session into the connector
/// CLI's config, via `memory-connector auth import` (session JSON on stdin).
///
/// Best-effort and non-blocking: the method never throws. A successful import
/// clears the legacy source and records a done flag; any failure leaves the flag
/// unset so the next launch retries.
///
/// `UserDefaults` is not `Sendable`, so this is an `@unchecked Sendable` class;
/// only thread-safe `UserDefaults` and injected `Sendable` collaborators are
/// touched.
final class LegacyMigrator: @unchecked Sendable {
    static let migrationDoneKey = "connector.legacyMigration.done"

    private let cli: ConnectorCLI
    private let serverURLProvider: @Sendable () -> String?
    private let reader: LegacySessionReading
    private let clearer: LegacySessionClearing
    private let defaults: UserDefaults
    private let configPath: String

    init(cli: ConnectorCLI,
         serverURLProvider: @escaping @Sendable () -> String?,
         reader: LegacySessionReading,
         clearer: LegacySessionClearing,
         defaults: UserDefaults = .standard,
         configPath: String = EngineManager.defaultConfigPath) {
        self.cli = cli
        self.serverURLProvider = serverURLProvider
        self.reader = reader
        self.clearer = clearer
        self.defaults = defaults
        self.configPath = configPath
    }

    /// Convenience for a fixed server URL (tests, simple call sites).
    convenience init(cli: ConnectorCLI,
                     serverURL: String?,
                     reader: LegacySessionReading,
                     clearer: LegacySessionClearing,
                     defaults: UserDefaults = .standard,
                     configPath: String = EngineManager.defaultConfigPath) {
        self.init(cli: cli,
                  serverURLProvider: { serverURL },
                  reader: reader,
                  clearer: clearer,
                  defaults: defaults,
                  configPath: configPath)
    }

    func migrateIfNeeded() async {
        if defaults.bool(forKey: Self.migrationDoneKey) { return }

        guard let legacy = reader.read() else {
            setDone()
            return
        }
        let server = serverURLProvider()?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        guard !server.isEmpty else {
            setDone()
            return
        }

        // Already signed in? Trust the CLI as the source of truth and skip the
        // import. A thrown error means "cannot tell": leave the flag unset and
        // retry next launch rather than risk clobbering a live session.
        do {
            let status = try await cli.authStatus(serverURL: server, configPath: configPath)
            if status.signedIn {
                setDone()
                return
            }
        } catch {
            return
        }

        guard let body = Self.sessionJSON(from: legacy) else { return }
        do {
            _ = try await cli.authImport(serverURL: server, sessionJSON: body, configPath: configPath)
            clearer.clear()
            setDone()
        } catch {
            // Leave the flag unset so the next launch retries.
        }
    }

    /// Builds the snake_case `auth import` stdin document. Optional fields are
    /// omitted when absent/empty so the CLI applies its own defaults.
    static func sessionJSON(from session: LegacySession) -> Data? {
        var payload: [String: String] = ["access_token": session.accessToken]
        if let refresh = session.refreshToken, !refresh.isEmpty {
            payload["refresh_token"] = refresh
        }
        if let expiresAt = session.expiresAt {
            let formatter = ISO8601DateFormatter()
            formatter.formatOptions = [.withInternetDateTime]
            payload["expires_at"] = formatter.string(from: expiresAt)
        }
        if let issuer = session.issuer, !issuer.isEmpty {
            payload["issuer"] = issuer
        }
        if let clientID = session.clientID, !clientID.isEmpty {
            payload["client_id"] = clientID
        }
        return try? JSONSerialization.data(withJSONObject: payload, options: [.sortedKeys])
    }

    private func setDone() {
        defaults.set(true, forKey: Self.migrationDoneKey)
    }
}
