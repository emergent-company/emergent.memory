import Foundation

/// Account lookup, per-account secret directories, the persisted account index,
/// and the small pure helpers the rest of the store builds on.
///
/// The store itself is declared in `AccountStore.swift`; these members are
/// statements about the account list, not lifecycle, so they live in their own
/// extension file.
extension AccountStore {
    // MARK: - Derived accessors

    var activeAccount: Account? {
        guard let activeAccountID else { return nil }
        return account(id: activeAccountID)
    }

    var activeEnvironment: Environment? {
        activeAccount?.environment
    }

    /// True when the NON-secret index has an active account. This can outlive
    /// the session, so it must not drive signed-in UI — use
    /// `isEffectivelySignedIn` for that.
    var isSignedIn: Bool {
        activeAccountID != nil
    }

    /// The identity captured for the active account, when known (in-memory; the
    /// account index is the fallback after a relaunch).
    var activeIdentity: OIDCUserInfo? {
        activeAccountID.flatMap { identities[$0] }
    }

    func account(id: String) -> Account? {
        accounts.first { $0.id == id }
    }

    func environment(for accountID: String) -> Environment? {
        account(id: accountID)?.environment
    }

    func identity(for accountID: String) -> OIDCUserInfo? {
        identities[accountID]
    }

    func needsReauthentication(_ accountID: String) -> Bool {
        needsReauth.contains(accountID)
    }

    // MARK: - Per-account storage

    /// The 0600 secret directory for an account.
    func accountDirectory(for accountID: String) -> URL {
        rootDirectory
            .appendingPathComponent(Self.accountsDirectoryName, isDirectory: true)
            .appendingPathComponent(Self.directoryName(for: accountID), isDirectory: true)
    }

    /// `AppSecretStore` rooted at the account's own directory.
    func secrets(for accountID: String) -> AppSecretStore {
        AppSecretStore(baseDirectory: accountDirectory(for: accountID))
    }

    /// The `ProjectStoreScope` that binds `ProjectStore` to this account's
    /// profiles, tokens, environment and active-project key.
    func projectScope(for accountID: String) -> ProjectStoreScope? {
        guard let account = account(id: accountID),
              let environment = account.environment else { return nil }
        return ProjectStoreScope.account(account, environment: environment, rootDirectory: rootDirectory)
    }

    // MARK: - Index persistence

    func upsert(_ account: Account) {
        if let index = accounts.firstIndex(where: { $0.id == account.id }) {
            accounts[index] = account
        } else {
            accounts.append(account)
        }
    }

    func persistIndex() {
        let index = AccountsIndex(accounts: accounts)
        guard let data = try? JSONEncoder().encode(index) else { return }
        try? indexStore.writeData(data, to: Self.accountsIndexFileName)
    }

    nonisolated static func loadIndex(indexStore: AppSecretStore) -> [Account] {
        guard let data = indexStore.readData(accountsIndexFileName),
              let index = try? JSONDecoder().decode(AccountsIndex.self, from: data)
        else {
            return []
        }
        return index.accounts
    }

    static func resolveActiveID(accounts: [Account], defaults: UserDefaults) -> String? {
        if let id = defaults.string(forKey: activeAccountIDKey),
           accounts.contains(where: { $0.id == id })
        {
            return id
        }
        return accounts.first?.id
    }

    nonisolated static func directoryName(for accountID: String) -> String {
        accountID.replacingOccurrences(of: "/", with: "_")
    }

    // MARK: - Identity

    /// `"<environmentID>:<email>"` for the CLI sign-in path (the CLI status
    /// carries only an email). Falls back to `"unknown"` when absent.
    nonisolated static func accountID(environment: Environment, email: String?) -> String {
        let suffix = email?.nilIfBlank ?? "unknown"
        return "\(environment.id):\(suffix)"
    }

    // MARK: - Errors

    /// Maps connector CLI failures onto the `OIDCError` vocabulary the app
    /// already surfaces; other errors (including `OIDCError`) pass through.
    nonisolated static func oidcError(from error: Error) -> OIDCError {
        if let oidc = error as? OIDCError {
            return oidc
        }
        if let cli = error as? ConnectorCLIError {
            switch cli {
            case .binaryNotFound:
                return .invalidResponse("the bundled memory-connector CLI was not found")
            case let .commandFailed(_, _, message):
                return .invalidResponse(message)
            case let .decodingFailed(_, message):
                return .invalidResponse(message)
            }
        }
        return .invalidResponse(error.localizedDescription)
    }
}

extension String {
    /// The trimmed string, or nil when blank.
    var nilIfBlank: String? {
        let trimmed = trimmingCharacters(in: .whitespacesAndNewlines)
        return trimmed.isEmpty ? nil : trimmed
    }
}
