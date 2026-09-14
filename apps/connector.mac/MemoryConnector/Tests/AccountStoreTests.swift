import XCTest
@testable import MemoryConnector

/// Scripted `ConnectorCLI` for `AccountStore` tests. Routes by the first two
/// arguments ("auth start", "auth complete", "auth access-token", ...) to
/// canned output; never runs a process.
final class StubCLI: @unchecked Sendable {
    typealias Handler = @Sendable ([String]) -> ProcessResult

    /// One `auth import`-style invocation: its argv plus the stdin body.
    struct StdinCall {
        let arguments: [String]
        let body: Data?
    }

    private let lock = NSLock()
    private var handlers: [String: Handler] = [:]
    private var _calls: [[String]] = []
    private var _stdinCalls: [StdinCall] = []

    func on(_ command: String, json: String) {
        on(command) { _ in ProcessResult(stdout: json, exitCode: 0, timedOut: false) }
    }

    func on(_ command: String, _ handler: @escaping Handler) {
        lock.withLock { handlers[command] = handler }
    }

    func fail(_ command: String, message: String, exitCode: Int32 = 1) {
        on(command) { _ in ProcessResult(stdout: message, exitCode: exitCode, timedOut: false) }
    }

    var calls: [[String]] { lock.withLock { _calls } }

    /// Every stdin-piped invocation (currently `auth import`), in order.
    var stdinCalls: [StdinCall] { lock.withLock { _stdinCalls } }

    func called(_ command: String) -> Bool {
        calls.contains { Array($0.prefix(2)).joined(separator: " ") == command }
    }

    var cli: ConnectorCLI {
        ConnectorCLI(binaryURL: URL(fileURLWithPath: "/stub/memory-connector"),
                     runner: { [self] _, arguments, _ in
                         let key = Array(arguments.prefix(2)).joined(separator: " ")
                         lock.withLock { _calls.append(arguments) }
                         let handler = lock.withLock { handlers[key] }
                         return handler?(arguments) ?? ProcessResult(stdout: "", exitCode: 1, timedOut: false)
                     },
                     timeout: 5,
                     stdinRunner: { [self] _, arguments, stdin, _ in
                         let key = Array(arguments.prefix(2)).joined(separator: " ")
                         lock.withLock {
                             _calls.append(arguments)
                             _stdinCalls.append(StdinCall(arguments: arguments, body: stdin))
                         }
                         let handler = lock.withLock { handlers[key] }
                         return handler?(arguments) ?? ProcessResult(stdout: "", exitCode: 1, timedOut: false)
                     })
    }
}

/// Multi-account core: CLI-backed sign-in/token/logout, account index,
/// per-account secret directories, switching, sign-out isolation, identity
/// backfill, and legacy migration.
final class AccountStoreTests: XCTestCase {

    private var suiteName = ""
    private var defaults = UserDefaults(suiteName: "") ?? .standard
    private var root = FileManager.default.temporaryDirectory
    private var fakeAuth = FakeAuthenticator()
    private var stop = CallCounter()
    private var cli = StubCLI()

    private let prodServer = Environment.prod.serverURLString
    private let devServer = Environment.dev.serverURLString

