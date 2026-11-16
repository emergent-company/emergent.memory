@testable import MemoryConnector
import XCTest

/// Classification of a failed project operation into what it means for the
/// connection that was being served.
///
/// The launch reconcile keeps a connection (its project token, its engine
/// child) unless the failure is one that reconnecting cannot fix, so these
/// cases are the contract for "never tear down a connection over a blip".
final class ProjectFailureTests: XCTestCase {
    // MARK: - Status codes

    func testNotFoundIsPermanent() {
        XCTAssertEqual(ProjectFailure.classifyMessage("[404] not_found: project not found"), .permanent)
    }

    func testForbiddenIsPermanent() {
        XCTAssertEqual(ProjectFailure.classifyMessage("[403] forbidden: no access to this project"), .permanent)
    }

    func testGoneIsPermanent() {
        XCTAssertEqual(ProjectFailure.classifyMessage("[410] gone: project was deleted"), .permanent)
    }

    func testUnauthorizedIsAuth() {
        XCTAssertEqual(ProjectFailure.classifyMessage("[401] unauthorized: token expired"), .auth)
    }

    func testOtherClientErrorIsPermanent() {
        // A bad request or a revoked scope is not fixed by trying again.
        XCTAssertEqual(ProjectFailure.classifyMessage("[400] bad_request: malformed project id"), .permanent)
    }

    func testServerErrorIsTransient() {
        XCTAssertEqual(ProjectFailure.classifyMessage("[500] internal: boom"), .transient)
        XCTAssertEqual(ProjectFailure.classifyMessage("[503] unavailable: try later"), .transient)
    }

    func testRateLimitAndTimeoutStatusesAreTransient() {
        XCTAssertEqual(ProjectFailure.classifyMessage("[429] rate_limited"), .transient)
        XCTAssertEqual(ProjectFailure.classifyMessage("[408] request_timeout"), .transient)
        XCTAssertEqual(ProjectFailure.classifyMessage("[425] too_early"), .transient)
    }

    func testStatusPrefixVariantsAreRecognised() {
        XCTAssertEqual(ProjectFailure.classifyMessage("status 503: service unavailable"), .transient)
        XCTAssertEqual(ProjectFailure.classifyMessage("HTTP 404: not found"), .permanent)
    }

    func testBracketedProjectNameDoesNotShadowTheRealStatus() {
        // A bracketed project name (with `401` in it) must not decide anything:
        // the status that follows is the real one.
        XCTAssertEqual(ProjectFailure.classifyMessage(
            "[Work 401 Audit] create failed: [404] not_found: project not found"
        ), .permanent)
        XCTAssertEqual(ProjectFailure.classifyMessage(
            "[401 Reasons] create failed: [503] unavailable"
        ), .transient)
    }

    func testStatusDigitsInsideAProjectNameAreNotAuth() {
        // No recognisable status token at all, so the documented default applies
        // (keep the connection and retry) — never `.auth`.
        XCTAssertEqual(ProjectFailure.classifyMessage("[Work 401 Audit] create failed"), .transient)
        XCTAssertEqual(ProjectFailure.classifyMessage("project 401-audit is unreachable"), .transient)
    }

    // MARK: - Transport text (no status code)

    func testTransportFailureIsTransient() {
        XCTAssertEqual(ProjectFailure.classifyMessage("dial tcp 127.0.0.1:8090: connection refused"), .transient)
        XCTAssertEqual(ProjectFailure.classifyMessage("i/o timeout"), .transient)
        XCTAssertEqual(ProjectFailure.classifyMessage("no such host"), .transient)
    }

    func testPermanentTextSignalsArePermanent() {
        XCTAssertEqual(ProjectFailure.classifyMessage("no project matching \"p1\""), .permanent)
        XCTAssertEqual(ProjectFailure.classifyMessage("insufficient permissions"), .permanent)
    }

    func testUnknownFailureDefaultsToTransient() {
        // The documented trade: keeping a connection costs a retry, while
        // clearing one discards the user's decision and a running engine.
        XCTAssertEqual(ProjectFailure.classifyMessage("boom"), .transient)
        XCTAssertEqual(ProjectFailure.classifyMessage(""), .transient)
    }

    // MARK: - Full classify (status consulted before message)

