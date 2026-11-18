@testable import MemoryConnector
import XCTest

/// Episode lifetime of the one-shot silent-re-auth guard: the record of an
/// attempt must die with the episode that produced it.
///
/// The case the guard has to survive is a cycle that ended WITHOUT a silent
/// success — a declined attempt, then a working session (an interactive sign-in,
/// or the stored session recovering). Clearing only the mark leaves the attempt
/// record behind, and because `ensureConnectorSession` marks the account *after*
/// a failed attempt, every later episode arrives already marked and is never
/// retried for the rest of the process. For the long-lived menu-bar app this
/// behaviour exists for, that is the difference between one silent retry and
/// none at all.
///
/// It lives in its own file because `AccountStoreSilentReauthTests.swift` is at
/// SwiftLint's `file_length` limit; that file's fixtures are shared for this one.
extension AccountStoreSilentReauthTests {
    @MainActor
    func testAnEndedEpisodeDoesNotDisableTheNextAttempt() async throws {
        let (store, account) = try await makeSignedInStore()

        // 1. One attempt, declined: the account and its episode are both marked.
        stubNeedsReauthStatus()
        fakeAuth.handler = { _ in .failure(OIDCError.cancelled) }
        let declined = await store.ensureConnectorSession()
        XCTAssertFalse(declined)
        XCTAssertEqual(calls("auth start").count, 1)

        // 2. The episode ends on a working session, with no silent success.
        stubSignedInStatus()
        let recovered = await store.ensureConnectorSession()
        XCTAssertTrue(recovered)
        XCTAssertFalse(store.needsReauthentication(account.id))

        // 3. A later rejection marks the account WITHOUT an attempt in this
        //    episode: the access token expired and the refresh is rejected.
        cli.on("auth status", json: """
        {"schema_version":1,"signed_in":true,"expired":true,"can_refresh":true}
        """)
        cli.on("auth refresh", json: """
        {"schema_version":1,"server_url":"\(prodServer)","refreshed":false,\
        "can_refresh":false,"needs_reauth":true}
        """)
        let rejected = await store.ensureConnectorSession()
        XCTAssertFalse(rejected)
        XCTAssertTrue(store.needsReauthentication(account.id))

        // 4. This episode gets its own one attempt, and it succeeds.
        stubNeedsReauthStatus()
        stubComplete()
        let callback = try XCTUnwrap(URL(string: "\(OAuthCallback.redirectURI)?code=code-1&state=\(state)"))
        fakeAuth.handler = { _ in .success(callback) }

        let signedIn = await store.ensureConnectorSession()

        XCTAssertTrue(signedIn)
        XCTAssertEqual(calls("auth start").count, 2,
                       "an attempt record that outlives its episode disables every later one")
        XCTAssertFalse(store.needsReauthentication(account.id))
        XCTAssertEqual(store.sessionState, .valid)
    }
}
