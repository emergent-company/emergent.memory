import Foundation

/// Shared sign-out confirmation copy.
///
/// Used by the in-window confirmation dialog (`ProjectAccountPage`) and by the
/// menu-bar item's confirmation so both surfaces read identically and a sign-out
/// is always confirmed, whichever path triggered it.
enum SignOutConfirmation {
    static let title: LocalizedStringResource = "Sign out of Memory?"

    static func message(displayTitle: String) -> LocalizedStringResource {
        "This stops the connector if \(displayTitle) is active and clears that account's saved session. Other accounts stay signed in."
    }

    static func confirmButtonTitle(displayTitle: String) -> LocalizedStringResource {
        "Sign out \(displayTitle)"
    }
}
