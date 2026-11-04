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
}