    private let signedInStatus = #"{"schema_version":1,"server":"https://api.example.test","signed_in":true}"#
    private let needsReauthStatus = #"{"schema_version":1,"server":"https://api.example.test","signed_in":true,"needs_reauth":true}"#
    /// The exact bytes `auth status --json` emits for an expired session with no
    /// refresh token (`apps/connector.linux`, pinned by
    /// `TestAuthStatusWireShapeForExpiredSessionWithoutRefreshToken`). Copied from
    /// the document rather than hand-written: the previous fixture added
    /// `"can_refresh":false` to a shape the CLI never emitted, which hid the fact
    /// that the classifier could not see it.
    private let expiredNoRefreshStatus = #"{"schema_version":1,"server":"https://api.example.test","signed_in":true,"expires_at":"2026-01-02T03:04:05Z","expired":true,"can_refresh":false}"#
    /// The shape an older CLI emits for the same session (`can_refresh` tagged
    /// `omitempty`, so the false value was dropped) and a session that never
    /// recorded an expiry: absence must mean the same as false.
    private let expiredNoRefreshLegacyStatus = #"{"schema_version":1,"signed_in":true,"expired":true}"#
    private let expiredRefreshableStatus = #"{"schema_version":1,"server":"https://api.example.test","signed_in":true,"expires_at":"2026-01-02T03:04:05Z","expired":true,"can_refresh":true}"#

    private func classify(_ message: String, status: String?) async -> ProjectFailure {
        let stub = StubCLI()
        if let status {
            stub.on("auth status", json: status)
        }
        let error = ConnectorCLIError.commandFailed(command: "projects use", exitCode: 1, message: message)
        return await ProjectFailure.classify(error,
                                             cli: stub.cli,
                                             serverURL: "https://api.example.test",
                                             configPath: "/tmp/config.yml")
    }

    func testSignedInSessionWithPermanentStatusIsPermanent() async {
        let failure = await classify("[404] not_found: project not found", status: signedInStatus)
        XCTAssertEqual(failure, .permanent)
    }

    func testRejectedSessionWinsOverPermanentStatus() async {
        // The documented trade: while the session needs re-auth, ANY project
        // failure classifies as auth, so a stale session can never tear a
        // connection down.
        let failure = await classify("[404] not_found: project not found", status: needsReauthStatus)
        XCTAssertEqual(failure, .auth)
    }

    func testExpiredSessionThatCannotRefreshIsAuth() async {
        // Expired with no refresh token: retrying can never renew it, so it must
        // land on the sign-in affordance instead of looping as transient.
        let failure = await classify("[404] not_found: project not found", status: expiredNoRefreshStatus)
        XCTAssertEqual(failure, .auth)
    }

    func testExpiredSessionWithOmittedCanRefreshIsAuth() async {
        // An older CLI omits `can_refresh` when it is false (and a session with
        // no recorded expiry omits `expires_at`): absent must mean the same as
        // false, or the branch is unreachable against that CLI.
        let failure = await classify("[404] not_found: project not found",
                                     status: expiredNoRefreshLegacyStatus)
        XCTAssertEqual(failure, .auth)
    }

    func testAProjectNamedLikeAStatusCarriesNoStatus() {
        // A project named exactly `[404]` has no detail text after the bracket,
        // so it is not the server SDK's `[<status>] <detail>` shape; the
        // connection is kept rather than disconnected permanently.
        XCTAssertEqual(ProjectFailure.classifyMessage("[404]"), .transient)
        XCTAssertEqual(ProjectFailure.classifyMessage(#"project "[401]" failed"#), .transient)
    }

    func testExpiredSessionThatCanRefreshKeepsTheProjectVerdict() async {
        // A renewable token is not an auth failure: a dead project is still
        // disconnected rather than hidden behind a sign-in prompt.
        let failure = await classify("[404] not_found: project not found", status: expiredRefreshableStatus)
        XCTAssertEqual(failure, .permanent)
    }

    func testAuthTextShortCircuitsTheStatusRead() async {
        let stub = StubCLI()
        stub.on("auth status", json: signedInStatus)
        let error = ConnectorCLIError.commandFailed(command: "projects use", exitCode: 1,
                                                    message: "not signed in — run 'memory-connector auth login' first")

        let failure = await ProjectFailure.classify(error,
                                                    cli: stub.cli,
                                                    serverURL: "https://api.example.test",
                                                    configPath: "/tmp/config.yml")

        XCTAssertEqual(failure, .auth)
        XCTAssertFalse(stub.called("auth status"),
                       "the text already answered it, so no extra process is spawned")
    }

    func testUnreadableStatusFallsBackToTheMessage() async {
        // No `auth status` stub: the CLI is unreachable, so there is no status
        // to consult and the message has to decide.
        let failure = await classify("[503] unavailable: try later", status: nil)
        XCTAssertEqual(failure, .transient)
    }
}
