import Foundation
@testable import Memory
import Testing

/// What a failed connection says to the user.
///
/// A deployment with no voice stack answers the token endpoint with `503`
/// (`web-ui/gateway/token.go` fails closed before it reads the credential). That
/// is a configuration fact, not an outage, so it must not read as a retryable
/// token failure — the same tap would get the same `503`
/// (`emergent-company/emergent.memory#86`).
@MainActor
struct MemoryFailureTests {
    private func classify(_ error: any Error) -> MemoryFailure {
        MemorySessionController(config: MemoryConfig()).classify(error)
    }

    @Test func aTokenConfiguration503IsNotARetryableTokenFailure() {
        let failure = classify(GatewayError.httpStatus(
            503,
            "Token server returned HTTP 503: token service not configured"
        ))

        #expect(failure == .voiceNotConfigured)
        #expect(failure.isRetryable == false)
        #expect(failure.errorDescription?.contains("Text chat works without it") == true)
    }

    /// Only the configuration status is special-cased: a rejected or expired
    /// credential is still a token failure the user can retry after re-scanning.
    @Test func aRejectedCredentialStaysARetryableTokenFailure() {
        let failure = classify(GatewayError.httpStatus(
            401,
            "Token server returned HTTP 401: invalid credential"
        ))

        #expect(failure == .tokenFetch(message: "Token server returned HTTP 401: invalid credential"))
        #expect(failure.isRetryable)
    }

    @Test func everyOtherFailureKeepsARetry() {
        let failures: [MemoryFailure] = [
            .tokenFetch(message: "boom"),
            .serverUnreachable(message: "boom"),
            .agentNotJoined(agentName: "alfred"),
            .micPermissionDenied(agentName: "alfred"),
            .unknown(message: "boom"),
        ]
        // Per case rather than `allSatisfy`: the `#expect` macro cannot expand a
        // `rethrows` call taking a key path, and a failure here should name the
        // case that lost its retry.
        for failure in failures {
            #expect(failure.isRetryable, "\(failure) should keep its retry")
        }
    }
}
