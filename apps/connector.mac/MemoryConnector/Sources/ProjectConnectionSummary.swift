import Foundation

/// What the main-window footer reports about the project connected to the
/// Memory relay. The relay serves at most one project, so the reported count is
/// always 0 or 1.
///
/// Deliberately free of SwiftUI so the mapping can be unit-tested without a UI
/// stack; the view maps `dot` to a `Color`. A connected-project id can outlive a
/// sign-out, so it is only honoured while the user is actually signed in — a
/// stale id must not read as connected.
enum ProjectConnectionSummary: Equatable {
    /// Signed in, with a project connected to the relay.
    case connected
    /// Signed out (even with a leftover connected id), or signed in with no
    /// project connected.
    case noProjects

    init(signedIn: Bool, hasConnectedProject: Bool) {
        self = (signedIn && hasConnectedProject) ? .connected : .noProjects
    }

    /// Footer dot state, mapped to a SwiftUI `Color` by the view.
    enum Dot: Equatable {
        case green
        case orange
    }

    var dot: Dot {
        switch self {
        case .connected: .green
        case .noProjects: .orange
        }
    }

    var label: LocalizedStringResource {
        switch self {
        case .connected: "1 project connected"
        case .noProjects: "no projects connected"
        }
    }
}
