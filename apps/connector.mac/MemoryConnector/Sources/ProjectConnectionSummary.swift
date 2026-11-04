import Foundation

/// What the main-window footer reports about the projects connected to the
/// Memory relays. The app can hold several simultaneous project connections, so
/// the reported count is zero or more.
///
/// Deliberately free of SwiftUI so the mapping can be unit-tested without a UI
/// stack; the view maps `dot` to a `Color`. Connected-project ids can outlive a
/// sign-out, so they are only honoured while the user is actually signed in — a
/// stale id must not read as connected.
enum ProjectConnectionSummary: Equatable {
    /// Signed in, with at least one project connected.
    case connected(count: Int)
    /// Signed out (even with leftover connected ids), or signed in with no
    /// project connected.
    case noProjects

    init(signedIn: Bool, connectedCount: Int) {
        self = (signedIn && connectedCount > 0) ? .connected(count: connectedCount) : .noProjects
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

    /// True when at least one project is connected (and the user is signed in).
    var isConnected: Bool {
        if case .connected = self {
            return true
        }
        return false
    }

    var label: LocalizedStringResource {
        switch self {
        case let .connected(count):
            count == 1 ? "1 project connected" : "\(count) projects connected"
        case .noProjects:
            "no projects connected"
        }
    }
}
