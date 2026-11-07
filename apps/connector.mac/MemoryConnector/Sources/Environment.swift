import Foundation

/// A Memory deployment the connector can sign in to.
///
/// An environment bundles the three values that differ between prod and dev:
/// the Memory server URL used for API calls, the OIDC issuer used for sign-in,
/// and the native OIDC client id registered against that issuer. The callback
/// scheme is deliberately NOT part of the value — both environments share
/// `com.emergent.memory.connector://callback`.
///
/// This is a plain value (`Equatable`/`Hashable`), never persisted: the account
/// index stores only `Account.environmentID`, and the built-ins are resolved
/// back through `Environment.environment(id:)`.
struct Environment: Identifiable, Hashable, Sendable {
    /// Stable identifier: `"prod"` / `"dev"`. Persisted in `Account`.
    let id: String
    /// User-facing name shown next to accounts (e.g. in the account switcher).
    let name: String
    /// Memory API base URL (no trailing slash).
    let serverURL: URL
    /// Web-console base URL. The console shares the API origin in prod and on a
    /// local dev gateway, but the hosted dev deployment serves it from its own
    /// ingress on a sibling host, so a deep link must never assume the API host.
    let consoleURL: URL
    /// OIDC issuer used for discovery + sign-in.
    let issuer: URL
    /// Native (public) OIDC client id registered for this environment.
    let clientID: String

    /// `issuer` as the string the OIDC client expects (discovery trims slashes).
    var issuerString: String {
        issuer.absoluteString
    }

    /// `serverURL` as the string API/settings call sites expect.
    var serverURLString: String {
        serverURL.absoluteString
    }

    /// Canonical web-console URL for a project, mirroring the console's own
    /// scoped routing (`/o/{org}/p/{project}/agents`). Built on `consoleURL`,
    /// not `serverURL`: on the hosted dev deployment the console and the API
    /// are different hosts. `nil` when either id is missing — the console has no
    /// org-less project route.
    func projectWebURL(orgID: String?, projectID: String) -> URL? {
        let org = (orgID ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        let project = projectID.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !org.isEmpty, !project.isEmpty else { return nil }
        return consoleURL
            .appending(path: "o").appending(path: org)
            .appending(path: "p").appending(path: project)
            .appending(path: "agents")
    }

    /// Compact badge label shown next to an account so prod/dev are never
    /// confused in the switcher, account page, or menu-bar popover.
    var shortLabel: String {
        switch id {
        case Environment.dev.id: "Dev"
        default: "Prod"
        }
    }

    /// Hosted production environment.
    ///
    /// Its base URLs (and the dev fallbacks below) are literals, so the
    /// remaining `URL(string:)!` force unwraps in this type operate on
    /// compile-time constants, never on environment input.
    static let prod = Environment(
        id: "prod",
        name: "Memory",
        serverURL: URL(string: "https://memory.emergent-company.ai")!,
        consoleURL: URL(string: "https://memory.emergent-company.ai")!,
        issuer: URL(string: "https://auth.emergent-company.ai")!,
        clientID: "390138006318678019"
    )

    /// Fallback dev deployment: RFC 2606 reserved hosts, so the public source
    /// never bakes in an internal deployment and the entry stays inert until a
    /// dev build sets the overrides in its Xcode scheme or shell.
    private static let devServerFallback = URL(string: "https://api.example.com")!
    private static let devIssuerFallback = URL(string: "https://zitadel.example.com")!
    private static let devClientIDFallback = "390138928478289930"

    /// Reads a dev-only override from a launch-environment dictionary.
    ///
    /// Absent, empty, and whitespace-only values all count as "not set" — a
    /// shell or Xcode scheme commonly exports `KEY=` for a variable that was
    /// never filled in, and `environment[key] ?? fallback` would let that blank
    /// defeat the fallback. A real value has surrounding whitespace trimmed.
    static func devValue(
        _ key: String,
        fallback: String,
        environment: [String: String]
    ) -> String {
        trimmedDevValue(key, environment: environment) ?? fallback
    }

    /// Same rules as `devValue`, but the value must also parse as an absolute
    /// `http`/`https` URL with a host. Anything else (malformed, relative,
    /// another scheme) falls back instead of trapping in `URL(string:)!`.
    static func devURL(
        _ key: String,
        fallback: URL,
        environment: [String: String]
    ) -> URL {
        guard
            let value = trimmedDevValue(key, environment: environment),
            let url = URL(string: value),
            let scheme = url.scheme?.lowercased(),
            scheme == "http" || scheme == "https",
            let host = url.host,
            !host.isEmpty
        else {
            return fallback
        }
        return url
    }

    private static func trimmedDevValue(
        _ key: String,
        environment: [String: String]
    ) -> String? {
        guard let raw = environment[key] else { return nil }
        let trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        return trimmed.isEmpty ? nil : trimmed
    }

    /// Dev console URL for an API base URL: the hosted dev deployment fronts the
    /// console from a sibling host that carries the `admin.` label where the API
    /// carries `api.`, so that label is rewritten. Any other host (a local
    /// gateway serving both, or a bare domain) keeps the API origin.
    static func devConsoleURL(for serverURL: URL) -> URL {
        guard
            var components = URLComponents(url: serverURL, resolvingAgainstBaseURL: false),
            let host = components.host,
            host.lowercased().hasPrefix("api.")
        else {
            return serverURL
        }
        // Rewrite only the label: the rest of the host keeps its own case.
        components.host = "admin." + host.dropFirst("api.".count)
        return components.url ?? serverURL
    }

    /// Builds the Dev entry from a launch environment. A function (rather than
    /// inline in the `static let`) so every value shape is testable without
    /// mutating the process environment.
    static func makeDev(environment: [String: String]) -> Environment {
        let serverURL = devURL(
            "MEMORY_DEV_SERVER_URL",
            fallback: devServerFallback,
            environment: environment
        )
        return Environment(
            id: "dev",
            name: "Memory Dev",
            serverURL: serverURL,
            consoleURL: devURL(
                "MEMORY_DEV_CONSOLE_URL",
                fallback: devConsoleURL(for: serverURL),
                environment: environment
            ),
            issuer: devURL(
                "MEMORY_DEV_ISSUER_URL",
                fallback: devIssuerFallback,
                environment: environment
            ),
            clientID: devValue(
                "MEMORY_DEV_CLIENT_ID",
                fallback: devClientIDFallback,
                environment: environment
            )
        )
    }

    /// Hosted development environment (separate Zitadel provider + client).
    /// Configure it out-of-band with `MEMORY_DEV_SERVER_URL` /
    /// `MEMORY_DEV_ISSUER_URL` (and optionally `MEMORY_DEV_CLIENT_ID`); without
    /// them — or with blank/unparseable values — the entry stays on reserved
    /// example hosts.
    static let dev = makeDev(environment: ProcessInfo.processInfo.environment)

    /// Built-in environments, in display order.
    static let all: [Environment] = [.prod, .dev]

    /// Resolves a built-in environment by its persisted id.
    static func environment(id: String) -> Environment? {
        all.first { $0.id == id }
    }
}
