import AppKit
import AuthenticationServices
import Foundation

/// Browser-based OAuth authenticator seam (real: `ASWebAuthenticationSession`;
/// tests inject a fake that returns a crafted callback URL).
protocol BrowserAuthenticator: Sendable {
    /// Opens `url` in the system browser and resolves with the callback URL
    /// delivered to `callbackURLScheme`. Throws `OIDCError.cancelled` when the
    /// user cancels. No time limit: the user is expected to interact.
    func authenticate(url: URL, callbackURLScheme: String) async throws -> URL
    /// Same, but gives up after `timeout`, tears the browser session down and
    /// reports `OIDCError.cancelled` — the clean decline a silent
    /// (`prompt=none`) attempt expects.
    func authenticate(url: URL, callbackURLScheme: String, timeout: Duration) async throws -> URL
}

extension BrowserAuthenticator {
    /// Default for authenticators with no cancellation hook (tests): the
    /// timeout is advisory and the underlying call decides when to stop.
    func authenticate(url: URL, callbackURLScheme: String, timeout _: Duration) async throws -> URL {
        try await authenticate(url: url, callbackURLScheme: callbackURLScheme)
    }
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

        nonisolated func presentationAnchor(for _: ASWebAuthenticationSession) -> ASPresentationAnchor {
            lock.lock()
            defer { lock.unlock() }
            return window ?? NSWindow()
        }
    }

    /// One in-flight browser round trip: the retained live session, the
    /// continuation, and the single-resume gate. It also keeps `start` down to a
    /// few parameters.
    private final class SessionAttempt: @unchecked Sendable {
        let holder = SessionHolder()
        private let lock = NSLock()
        private var finished = false
        private let continuation: CheckedContinuation<URL, Error>

        init(continuation: CheckedContinuation<URL, Error>) {
            self.continuation = continuation
        }

        /// Keeps the started session (and its presentation anchor) alive until
        /// the attempt ends; ASWebAuthenticationSession does not retain itself.
        func retain(session: ASWebAuthenticationSession, provider: AnchorProvider) {
            holder.set(session: session, provider: provider)
        }

        /// Completes the attempt. The timeout and the session's own completion
        /// handler can both fire, and a second resume traps, so only the first
        /// call does anything.
        func finish(_ result: Result<URL, Error>) {
            holder.clear()
            lock.lock()
            let alreadyFinished = finished
            finished = true
            lock.unlock()
            guard !alreadyFinished else { return }
            continuation.resume(with: result)
        }
    }

    nonisolated func authenticate(url: URL, callbackURLScheme: String) async throws -> URL {
        try await run(url: url, callbackURLScheme: callbackURLScheme, timeout: nil)
    }

    nonisolated func authenticate(url: URL, callbackURLScheme: String, timeout: Duration) async throws -> URL {
        try await run(url: url, callbackURLScheme: callbackURLScheme, timeout: timeout)
    }

    /// Shared implementation. `timeout` nil waits for the user; otherwise the
    /// session is cancelled when it expires and the attempt completes as
    /// `.cancelled`.
    ///
    /// A silent (`prompt=none`) attempt means no login page, not no window: an
    /// IdP that answers with an HTML page instead of a callback would otherwise
    /// leave a window up from a background trigger, so the timeout is what makes
    /// that path safe.
    private nonisolated func run(url: URL, callbackURLScheme: String, timeout: Duration?) async throws -> URL {
        try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<URL, Error>) in
            start(url: url, callbackURLScheme: callbackURLScheme, timeout: timeout,
                  attempt: SessionAttempt(continuation: continuation))
        }
    }

    /// Creates and starts the session on the main actor (an AppKit requirement)
    /// and arms the timeout, which cancels the session so it cannot linger.
    private nonisolated func start(url: URL, callbackURLScheme: String, timeout: Duration?,
                                   attempt: SessionAttempt)
    {
        // The session completion handler runs on a background XPC queue, so it
        // only touches Sendable values.
        let completion: @Sendable (URL?, Error?) -> Void = { callbackURL, error in
            if let error {
                let nsError = error as NSError
                if nsError.code == ASWebAuthenticationSessionError.canceledLogin.rawValue {
                    attempt.finish(.failure(OIDCError.cancelled))
                } else {
                    attempt.finish(.failure(OIDCError.invalidResponse(error.localizedDescription)))
                }
            } else if let callbackURL {
                attempt.finish(.success(callbackURL))
            } else {
                attempt.finish(.failure(OIDCError.invalidResponse("empty authentication callback")))
            }
        }
        Task { @MainActor in
            let provider = AnchorProvider()
            provider.setWindow(NSApp.keyWindow ?? NSApp.windows.first ?? NSWindow())

            let session = ASWebAuthenticationSession(url: url,
                                                     callbackURLScheme: callbackURLScheme,
                                                     completionHandler: completion)
            session.presentationContextProvider = provider
            session.prefersEphemeralWebBrowserSession = false
            attempt.retain(session: session, provider: provider)

            if !session.start() {
                attempt.finish(.failure(OIDCError.invalidResponse("could not start the authentication session")))
                return
            }
            guard let timeout else { return }
            Task { @MainActor in
                try? await Task.sleep(for: timeout)
                session.cancel()
                attempt.finish(.failure(OIDCError.cancelled))
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

    /// How long a silent (`prompt=none`) browser round trip may take before it
    /// is cancelled and treated as a clean decline. "Silent" means no login
    /// page, not necessarily no window: an IdP that answers with HTML instead
    /// of a callback must not leave one up, least of all from a background
    /// launch or wake.
    nonisolated static let silentBrowserTimeout: Duration = .seconds(15)

    /// Parses the callback URL's query into a dictionary (last value wins).
    nonisolated static func queryItems(from url: URL) -> [String: String] {
        guard let components = URLComponents(url: url, resolvingAgainstBaseURL: false),
              let queryItems = components.queryItems
        else {
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
        case let .discoveryFailed(detail):
            "OIDC discovery failed: \(detail)"
        case .missingEndpoints:
            "The OIDC provider document is missing required endpoints."
        case let .httpStatus(code):
            "The identity provider returned HTTP \(code)."
        case let .invalidResponse(detail):
            "Unexpected identity provider response: \(detail)"
        case .stateMismatch:
            "Sign-in failed: the authorization state did not match."
        case .notSignedIn:
            "Not signed in."
        case .cancelled:
            "Sign-in was cancelled."
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