    override func setUpWithError() throws {
        suiteName = "mc-accounts-\(UUID().uuidString)"
        defaults = UserDefaults(suiteName: suiteName) ?? .standard
        root = FileManager.default.temporaryDirectory
            .appendingPathComponent("mc-accounts-root-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
        fakeAuth = FakeAuthenticator()
        stop = CallCounter()
        cli = StubCLI()
        stubStart()
        stubAuthStatus()
        stubLogout()
        stubAccessToken([prodServer: "access-default", devServer: "access-default"])
        StubURLProtocol.registry.reset()
        ConnectorKeychainCleanup.clearAll(defaults: defaults)
    }

    override func tearDownWithError() throws {
        if !suiteName.isEmpty { defaults.removePersistentDomain(forName: suiteName) }
        try? FileManager.default.removeItem(at: root)
        StubURLProtocol.registry.reset()
        ConnectorKeychainCleanup.clearAll(defaults: defaults)
    }

    // MARK: - CLI stubs

    private func stubStart(loginID: String = "login-1", state: String = "state-1") {
        cli.on("auth start", json: """
        {"schema_version":1,"login_id":"\(loginID)",\
        "authorize_url":"https://auth.example.test/authorize?state=\(state)",\
        "state":"\(state)","expires_at":"2030-01-02T03:04:05Z"}
        """)
    }

    private func stubComplete(email: String,
                              server: String? = nil,
                              issuer: String = "https://auth.example.test") {
        cli.on("auth complete", json: """
        {"schema_version":1,"server":"\(server ?? prodServer)","signed_in":true,\
        "email":"\(email)","issuer":"\(issuer)","expires_at":"2030-01-02T03:04:05Z","expired":false}
        """)
    }

    private func stubAuthStatus(signedIn: [String: Bool] = [:]) {
        cli.on("auth status") { arguments in
            let server = Self.flagValue("--server", in: arguments) ?? ""
            let signed = signedIn[server] ?? true
            return ProcessResult(stdout: """
            {"schema_version":1,"server":"\(server)","signed_in":\(signed ? "true" : "false")}
            """, exitCode: 0, timedOut: false)
        }
    }

    private func stubLogout() {
        cli.on("auth logout") { _ in ProcessResult(stdout: "", exitCode: 0, timedOut: false) }
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

    private static func flagValue(_ flag: String, in arguments: [String]) -> String? {
        guard let index = arguments.firstIndex(of: flag), arguments.indices.contains(index + 1) else {
            return nil
        }
        return arguments[index + 1]
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

    private func callback(from authorizeURL: URL, code: String? = "code-1") -> URL {
        let state = OAuthCallback.queryItems(from: authorizeURL)["state"] ?? "missing"
        var components = URLComponents(string: OAuthCallback.redirectURI) ?? URLComponents()
        var items = [URLQueryItem(name: "state", value: state)]
        if let code { items.append(URLQueryItem(name: "code", value: code)) }
        components.queryItems = items
        return components.url ?? URL(fileURLWithPath: "/callback")
    }

    @MainActor
    @discardableResult
    private func signIn(_ store: AccountStore, _ environment: Environment) async throws -> Account {
        fakeAuth.handler = { url in .success(self.callback(from: url)) }
        return try await store.signIn(environment: environment)
    }

    private func indexExists() -> Bool {
        FileManager.default.fileExists(atPath: root.appendingPathComponent("accounts.json").path)
    }

    private func accountDirectory(_ id: String) -> URL {
        root.appendingPathComponent("accounts", isDirectory: true)
            .appendingPathComponent(AccountStore.directoryName(for: id), isDirectory: true)
    }

    // MARK: - Sign in / index

    @MainActor
    func testSignInCreatesAccountViaCLI() async throws {
        stubComplete(email: "a@example.test")
        let store = makeStore()

        let account = try await signIn(store, .prod)

        XCTAssertEqual(account.id, "prod:a@example.test")
        XCTAssertEqual(account.environmentID, "prod")
        XCTAssertEqual(account.email, "a@example.test")
        XCTAssertEqual(store.accounts, [account])
        XCTAssertEqual(store.activeAccountID, "prod:a@example.test")
        XCTAssertEqual(store.activeEnvironment, .prod)
        XCTAssertEqual(defaults.string(forKey: AccountStore.activeAccountIDKey), "prod:a@example.test")

        XCTAssertTrue(cli.called("auth start"), "sign-in starts the CLI login")
        XCTAssertTrue(cli.called("auth complete"), "sign-in completes the CLI login")
        XCTAssertTrue(indexExists())
        let attrs = try FileManager.default.attributesOfItem(atPath: root.appendingPathComponent("accounts.json").path)
        XCTAssertEqual((attrs[.posixPermissions] as? NSNumber)?.intValue, 0o600, "index is 0600")
        let localSession = store.accountDirectory(for: "prod:a@example.test")
            .appendingPathComponent(AppSecretStore.sessionFileName)
        XCTAssertFalse(FileManager.default.fileExists(atPath: localSession.path),
                       "the CLI owns the session; no local session file is written")
    }

    @MainActor
    func testSignInSameAccountTwiceKeepsOneAccount() async throws {
        let store = makeStore()
        stubComplete(email: "a@example.test")
        _ = try await signIn(store, .prod)

        stubComplete(email: "a@example.test")
        _ = try await signIn(store, .prod)

        XCTAssertEqual(store.accounts.count, 1, "same user/env id must upsert, not duplicate")
        XCTAssertEqual(store.accounts.first?.email, "a@example.test")
    }

    @MainActor
    func testSignInStateMismatchThrowsBeforeCompletion() async throws {
        let store = makeStore()
        fakeAuth.handler = { _ in
            var components = URLComponents(string: OAuthCallback.redirectURI) ?? URLComponents()
            components.queryItems = [URLQueryItem(name: "state", value: "wrong"),
                                     URLQueryItem(name: "code", value: "code-1")]
            return .success(components.url ?? URL(fileURLWithPath: "/callback"))
        }

        do {
            _ = try await store.signIn(environment: .prod)
            XCTFail("expected stateMismatch")
        } catch let error as OIDCError {
            XCTAssertEqual(error, .stateMismatch)
        }
        XCTAssertFalse(cli.called("auth complete"), "mismatch aborts before the exchange")
        XCTAssertTrue(store.accounts.isEmpty)
    }

    @MainActor
    func testSignInCLIErrorMapsToOIDCError() async throws {
        cli.fail("auth start", message: "binary exploded")
        let store = makeStore()

        do {
            _ = try await store.signIn(environment: .prod)
            XCTFail("expected an OIDCError")
        } catch let error as OIDCError {
            XCTAssertEqual(error, .invalidResponse("binary exploded"))
        }
        XCTAssertNotNil(store.lastError)
        XCTAssertTrue(store.accounts.isEmpty)
    }

    // MARK: - Connector CLI session bridge

    @MainActor
    func testSignInImportsSessionIntoConnectorCLI() async throws {
        stubComplete(email: "a@example.test")
        stubAccessToken([prodServer: "access-1"])
        cli.on("auth import", json: """
        {"schema_version":1,"server":"\(prodServer)","signed_in":true,"email":"a@example.test"}
        """)
        let store = makeStore()

        _ = try await signIn(store, .prod)

        XCTAssertTrue(cli.called("auth import"), "sign-in bridges the session into the CLI")
        let call = try XCTUnwrap(cli.stdinCalls.last {
            Array($0.arguments.prefix(2)).joined(separator: " ") == "auth import"
        })
        XCTAssertEqual(Self.flagValue("--server", in: call.arguments), prodServer)

        let body = try XCTUnwrap(call.body)
        let json = try XCTUnwrap(JSONSerialization.jsonObject(with: body) as? [String: String])
        XCTAssertEqual(json["access_token"], "access-1")
        XCTAssertEqual(json["email"], "a@example.test")
        XCTAssertEqual(json["issuer"], Environment.prod.issuerString)
        XCTAssertEqual(json["expires_at"], "2030-01-02T03:04:05Z")
    }

    @MainActor
    func testEnsureConnectorSessionRepairsWhenCLIUnsigned() async throws {
        stubComplete(email: "a@example.test")
        stubAccessToken([prodServer: "access-1"])
        let store = makeStore()
        _ = try await signIn(store, .prod)

        // Simulate the wedge: the app is signed in, the CLI reports unsigned,
        // until the bridge import succeeds.
        let imported = CallCounter()
        let signedInJSON = """
        {"schema_version":1,"server":"\(prodServer)","signed_in":true,"email":"a@example.test"}
        """
        cli.on("auth status") { arguments in
            let requested = Self.flagValue("--server", in: arguments) ?? ""
            let signed = imported.count > 0
            return ProcessResult(stdout: """
            {"schema_version":1,"server":"\(requested)","signed_in":\(signed ? "true" : "false")}
            """, exitCode: 0, timedOut: false)
        }
        cli.on("auth import") { _ in
            imported.increment()
            return ProcessResult(stdout: signedInJSON, exitCode: 0, timedOut: false)
        }

        let repaired = await store.ensureConnectorSession()
        XCTAssertTrue(repaired, "the bridge import restores the CLI session")
        XCTAssertEqual(imported.count, 1, "exactly one best-effort import")
    }

    @MainActor
    func testEnsureConnectorSessionFailsWhenNoSessionAvailable() async throws {
        stubComplete(email: "a@example.test")
        let store = makeStore()
        _ = try await signIn(store, .prod)

        stubAuthStatus(signedIn: [prodServer: false])
        // No readable session to bridge: the CLI cannot mint an access token.
        cli.on("auth access-token") { _ in
            ProcessResult(stdout: "not signed in", exitCode: 1, timedOut: false)
        }

        let repaired = await store.ensureConnectorSession()
        XCTAssertFalse(repaired)
    }

    // MARK: - Effective signed-in state

    @MainActor
    func testSignInMarksEffectivelySignedIn() async throws {
        stubComplete(email: "a@example.test")
        let store = makeStore()
        XCTAssertFalse(store.isEffectivelySignedIn)

        _ = try await signIn(store, .prod)

        XCTAssertTrue(store.isEffectivelySignedIn)
        XCTAssertEqual(store.sessionValid["prod:a@example.test"], true)
    }

    @MainActor
    func testReconcileMarksSignedInWhenCLISessionValid() async throws {
        stubComplete(email: "a@example.test")
        let writer = makeStore()
        _ = try await signIn(writer, .prod)

        // Fresh store over the same non-secret index (defaults + accounts.json):
        // the effective state is unknown until reconciled.
        let restored = makeStore()
        XCTAssertTrue(restored.isSignedIn, "index has the account")
        XCTAssertFalse(restored.isEffectivelySignedIn, "not reconciled yet")

        let signedIn = await restored.reconcileEffectiveSignedInState()

        XCTAssertTrue(signedIn)
        XCTAssertTrue(restored.isEffectivelySignedIn)
        XCTAssertEqual(restored.sessionValid["prod:a@example.test"], true)
    }

    @MainActor
    func testReconcileMarksNotSignedInWhenIndexOutlivesCLISession() async throws {
        stubComplete(email: "a@example.test")
        let writer = makeStore()
        _ = try await signIn(writer, .prod)

        // The non-secret index survives, but the connector CLI has no session.
        stubAuthStatus(signedIn: [prodServer: false])
        cli.on("auth access-token") { _ in
            ProcessResult(stdout: "not signed in", exitCode: 1, timedOut: false)
        }
        let restored = makeStore()
        XCTAssertTrue(restored.isSignedIn, "index still lists the account")

        let signedIn = await restored.reconcileEffectiveSignedInState()

        XCTAssertFalse(signedIn)
        XCTAssertFalse(restored.isEffectivelySignedIn, "index present, CLI unsigned")
        XCTAssertEqual(restored.sessionValid["prod:a@example.test"], false)
    }

    @MainActor
    func testEffectiveStateFalseWithNoAccounts() async {
        let store = makeStore()
        XCTAssertFalse(store.isEffectivelySignedIn)

        await store.reconcileEffectiveSignedInState()

        XCTAssertFalse(store.isEffectivelySignedIn)
    }

    @MainActor
    func testSignOutLastAccountClearsEffectiveState() async throws {
        stubComplete(email: "a@example.test")
        let store = makeStore()
        let account = try await signIn(store, .prod)
        XCTAssertTrue(store.isEffectivelySignedIn)

        await store.signOut(accountID: account.id)

        XCTAssertFalse(store.isEffectivelySignedIn)
        XCTAssertNil(store.sessionValid[account.id])
    }

    @MainActor
    func testMarkActiveSessionInvalidClearsEffectiveState() async throws {
        stubComplete(email: "a@example.test")
        let store = makeStore()
        _ = try await signIn(store, .prod)
        XCTAssertTrue(store.isEffectivelySignedIn)

        store.markActiveSessionInvalid()

        XCTAssertFalse(store.isEffectivelySignedIn)
        XCTAssertEqual(store.sessionValid["prod:a@example.test"], false)
    }

    // MARK: - Isolation

    @MainActor
    func testTwoAccountsGetDistinctTokensAndDirectories() async throws {
        let store = makeStore()
        stubComplete(email: "a@example.test")
        let a = try await signIn(store, .prod)
        stubComplete(email: "b@example.test", server: devServer, issuer: Environment.dev.issuerString)
        let b = try await signIn(store, .dev)

        XCTAssertEqual(Set(store.accounts.map(\.id)), ["prod:a@example.test", "dev:b@example.test"])
        XCTAssertEqual(store.activeAccountID, b.id, "newest sign-in becomes active")

        // Distinct directories: no file can be shared. (The directory is
        // created lazily on the next secret write; the CLI owns the session.)
        XCTAssertNotEqual(store.accountDirectory(for: a.id), store.accountDirectory(for: b.id))
        XCTAssertTrue(store.accountDirectory(for: a.id).path.hasSuffix("/accounts/prod:a@example.test"))
        XCTAssertTrue(store.accountDirectory(for: b.id).path.hasSuffix("/accounts/dev:b@example.test"))

        stubAccessToken([prodServer: "access-A", devServer: "access-B"])
        let tokenA = try await store.currentAccessToken(for: a.id)
        let tokenB = try await store.currentAccessToken(for: b.id)
        XCTAssertEqual(tokenA, "access-A")
        XCTAssertEqual(tokenB, "access-B")
    }

    // MARK: - Switching

    @MainActor
    func testSwitchToDisconnectsEngineAndValidatesViaCLI() async throws {
        let store = makeStore()
        stubComplete(email: "a@example.test")
        let a = try await signIn(store, .prod)
        let afterFirst = stop.count

        stubComplete(email: "b@example.test", server: devServer, issuer: Environment.dev.issuerString)
        _ = try await signIn(store, .dev)
        XCTAssertEqual(stop.count, afterFirst + 1, "signing in a second account disconnects the first")

        try await store.switchTo(accountID: a.id)

        XCTAssertEqual(stop.count, afterFirst + 2, "switch stops/disconnects the engine")
        XCTAssertEqual(store.activeAccountID, a.id)
        XCTAssertEqual(defaults.string(forKey: AccountStore.activeAccountIDKey), a.id)
        XCTAssertTrue(cli.called("auth status"), "switch validates through the CLI")
        stubAccessToken([prodServer: "access-A", devServer: "access-B"])
        let activeToken = try await store.currentAccessToken()
        XCTAssertEqual(activeToken, "access-A")
    }

    @MainActor
    func testSwitchToNotSignedInAccountThrows() async throws {
        let store = makeStore()
        stubComplete(email: "a@example.test")
        let a = try await signIn(store, .prod)
        stubComplete(email: "b@example.test", server: devServer, issuer: Environment.dev.issuerString)
        let b = try await signIn(store, .dev)

        stubAuthStatus(signedIn: [prodServer: false])
        do {
            try await store.switchTo(accountID: a.id)
            XCTFail("expected notSignedIn")
        } catch let error as OIDCError {
            XCTAssertEqual(error, .notSignedIn)
        }
        XCTAssertEqual(store.activeAccountID, b.id, "failed switch leaves the active account unchanged")
    }

    @MainActor
    func testSwitchToUnknownAccountThrows() async throws {
        let store = makeStore()
        do {
            try await store.switchTo(accountID: "prod:nobody")
            XCTFail("expected unknownAccount")
        } catch let error as AccountStoreError {
            XCTAssertEqual(error, .unknownAccount("prod:nobody"))
        }
    }

    // MARK: - Sign out

    @MainActor
    func testSignOutCallsCLIAndPromotesAnother() async throws {
        let store = makeStore()
        stubComplete(email: "a@example.test")
        let a = try await signIn(store, .prod)
        stubComplete(email: "b@example.test", server: devServer, issuer: Environment.dev.issuerString)
        let b = try await signIn(store, .dev)
        try await store.switchTo(accountID: a.id)
        let before = stop.count

        await store.signOut(accountID: a.id)

        XCTAssertEqual(stop.count, before + 1, "signing out the active account disconnects")
        XCTAssertEqual(store.accounts.map(\.id), [b.id])
        XCTAssertEqual(store.activeAccountID, b.id, "another account is promoted")
        XCTAssertFalse(FileManager.default.fileExists(atPath: store.accountDirectory(for: a.id).path),
                       "signed-out account's directory is deleted")
        XCTAssertTrue(cli.called("auth logout"))
        XCTAssertTrue(cli.calls.contains { args in
            Array(args.prefix(2)).joined(separator: " ") == "auth logout" && args.contains(prodServer)
        }, "logout is scoped to the removed account's server")

        await store.signOut(accountID: b.id)
        XCTAssertTrue(store.accounts.isEmpty)
        XCTAssertNil(store.activeAccountID)
        XCTAssertNil(defaults.string(forKey: AccountStore.activeAccountIDKey))
    }

    @MainActor
    func testIndexReloadsAccountsAndActive() async throws {
        let first = makeStore()
        stubComplete(email: "a@example.test")
        _ = try await signIn(first, .prod)
        stubComplete(email: "b@example.test", server: devServer, issuer: Environment.dev.issuerString)
        _ = try await signIn(first, .dev)

        let second = makeStore()
        XCTAssertEqual(Set(second.accounts.map(\.id)), ["prod:a@example.test", "dev:b@example.test"])
        XCTAssertEqual(second.activeAccountID, "dev:b@example.test")
        XCTAssertEqual(second.activeEnvironment, .dev)
    }

    // MARK: - Per-account token failure

    @MainActor
    func testAccessTokenFailureMarksOnlyThatAccount() async throws {
        let store = makeStore()
        stubComplete(email: "a@example.test")
        let a = try await signIn(store, .prod)
        stubComplete(email: "b@example.test", server: devServer, issuer: Environment.dev.issuerString)
        let b = try await signIn(store, .dev)

        let devServer = self.devServer
        cli.on("auth access-token") { arguments in
            let server = Self.flagValue("--server", in: arguments) ?? ""
            if server == devServer {
                return ProcessResult(stdout: "refresh rejected", exitCode: 1, timedOut: false)
            }
            return ProcessResult(stdout: """
            {"schema_version":1,"server_url":"\(server)","access_token":"access-A",\
            "expires_at":"2030-01-02T03:04:05Z"}
            """, exitCode: 0, timedOut: false)
        }

        let tokenA = try await store.currentAccessToken(for: a.id)
        XCTAssertEqual(tokenA, "access-A")
        XCTAssertFalse(store.needsReauthentication(a.id))

        do {
            _ = try await store.currentAccessToken(for: b.id)
            XCTFail("expected an access-token failure for B")
        } catch {
            // expected
        }
        XCTAssertTrue(store.needsReauthentication(b.id), "only account B needs re-auth")
        XCTAssertFalse(store.needsReauthentication(a.id), "A is unaffected by B's failure")
    }

    // MARK: - Identity backfill

    @MainActor
    private func seedAccount(id: String,
                             environmentID: String,
                             email: String?,
                             displayName: String?,
                             avatarObjectKey: String? = nil) throws {
        let account = Account(id: id,
                              environmentID: environmentID,
                              email: email,
                              displayName: displayName,
                              avatarObjectKey: avatarObjectKey)
        let index = AccountsIndex(accounts: [account])
        try AppSecretStore(baseDirectory: root).writeData(try JSONEncoder().encode(index),
                                                          to: AccountStore.accountsIndexFileName)
    }

    @MainActor
    func testRefreshIdentityBackfillsAndPersistsMissingFields() async throws {
        try seedAccount(id: "prod:user-X", environmentID: "prod",
                        email: nil, displayName: nil)
        stubAccessToken([prodServer: "access-X"])
        let profile = """
        {"id":"u1","displayName":"Xena Wu","firstName":"Xena","lastName":"Wu",
         "email":"xena@example.test","avatarObjectKey":"avatars/xena.png"}
        """
        StubURLProtocol.registry.setHandler { request in
            switch request.url?.path {
            case "/api/user/profile": return .ok(profile)
            default: return .status(404)
            }
        }
        let store = makeStore()

        await store.refreshIdentity(for: "prod:user-X")

        let account = try XCTUnwrap(store.account(id: "prod:user-X"))
        XCTAssertEqual(account.email, "xena@example.test")
        XCTAssertEqual(account.displayName, "Xena Wu")
        XCTAssertEqual(account.avatarObjectKey, "avatars/xena.png")
        XCTAssertEqual(account.displayTitle, "Xena Wu")

        // Persisted into the index: a reload sees the enriched entry.
        let reloaded = AccountStore.loadIndex(indexStore: AppSecretStore(baseDirectory: root))
        XCTAssertEqual(reloaded.first?.email, "xena@example.test")
        XCTAssertEqual(reloaded.first?.displayName, "Xena Wu")

        // Select this test's own request: background identity tasks can record
        // unrelated requests into the shared stub registry, so match this
        // test's own token as well as the path.
        let request = try XCTUnwrap(StubURLProtocol.registry.capturedRequests
            .last { $0.url?.path == "/api/user/profile"
                && $0.value(forHTTPHeaderField: "Authorization") == "Bearer access-X" })
        XCTAssertEqual(request.httpMethod, "GET")
        XCTAssertEqual(request.url?.path, "/api/user/profile")
        XCTAssertEqual(request.url?.host, Environment.prod.serverURL.host)
        XCTAssertNil(request.value(forHTTPHeaderField: "X-Project-ID"))
    }

    @MainActor
    func testRefreshIdentityFallsBackToAuthMeForEmail() async throws {
        try seedAccount(id: "prod:user-W", environmentID: "prod",
                        email: nil, displayName: "Wade")
        stubAccessToken([prodServer: "access-W"])
        StubURLProtocol.registry.setHandler { request in
            switch request.url?.path {
            case "/api/user/profile": return .status(500)
            case "/api/auth/me": return .ok(#"{"email":"wade@example.test"}"#)
            default: return .status(404)
            }
        }
        let store = makeStore()

        await store.refreshIdentity(for: "prod:user-W")

        XCTAssertEqual(store.account(id: "prod:user-W")?.email, "wade@example.test")
        XCTAssertEqual(store.account(id: "prod:user-W")?.displayName, "Wade")
    }

    @MainActor
    func testRefreshIdentityLeavesAccountUnchangedOnFetchFailure() async throws {
        try seedAccount(id: "prod:user-Y", environmentID: "prod",
                        email: nil, displayName: nil)
        stubAccessToken([prodServer: "access-Y"])
        StubURLProtocol.registry.setHandler { _ in .status(500) }
        let store = makeStore()

        await store.refreshIdentity(for: "prod:user-Y")

        let account = try XCTUnwrap(store.account(id: "prod:user-Y"))
        XCTAssertNil(account.email)
        XCTAssertNil(account.displayName)
        XCTAssertNil(account.avatarObjectKey)
        XCTAssertEqual(account.displayTitle, "Prod", "never a generic 'Signed in' fallback")
        let reloaded = AccountStore.loadIndex(indexStore: AppSecretStore(baseDirectory: root))
        XCTAssertNil(reloaded.first?.email, "failed backfill must not persist")
    }

    @MainActor
    func testRefreshIdentitySkipsNetworkWhenIdentityPresent() async throws {
        try seedAccount(id: "prod:user-Z", environmentID: "prod",
                        email: "z@example.test", displayName: "Zed")
        StubURLProtocol.registry.setHandler { _ in .ok("{}") }
        let store = makeStore()

        await store.refreshIdentity(for: "prod:user-Z")

        XCTAssertTrue(StubURLProtocol.registry.capturedRequests.isEmpty,
                      "no request when identity is already present")
        XCTAssertEqual(store.account(id: "prod:user-Z")?.displayName, "Zed")
    }

    // MARK: - Empty state

    @MainActor
    func testEmptyStoreHasNoAccountsOrIndex() {
        let store = makeStore()
        XCTAssertTrue(store.accounts.isEmpty)
        XCTAssertNil(store.activeAccountID)
        XCTAssertFalse(indexExists())
    }
}
