import AppKit
import AuthenticationServices
import Foundation

/// Browser-based OAuth authenticator seam (real: `ASWebAuthenticationSession`;
/// tests inject a fake that returns a crafted callback URL).
protocol BrowserAuthenticator: Sendable {
    /// Opens `url` in the system browser and resolves with the callback URL
    /// delivered to `callbackURLScheme`. Throws `OIDCError.cancelled` when the
    /// user cancels.
    func authenticate(url: URL, callbackURLScheme: String) async throws -> URL
}

/// Real `ASWebAuthenticationSession` implementation.
///
/// Concurrency: the session completion handler runs on a background XPC queue,
/// so it is a nonisolated `@Sendable` closure that only resumes the
/// continuation and clears a Sendable holder — it never touches MainActor
/// state (doing so traps in Swift 6's executor check). Session setup and
/// `start()` happen on the main actor, where AppKit requires them.
final class WebAuthenticator: NSObject, BrowserAuthenticator, @unchecked Sendable {

    /// Retains the live session + its anchor provider until the callback
    /// arrives (ASWebAuthenticationSession is not guaranteed to keep itself
    /// alive). Sendable so the callback can clear it off-main.
    private final class SessionHolder: @unchecked Sendable {
        private let lock = NSLock()
        private var session: ASWebAuthenticationSession?
        private var provider: AnchorProvider?

        func set(session: ASWebAuthenticationSession, provider: AnchorProvider) {
            lock.lock()
            self.session = session
            self.provider = provider
            lock.unlock()
        }

        func clear() {
            lock.lock()
            session = nil
            provider = nil
            lock.unlock()
        }
    }

    /// Thread-safe presentation-anchor provider. The anchor window is captured
    /// on the main actor before `start()`; `presentationAnchor(for:)` is
    /// nonisolated and just returns it, so no MainActor assertion can fire from
    /// a framework thread.
    private final class AnchorProvider: NSObject, ASWebAuthenticationPresentationContextProviding, @unchecked Sendable {
        private let lock = NSLock()
        private var window: NSWindow?

        func setWindow(_ window: NSWindow) {
            lock.lock()
            self.window = window
            lock.unlock()
        }

        nonisolated func presentationAnchor(for session: ASWebAuthenticationSession) -> ASPresentationAnchor {
            lock.lock()
            defer { lock.unlock() }
            return window ?? NSWindow()
        }
    }

    nonisolated func authenticate(url: URL, callbackURLScheme: String) async throws -> URL {
        try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<URL, Error>) in
            let holder = SessionHolder()

            // Runs on a background queue — Sendable values only.
            let completion: @Sendable (URL?, Error?) -> Void = { callbackURL, error in
                holder.clear()
                if let error {
                    let nsError = error as NSError
                    if nsError.code == ASWebAuthenticationSessionError.canceledLogin.rawValue {
                        continuation.resume(throwing: OIDCError.cancelled)
                    } else {
                        continuation.resume(throwing: OIDCError.invalidResponse(error.localizedDescription))
                    }
                } else if let callbackURL {
                    continuation.resume(returning: callbackURL)
                } else {
                    continuation.resume(throwing: OIDCError.invalidResponse("empty authentication callback"))
                }
            }

            // AppKit requires creating/starting the session on the main thread.
            Task { @MainActor in
                let provider = AnchorProvider()
                provider.setWindow(NSApp.keyWindow ?? NSApp.windows.first ?? NSWindow())

                let session = ASWebAuthenticationSession(url: url,
                                                         callbackURLScheme: callbackURLScheme,
                                                         completionHandler: completion)
                session.presentationContextProvider = provider
                session.prefersEphemeralWebBrowserSession = false
                holder.set(session: session, provider: provider)

                if !session.start() {
                    holder.clear()
                    continuation.resume(throwing: OIDCError.invalidResponse("could not start the authentication session"))
                }
            }
        }
    }
}

/// Shared OAuth callback constants + query parsing used by the CLI-backed
/// stores. The connector CLI owns the OAuth flow; the app only opens the
/// authorization URL and relays the custom-scheme callback.
enum OAuthCallback {
    nonisolated static let callbackScheme = "com.emergent.memory.connector"
    nonisolated static let redirectURI = "com.emergent.memory.connector://callback"
    nonisolated static let postLogoutRedirectURI = "com.emergent.memory.connector://logout"

    /// Parses the callback URL's query into a dictionary (last value wins).
    nonisolated static func queryItems(from url: URL) -> [String: String] {
        guard let components = URLComponents(url: url, resolvingAgainstBaseURL: false),
              let queryItems = components.queryItems else {
            return [:]
        }
        return Dictionary(queryItems.compactMap { item in
            item.value.map { (item.name, $0) }
        }, uniquingKeysWith: { _, second in second })
    }
}

/// Errors surfaced by the sign-in flow.
enum OIDCError: LocalizedError, Equatable, Sendable {
    case discoveryFailed(String)
    case missingEndpoints
    case httpStatus(Int)
    case invalidResponse(String)
    case stateMismatch
    case notSignedIn
    case cancelled

    var errorDescription: String? {
        switch self {
        case .discoveryFailed(let detail):
            return "OIDC discovery failed: \(detail)"
        case .missingEndpoints:
            return "The OIDC provider document is missing required endpoints."
        case .httpStatus(let code):
            return "The identity provider returned HTTP \(code)."
        case .invalidResponse(let detail):
            return "Unexpected identity provider response: \(detail)"
        case .stateMismatch:
            return "Sign-in failed: the authorization state did not match."
        case .notSignedIn:
            return "Not signed in."
        case .cancelled:
            return "Sign-in was cancelled."
        }
    }
}

/// `GET userinfo` response (subset; unknown keys ignored). Retained because
/// `AccountStore` exposes the captured identity to the UI/avatar layer.
struct OIDCUserInfo: Decodable, Equatable, Sendable {
    let sub: String?
    let name: String?
    let email: String?
    let preferredUsername: String?

    enum CodingKeys: String, CodingKey {
        case sub
        case name
        case email
        case preferredUsername = "preferred_username"
    }
}
