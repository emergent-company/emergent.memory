import Foundation

/// Accessible names for controls whose visible title is empty or icon-only.
///
/// A tooltip (`help`) is not a reliable VoiceOver name on macOS, so every
/// empty-title control supplies an `accessibilityLabel` built from here. The
/// names are pure functions (or constants) so they can be unit-tested.
enum AccessibleNames {
    /// The icon-only account control in the window toolbar.
    static let account: LocalizedStringResource = "Account"

    /// The "All local tools" master switch on the MCP Tools page.
    static let allLocalTools: LocalizedStringResource = "All local tools"

    /// The per-project connection switch on the Dashboard and Project & Account.
    static func projectConnection(_ projectName: String?) -> LocalizedStringResource {
        let name = projectName?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        return name.isEmpty ? "Connect project" : "Connect \(name)"
    }

    /// A hosted MCP server's enable switch on the servers page.
    static func serverEnabled(_ name: String) -> LocalizedStringResource {
        let subject = name.trimmingCharacters(in: .whitespacesAndNewlines)
        return subject.isEmpty ? "Enable server" : "Enable \(subject)"
    }
}
