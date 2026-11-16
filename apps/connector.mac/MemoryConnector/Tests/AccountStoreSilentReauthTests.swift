@testable import MemoryConnector
import XCTest

/// Silent re-auth: when the provider has rejected the stored session, the app
/// makes ONE non-interactive `prompt=none` attempt (with the account's email as
/// `login_hint`) before it shows a sign-in affordance.
///
/// The two properties that matter, and that a regression would break:
///
/// - a machine with a live SSO session comes back signed in without a login
///   page;
/// - a declined attempt changes nothing — the `needs_reauth` mark and the
///   interactive affordance stay — and never loops (one attempt per episode).
final class AccountStoreSilentReauthTests: XCTestCase {
    private var suiteName = ""
    private var defaults = UserDefaults(suiteName: "") ?? .standard
    private var root = FileManager.default.temporaryDirectory
    private var fakeAuth = FakeAuthenticator()
    private var cli = StubCLI()

    private let prodServer = Environment.prod.serverURLString
    private let loginID = "silent-login"
    private let state = "state-1"

    override func setUpWithError() throws {
        suiteName = "mc-silent-reauth-\(UUID().uuidString)"
        defaults = UserDefaults(suiteName: suiteName) ?? .standard
        root = FileManager.default.temporaryDirectory
            .appendingPathComponent("mc-silent-reauth-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
        fakeAuth = FakeAuthenticator()
        cli = StubCLI()
        stubSignedInStatus()
        stubAccessToken("access-default")
        stubStart()
        stubCancel()
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

    // MARK: - Fixtures

    @MainActor
    private func makeSignedInStore() async throws -> (AccountStore, Account) {
        let account = Account(id: "prod:a@example.test",
                              environmentID: "prod",
                              email: "a@example.test",
                              displayName: nil,
                              avatarObjectKey: nil)
        try AppSecretStore(baseDirectory: root)
            .writeData(JSONEncoder().encode(AccountsIndex(accounts: [account])),
                       to: AccountStore.accountsIndexFileName)
        defaults.set(account.id, forKey: AccountStore.activeAccountIDKey)
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
        let signedIn = await store.reconcileEffectiveSignedInState()
        XCTAssertTrue(signedIn, "the seeded store must come back signed in")
        return (store, account)
    }

    private func stubSignedInStatus() {
        cli.on("auth status") { arguments in
            let server = silentReauthFlag("--server", in: arguments) ?? ""
            return ProcessResult(stdout: """
            {"schema_version":1,"server":"\(server)","signed_in":true}
            """, exitCode: 0, timedOut: false)
        }
    }

    /// The provider rejected the stored session but kept it on disk.
    private func stubNeedsReauthStatus() {
        cli.on("auth status") { arguments in
            let server = silentReauthFlag("--server", in: arguments) ?? ""
            return ProcessResult(stdout: """
            {"schema_version":1,"server":"\(server)","signed_in":true,\
            "expired":true,"can_refresh":true,"needs_reauth":true}
            """, exitCode: 0, timedOut: false)
        }
    }

    private func stubAccessToken(_ token: String) {
        cli.on("auth access-token") { arguments in
            let server = silentReauthFlag("--server", in: arguments) ?? ""
            return ProcessResult(stdout: """
            {"schema_version":1,"server_url":"\(server)","access_token":"\(token)",\
            "expires_at":"2030-01-02T03:04:05Z"}
            """, exitCode: 0, timedOut: false)
        }
    }

    private func stubStart() {
        cli.on("auth start", json: """
        {"schema_version":1,"login_id":"\(loginID)",\
        "authorize_url":"https://auth.example.test/authorize?prompt=none&state=\(state)",\
        "state":"\(state)","expires_at":"2030-01-02T03:04:05Z"}
        """)
    }

    private func stubComplete() {
        cli.on("auth complete", json: """
        {"schema_version":1,"server":"\(prodServer)","signed_in":true,\
        "email":"a@example.test","issuer":"https://auth.example.test",\
        "expires_at":"2030-01-02T03:04:05Z","expired":false}
        """)
    }

    private func stubCancel() {
        cli.on("auth cancel") { _ in ProcessResult(stdout: "", exitCode: 0, timedOut: false) }
    }

    private func calls(_ command: String) -> [[String]] {
        cli.calls.filter { Array($0.prefix(2)).joined(separator: " ") == command }
    }

    // MARK: - Success

    @MainActor
    func testNeedsReauthIsSilentlyRepaired() async throws {
        let (store, account) = try await makeSignedInStore()
        stubNeedsReauthStatus()
        stubComplete()
        let callback = try silentReauthCallbackURL(query: "code=code-1&state=\(state)")
        fakeAuth.handler = { _ in .success(callback) }

        let signedIn = await store.ensureConnectorSession()

        XCTAssertTrue(signedIn, "a live SSO session restores the connector session")
        XCTAssertEqual(store.sessionState, .valid)
        XCTAssertFalse(store.needsReauthentication(account.id))
        XCTAssertNil(store.lastError, "a successful silent re-auth reports no error")
        XCTAssertEqual(calls("auth start").count, 1)
        XCTAssertTrue(calls("auth cancel").isEmpty)
    }

    @MainActor
    func testSilentAttemptUsesPromptNoneAndTheAccountEmail() async throws {
        let (store, _) = try await makeSignedInStore()
        stubNeedsReauthStatus()
        stubComplete()
        let callback = try silentReauthCallbackURL(query: "code=code-1&state=\(state)")
        fakeAuth.handler = { _ in .success(callback) }

        _ = await store.ensureConnectorSession()

        let arguments = try XCTUnwrap(calls("auth start").last)
        XCTAssertEqual(silentReauthFlag("--prompt", in: arguments), "none",
                       "the provider must answer from its own session, never render a login page")
        XCTAssertEqual(silentReauthFlag("--login-hint", in: arguments), "a@example.test",
                       "the hint is what makes prompt=none resolve to this account")
    }

    @MainActor
    func testSuccessfulSilentReauthYieldsAUsableToken() async throws {
        let (store, account) = try await makeSignedInStore()
        stubNeedsReauthStatus()
        stubComplete()
        let callback = try silentReauthCallbackURL(query: "code=code-1&state=\(state)")
        fakeAuth.handler = { _ in .success(callback) }

        _ = await store.ensureConnectorSession()
        let token = try await store.currentAccessToken(for: account.id)

        XCTAssertEqual(token, "access-default")
        XCTAssertFalse(store.needsReauthentication(account.id))
    }

    // MARK: - Declined attempt changes nothing

    @MainActor
    func testDeclinedSilentAttemptKeepsReauthMarkAndSignInAffordance() async throws {
        let (store, account) = try await makeSignedInStore()
        stubNeedsReauthStatus()
        // No SSO session: the browser session cannot complete prompt=none.
        fakeAuth.handler = { _ in .failure(OIDCError.cancelled) }

        let signedIn = await store.ensureConnectorSession()

        XCTAssertFalse(signedIn)
        XCTAssertTrue(store.needsReauthentication(account.id),
                      "a declined attempt leaves the sign-in affordance where it was")
        XCTAssertEqual(store.sessionState, .needsReauth)
        XCTAssertNil(store.lastError, "a background decline is not an error the user caused")
        XCTAssertEqual(calls("auth start").count, 1)
    }

    @MainActor
    func testDeclinedSilentAttemptDiscardsThePendingLogin() async throws {
        let (store, _) = try await makeSignedInStore()
        stubNeedsReauthStatus()
        fakeAuth.handler = { _ in .failure(OIDCError.cancelled) }

        _ = await store.ensureConnectorSession()

        let cancel = try XCTUnwrap(calls("auth cancel").last)
        XCTAssertEqual(silentReauthFlag("--login-id", in: cancel), loginID,
                       "the half-started login is cleaned up, not left pending")
    }

    @MainActor
    func testProviderDeclineWithErrorKeepsReauthMark() async throws {
        let (store, account) = try await makeSignedInStore()
        stubNeedsReauthStatus()
        // The provider's own prompt=none answer: a callback carrying `error`
        // and no code.
        let callback = try silentReauthCallbackURL(query: "error=login_required&state=\(state)")
        fakeAuth.handler = { _ in .success(callback) }

        let signedIn = await store.ensureConnectorSession()

        XCTAssertFalse(signedIn)
        XCTAssertTrue(store.needsReauthentication(account.id))
        XCTAssertEqual(store.sessionState, .needsReauth)
        XCTAssertEqual(calls("auth start").count, 1)
        XCTAssertEqual(calls("auth cancel").count, 1)
    }

    @MainActor
    func testDeclinedAttemptIsNotRetriedWithinTheSameEpisode() async throws {
        let (store, account) = try await makeSignedInStore()
        stubNeedsReauthStatus()
        fakeAuth.handler = { _ in .failure(OIDCError.cancelled) }

        _ = await store.ensureConnectorSession()
        _ = await store.ensureConnectorSession()
        _ = await store.ensureConnectorSession()

        XCTAssertEqual(calls("auth start").count, 1,
                       "a failed attempt must not start a sign-in loop")
        XCTAssertTrue(store.needsReauthentication(account.id))
    }

    // MARK: - Token-read path

    @MainActor
    func testRejectedTokenReadRepairsSilentlyAndRetries() async throws {
        let (store, account) = try await makeSignedInStore()
        let reads = CallCounter()
        cli.on("auth access-token") { arguments in
            reads.increment()
            let server = silentReauthFlag("--server", in: arguments) ?? ""
            if reads.count == 1 {
                // A rejected refresh with no OAuth code: the status is the
                // authority, and it says needs_reauth.
                return ProcessResult(stdout: "memory-connector auth access-token: refresh failed with status 400",
                                     exitCode: 1, timedOut: false)
            }
            return ProcessResult(stdout: """
            {"schema_version":1,"server_url":"\(server)","access_token":"access-default",\
            "expires_at":"2030-01-02T03:04:05Z"}
            """, exitCode: 0, timedOut: false)
        }
        stubNeedsReauthStatus()
        stubComplete()
        let callback = try silentReauthCallbackURL(query: "code=code-1&state=\(state)")
        fakeAuth.handler = { _ in .success(callback) }

        let token = try await store.currentAccessToken(for: account.id)

        XCTAssertEqual(token, "access-default")
        XCTAssertEqual(reads.count, 2, "the read is retried after the repair")
        XCTAssertEqual(calls("auth start").count, 1)
        XCTAssertFalse(store.needsReauthentication(account.id))
        XCTAssertEqual(store.sessionState, .valid)
    }

    @MainActor
    func testRejectedTokenReadWithDeclinedRepairThrowsNotSignedInOnce() async throws {
        let (store, account) = try await makeSignedInStore()
        let reads = CallCounter()
        cli.on("auth access-token") { _ in
            reads.increment()
            return ProcessResult(stdout: "memory-connector auth access-token: refresh failed with status 400",
                                 exitCode: 1, timedOut: false)
        }
        stubNeedsReauthStatus()
        fakeAuth.handler = { _ in .failure(OIDCError.cancelled) }

        for _ in 0 ..< 2 {
            do {
                _ = try await store.currentAccessToken(for: account.id)
                XCTFail("a rejected session with no repair must surface notSignedIn")
            } catch let error as OIDCError {
                XCTAssertEqual(error, .notSignedIn)
            }
        }

        XCTAssertEqual(calls("auth start").count, 1,
                       "the second rejection is the same episode, so it must not attempt again")
        XCTAssertTrue(store.needsReauthentication(account.id))
        XCTAssertNil(store.lastError)
    }
}

/// The background-hygiene cases: a bounded browser wait, and a second episode
/// after a success. They live in an extension because the test type above is at
/// SwiftLint's `type_body_length` limit — an extension is a separate
/// declaration, so it counts on its own (same reason the helpers below are
/// file-scoped).
extension AccountStoreSilentReauthTests {
    /// A silent attempt shows no login page, but that is not the same as showing
    /// no window: an IdP that answers with an HTML page instead of a callback
    /// must not leave one up from a background trigger, so the wait is bounded
    /// and expiring it is the same clean decline as `login_required`.
    @MainActor
    func testSilentAttemptBoundsTheBrowserWait() async throws {
        let (store, account) = try await makeSignedInStore()
        stubNeedsReauthStatus()
        fakeAuth.handler = { _ in .failure(OIDCError.cancelled) }

        let signedIn = await store.ensureConnectorSession()

        XCTAssertFalse(signedIn)
        XCTAssertEqual(fakeAuth.requestedTimeouts, [OAuthCallback.silentBrowserTimeout],
                       "background work must not leave a browser window up indefinitely")
        XCTAssertTrue(store.needsReauthentication(account.id),
                      "expiry is a clean decline: the mark and the affordance stay")
        XCTAssertNil(store.lastError)
        XCTAssertEqual(calls("auth cancel").count, 1, "the pending login is discarded")
    }

    /// A success ends the episode, so the one-shot guard is cleared: the next
    /// rejection is a NEW episode and may attempt again. `fetchAccessToken`
    /// bounds the retried read, so clearing cannot loop the browser.
    @MainActor
    func testSuccessDoesNotDisableALaterEpisode() async throws {
        let (store, account) = try await makeSignedInStore()
        stubNeedsReauthStatus()
        stubComplete()
        let callback = try silentReauthCallbackURL(query: "code=code-1&state=\(state)")
        fakeAuth.handler = { _ in .success(callback) }

        _ = await store.ensureConnectorSession()
        XCTAssertEqual(calls("auth start").count, 1)

        _ = await store.ensureConnectorSession()

        XCTAssertEqual(calls("auth start").count, 2,
                       "a success must not disable silent re-auth for the rest of the session")
        XCTAssertFalse(store.needsReauthentication(account.id))
        XCTAssertEqual(store.sessionState, .valid)
    }
}

/// File-scoped so the test type above stays within SwiftLint's body limit.
private func silentReauthCallbackURL(query: String) throws -> URL {
    try XCTUnwrap(URL(string: "\(OAuthCallback.redirectURI)?\(query)"))
}

private func silentReauthFlag(_ flag: String, in arguments: [String]) -> String? {
    guard let index = arguments.firstIndex(of: flag), arguments.indices.contains(index + 1) else {
        return nil
    }
    return arguments[index + 1]
}
