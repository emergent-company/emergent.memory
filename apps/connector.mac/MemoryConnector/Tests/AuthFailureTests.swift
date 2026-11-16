import Foundation
@testable import MemoryConnector
import Testing

/// Unit cases for the shared auth/session-failure classifier, so `ProjectStore`
/// and `AccountStore` stay on the same rules.
struct AuthFailureTests {
    @Test func notSignedInTextIsAuthFailure() {
        #expect(AuthFailure.looksLikeAuthFailure(
            ConnectorCLIError.commandFailed(command: "auth access-token", exitCode: 1, message: "not signed in")
        ))
    }

    @Test func invalidTokenTextIsAuthFailure() {
        #expect(AuthFailure.looksLikeAuthFailure(
            ConnectorCLIError.commandFailed(command: "auth access-token", exitCode: 1, message: "invalid_token")
        ))
    }

    @Test func unauthorizedTextIsAuthFailure() {
        #expect(AuthFailure.looksLikeAuthFailure(
            ConnectorCLIError.commandFailed(command: "projects list", exitCode: 1, message: "unauthorized")
        ))
    }

    @Test func http401StatusIsAuthFailure() {
        #expect(AuthFailure.looksLikeAuthFailure(OIDCError.httpStatus(401)))
    }

    @Test func notSignedInOIDCErrorIsAuthFailure() {
        #expect(AuthFailure.looksLikeAuthFailure(OIDCError.notSignedIn))
    }

    @Test func invalidGrantIsAuthFailure() {
        // A rejected refresh is the one refresh error that no retry can fix:
        // the provider has invalidated the refresh token itself.
        #expect(AuthFailure.looksLikeAuthFailure(
            ConnectorCLIError.commandFailed(command: "auth access-token", exitCode: 1,
                                            message: "refresh failed: invalid_grant - refresh token is invalid")
        ))
    }

    @Test func transientRefreshFailureIsNotAuthFailure() {
        // Transport/status failures leave the stored refresh token usable, so
        // they must not sign the account out.
        #expect(!AuthFailure.looksLikeAuthFailure(
            ConnectorCLIError.commandFailed(command: "auth access-token", exitCode: 1,
                                            message: "refresh failed with status 503")
        ))
    }

    @Test func nonAuthMessageIsNotAuthFailure() {
        #expect(!AuthFailure.looksLikeAuthFailure(
            ConnectorCLIError.commandFailed(command: "projects list", exitCode: 1, message: "boom")
        ))
    }

    @Test func caseInsensitiveMatch() {
        #expect(AuthFailure.looksLikeAuthFailure(
            ConnectorCLIError.commandFailed(command: "auth access-token", exitCode: 1, message: "Not Signed In")
        ))
    }

    @Test func bracketedStatusTokenIsAuthFailure() {
        // The server SDK's shape: `[<status>] <code>: <message>`.
        #expect(AuthFailure.looksLikeAuthFailure(
            ConnectorCLIError.commandFailed(command: "projects use", exitCode: 1,
                                            message: "[401] unauthorized: token expired")
        ))
    }

    @Test func statusTokenTextIsAuthFailure() {
        // `status NNN` is the shape a refresh failure uses.
        #expect(AuthFailure.looksLikeAuthFailure(
            ConnectorCLIError.commandFailed(command: "auth access-token", exitCode: 1,
                                            message: "refresh failed with status 401")
        ))
    }

    @Test func projectNameContaining401IsNotAuthFailure() {
        // The digits alone are not a token: a project name, path or id can
        // contain `401` while the session is perfectly usable.
        #expect(!AuthFailure.looksLikeAuthFailure(
            ConnectorCLIError.commandFailed(command: "projects use", exitCode: 1,
                                            message: "[Work 401 Audit] create failed: [404] not_found: project not found")
        ))
        #expect(!AuthFailure.looksLikeAuthFailure(
            ConnectorCLIError.commandFailed(command: "projects use", exitCode: 1,
                                            message: "project 401-audit could not be reached")
        ))
    }

    @Test func onlyAnchoredStatusShapesAreTokens() {
        // `[NNN]`, `status NNN` and `HTTP NNN` are the emitted shapes; a
        // bracketed name is not one, so the scan continues to the real status.
        #expect(AuthFailure.statusToken(in: "[503] unavailable: try later") == 503)
        #expect(AuthFailure.statusToken(in: "[Work 401 Audit] create failed: [404] not_found") == 404)
        #expect(AuthFailure.statusToken(in: "refresh failed with status 400") == 400)
        #expect(AuthFailure.statusToken(in: "The identity provider returned HTTP 401.") == 401)
        #expect(AuthFailure.statusToken(in: "[404 Reasons] create failed") == nil,
                "a bracketed name that starts with digits is not a status")
        #expect(AuthFailure.statusToken(in: "project 401-audit is unreachable") == nil)
    }
}
