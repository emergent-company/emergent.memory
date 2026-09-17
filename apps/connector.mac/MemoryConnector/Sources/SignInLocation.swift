import Foundation

/// A surface in the app that can offer a sign-in action.
///
/// Each surface resolves the environments it may offer through
/// `Environment.signInEnvironments(for:)` instead of enumerating environments
/// locally, so "Production everywhere, Development only on About" is one
/// testable rule rather than a convention repeated in every view.
enum SignInLocation: String, CaseIterable {
    /// Top-right account control in the window toolbar.
    case windowHeader
    /// Popover shown on a left-click of the status item.
    case menuBar
    /// Connection settings page's "Memory Account" section.
    case connectionPage
    /// Project & Account page's accounts card.
    case projectAccount
    /// About page — the deliberate developer escape hatch.
    case about
}

extension Environment {
    /// The environment a primary sign-in action targets: Production.
    static let primary: Environment = .prod

    /// The environments a sign-in surface may offer.
    ///
    /// Production is offered everywhere. Development is an internal deployment,
    /// so it is confined to the About page, which acts as the developer escape
    /// hatch — every other surface returns Production only. `[.primary]` is
    /// returned (never `[]`) so a caller can always take `first` as the
    /// action's target.
    ///
    /// This does not remove Development from the app: `Environment.all` still
    /// contains both built-ins and existing Dev accounts keep working.
    static func signInEnvironments(for location: SignInLocation) -> [Environment] {
        switch location {
        case .about:
            return [.primary, .dev]
        default:
            return [.primary]
        }
    }
}
