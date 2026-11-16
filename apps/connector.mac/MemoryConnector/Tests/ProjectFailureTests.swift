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
