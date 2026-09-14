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
    /// OIDC issuer used for discovery + sign-in.
    let issuer: URL
    /// Native (public) OIDC client id registered for this environment.
    let clientID: String

    /// `issuer` as the string the OIDC client expects (discovery trims slashes).
    var issuerString: String { issuer.absoluteString }

    /// `serverURL` as the string API/settings call sites expect.
    var serverURLString: String { serverURL.absoluteString }

    /// Compact badge label shown next to an account so prod/dev are never
    /// confused in the switcher, account page, or menu-bar popover.
    var shortLabel: String {
        switch id {
        case Environment.dev.id: return "Dev"
        default: return "Prod"
        }
    }

    /// Hosted production environment.
    static let prod = Environment(
        id: "prod",
        name: "Memory",
        serverURL: URL(string: "https://memory.emergent-company.ai")!,
        issuer: URL(string: "https://auth.emergent-company.ai")!,
        clientID: "390138006318678019"
    )

    /// Hosted development environment (separate Zitadel provider + client).
    static let dev = Environment(
        id: "dev",
        name: "Memory Dev",
        serverURL: URL(string: "https://api.dev.emergent-company.ai")!,
        issuer: URL(string: "https://zitadel.dev.emergent-company.ai")!,
        clientID: "390138928478289930"
    )

    /// Built-in environments, in display order.
    static let all: [Environment] = [.prod, .dev]

    /// Resolves a built-in environment by its persisted id.
    static func environment(id: String) -> Environment? {
        all.first { $0.id == id }
    }
}
