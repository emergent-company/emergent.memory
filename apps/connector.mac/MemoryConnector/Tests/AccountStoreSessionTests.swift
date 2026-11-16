@testable import MemoryConnector
import XCTest

/// Session behaviour: what keeps a user signed in (a transient failure, a
/// renewal, a cache hit) and what legitimately signs them out (a rejected
/// refresh). Split from `AccountStoreTests` — that file owns the account
/// index/directory/switch behaviour, and neither file should need the other's
/// fixture to grow.
final class AccountStoreSessionTests: XCTestCase {
    private var suiteName = ""
    private var defaults = UserDefaults(suiteName: "") ?? .standard
    private var root = FileManager.default.temporaryDirectory
    private var fakeAuth = FakeAuthenticator()
    private var stop = CallCounter()
    private var cli = StubCLI()

    private let prodServer = Environment.prod.serverURLString

    override func setUpWithError() throws {
        suiteName = "mc-accounts-session-\(UUID().uuidString)"
        defaults = UserDefaults(suiteName: suiteName) ?? .standard
        root = FileManager.default.temporaryDirectory
            .appendingPathComponent("mc-accounts-session-root-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
        fakeAuth = FakeAuthenticator()
        stop = CallCounter()
        cli = StubCLI()
        stubAuthStatus()
        stubAccessToken([prodServer: "access-default"])
        StubURLProtocol.registry.reset()
        ConnectorKeychainCleanup.clearAll(defaults: defaults)
    }

    override func tearDownWithError() throws {
        if !suiteName.isEmpty {
            defaults.removePersistentDomain(forName: suiteName)
        }
        try? FileManager.default.removeItem(at: root)
        StubURLProtocol.registry.reset()
        ConnectorKeychainCleanup.clearAll(defaults: defaults)
    }

    // MARK: - Helpers

    @MainActor
    private func makeStore() -> AccountStore {
        let store = AccountStore(rootDirectory: root,
                                 defaults: defaults,
                                 authenticator: fakeAuth,
                                 memoryClientFactory: { serverURL, token in
                                     MemoryAPIClient(serverURL: serverURL,
                                                     token: token,
                                                     session: StubURLProtocol.makeSession())
                                 })
        store.connectorCLI = cli.cli
        store.configPath = root.appendingPathComponent("memory-connector.yml").path
        store.engineWillStop = { [stop] in stop.increment() }
        return store
    }

    /// A store with one already-signed-in active account, without running the
    /// sign-in flow.
    ///
    /// `signIn` fires a detached identity backfill that reads a token, so any
    /// assertion about how many token reads happened would depend on a task it
    /// did not await. Seeding the index and reconciling reproduces a relaunch
    /// over an existing session — which is the state these tests describe.
    @MainActor
    private func makeSignedInStore(id: String = "prod:a@example.test",
                                   environmentID: String = "prod",
                                   email: String = "a@example.test") async throws -> (AccountStore, Account)
    {
        let account = Account(id: id,
                              environmentID: environmentID,
                              email: email,
                              displayName: nil,
                              avatarObjectKey: nil)
        let index = AccountsIndex(accounts: [account])
        try AppSecretStore(baseDirectory: root)
            .writeData(JSONEncoder().encode(index), to: AccountStore.accountsIndexFileName)
        defaults.set(account.id, forKey: AccountStore.activeAccountIDKey)
        let store = makeStore()
        let signedIn = await store.reconcileEffectiveSignedInState()
        XCTAssertTrue(signedIn, "the seeded store must come back signed in")
        return (store, account)
    }

    /// Number of `auth access-token` invocations the stub has seen.
    private func accessTokenCalls() -> Int {
        cli.calls.filter { Array($0.prefix(2)).joined(separator: " ") == "auth access-token" }.count
    }

    private func stubAuthStatus() {
        cli.on("auth status") { arguments in
            let server = Self.flagValue("--server", in: arguments) ?? ""
            return ProcessResult(stdout: """
            {"schema_version":1,"server":"\(server)","signed_in":true}
            """, exitCode: 0, timedOut: false)
        }
    }

    private func stubAccessToken(_ tokens: [String: String]) {
        cli.on("auth access-token") { arguments in
            let server = Self.flagValue("--server", in: arguments) ?? ""
            guard let token = tokens[server] else {
                return ProcessResult(stdout: "not signed in", exitCode: 1, timedOut: false)
            }
            return ProcessResult(stdout: """
            {"schema_version":1,"server_url":"\(server)","access_token":"\(token)",\
            "expires_at":"2030-01-02T03:04:05Z"}
            """, exitCode: 0, timedOut: false)
        }
    }

    /// `auth status` reporting an expired access token that a refresh token can
    /// still renew.
    private func stubExpiredStatus(needsReauth: Bool = false) {
        cli.on("auth status") { arguments in
            let server = Self.flagValue("--server", in: arguments) ?? ""
            return ProcessResult(stdout: """
            {"schema_version":1,"server":"\(server)","signed_in":true,\
            "expired":true,"can_refresh":true,"needs_reauth":\(needsReauth ? "true" : "false")}
            """, exitCode: 0, timedOut: false)
        }
    }

    private func stubRefresh(fails: Bool = false) {
        cli.on("auth refresh") { arguments in
            let server = Self.flagValue("--server", in: arguments) ?? ""
            if fails {
                return ProcessResult(stdout: "memory-connector auth refresh: refresh failed with status 400",
                                     exitCode: 1, timedOut: false)
            }
            return ProcessResult(stdout: """
            {"schema_version":1,"server_url":"\(server)","refreshed":true,\
            "expires_at":"2030-01-02T03:04:05Z","can_refresh":true,"needs_reauth":false}
            """, exitCode: 0, timedOut: false)
        }
    }

    private static func flagValue(_ flag: String, in arguments: [String]) -> String? {
        guard let index = arguments.firstIndex(of: flag), arguments.indices.contains(index + 1) else {
            return nil
        }
        return arguments[index + 1]
    }

    // MARK: - Stays signed in (transient vs permanent)

    @MainActor
    func testAccessTokenTransientFailureKeepsSignedIn() async throws {
        let (store, account) = try await makeSignedInStore()

        // A CLI/transport failure, not a rejection: the stored refresh token is
        // still there, so the account must NOT be flipped to signed out.
        cli.on("auth access-token") { _ in
            ProcessResult(stdout: "memory-connector auth access-token: dial tcp 127.0.0.1:8090: connection refused",
                          exitCode: 1, timedOut: false)
        }

        do {
            _ = try await store.currentAccessToken(for: account.id)
            XCTFail("the transient failure still surfaces to the caller")
        } catch {
            // expected
        }
        XCTAssertFalse(store.needsReauthentication(account.id))
        XCTAssertTrue(store.isEffectivelySignedIn, "a transient failure must not sign the user out")
        XCTAssertEqual(store.sessionState, .offline)
    }

    @MainActor
    func testAccessTokenRejectionWithoutOAuthCodeAsksTheStatus() async throws {
        let (store, account) = try await makeSignedInStore()

        // A rejected refresh whose text carries no OAuth code ("refresh failed
        // with status 400"): the classification must come from the session
        // status the CLI updated, not from string matching.
        cli.on("auth access-token") { _ in
            ProcessResult(stdout: "memory-connector auth access-token: account: refresh: refresh failed with status 400",
                          exitCode: 1, timedOut: false)
        }
        cli.on("auth status") { arguments in
            let server = Self.flagValue("--server", in: arguments) ?? ""
            return ProcessResult(stdout: """
            {"schema_version":1,"server":"\(server)","signed_in":true,\
            "expired":true,"can_refresh":true,"needs_reauth":true}
            """, exitCode: 0, timedOut: false)
        }

        do {
            _ = try await store.currentAccessToken(for: account.id)
            XCTFail("expected notSignedIn")
        } catch let error as OIDCError {
            XCTAssertEqual(error, .notSignedIn)
        }
        XCTAssertTrue(store.needsReauthentication(account.id))
        XCTAssertFalse(store.isEffectivelySignedIn)
        XCTAssertEqual(store.sessionState, .needsReauth)
    }

    @MainActor
    func testAccessTokenIsCachedWhileItKeepsEnoughLife() async throws {
        let (store, account) = try await makeSignedInStore()

        let first = try await store.currentAccessToken(for: account.id)
        let second = try await store.currentAccessToken(for: account.id)

        XCTAssertEqual(first, "access-default")
        XCTAssertEqual(second, first)
        XCTAssertEqual(accessTokenCalls(), 1,
                       "a burst of reads is served from the cache — each read is a process spawn")
    }

    @MainActor
    func testConcurrentTokenReadsShareOneCLIInvocation() async throws {
        let (store, account) = try await makeSignedInStore()

        let first = Task { @MainActor in try? await store.currentAccessToken(for: account.id) }
        let second = Task { @MainActor in try? await store.currentAccessToken(for: account.id) }
        let firstToken = await first.value
        let secondToken = await second.value

        XCTAssertEqual(firstToken, "access-default")
        XCTAssertEqual(secondToken, "access-default")
        XCTAssertEqual(accessTokenCalls(), 1,
                       "concurrent readers coalesce onto one CLI invocation")
    }

    // MARK: - Session renewal

    @MainActor
    func testEnsureConnectorSessionSilentlyRenewsAnExpiredSession() async throws {
        let (store, account) = try await makeSignedInStore()
        stubExpiredStatus()
        stubRefresh()

        let signedIn = await store.ensureConnectorSession()

        XCTAssertTrue(signedIn, "an expired-but-renewable session comes back signed in")
        XCTAssertTrue(store.isEffectivelySignedIn)
        XCTAssertEqual(store.sessionState, .valid)
        XCTAssertTrue(cli.called("auth refresh"),
                      "the CLI renewed the token instead of asking for a login")
        XCTAssertFalse(store.needsReauthentication(account.id))
    }

    @MainActor
    func testEnsureConnectorSessionRejectedRenewalNeedsReauth() async throws {
        let (store, account) = try await makeSignedInStore()
        let refreshes = CallCounter()
        cli.on("auth status") { arguments in
            let server = Self.flagValue("--server", in: arguments) ?? ""
            // The CLI records needs_reauth on the session when the provider
            // rejects the refresh token.
            let reauth = refreshes.count > 0
            return ProcessResult(stdout: """
            {"schema_version":1,"server":"\(server)","signed_in":true,\
            "expired":true,"can_refresh":true,"needs_reauth":\(reauth ? "true" : "false")}
            """, exitCode: 0, timedOut: false)
        }
        cli.on("auth refresh") { _ in
            refreshes.increment()
            return ProcessResult(stdout: "memory-connector auth refresh: refresh failed with status 400",
                                 exitCode: 1, timedOut: false)
        }

        let signedIn = await store.ensureConnectorSession()

        XCTAssertFalse(signedIn)
        XCTAssertTrue(store.needsReauthentication(account.id))
        XCTAssertFalse(store.isEffectivelySignedIn, "a rejected refresh is the one case that signs the user out")
        XCTAssertEqual(store.sessionState, .needsReauth)
    }

    @MainActor
    func testEnsureConnectorSessionOfflineKeepsSignedIn() async throws {
        let (store, account) = try await makeSignedInStore()
        // The CLI cannot be reached at all (killed process, timeout).
        cli.on("auth status") { _ in
            ProcessResult(stdout: "binary exploded", exitCode: 1, timedOut: false)
        }

        let signedIn = await store.ensureConnectorSession()

        XCTAssertFalse(signedIn)
        XCTAssertTrue(store.isEffectivelySignedIn, "an unreachable CLI is not a sign-out")
        XCTAssertFalse(store.needsReauthentication(account.id))
        XCTAssertEqual(store.sessionState, .offline)
    }
}
