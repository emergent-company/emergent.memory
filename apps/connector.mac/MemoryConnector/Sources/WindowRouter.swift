import AppKit
import Combine
import SwiftUI

/// Bridges non-SwiftUI reopen requests (Dock click, app activation, the
/// right-click "Settings…" item) into SwiftUI's `openWindow` environment.
@MainActor
final class WindowRouter: ObservableObject {
    static let shared = WindowRouter()

    /// Bumped by `requestMainWindow()`; `WindowOpenBridge` observes it and
    /// opens the main window. Not a boolean so repeated requests always fire.
    @Published private(set) var openRequests = 0

    private init() {}

    func requestMainWindow() {
        openRequests &+= 1
    }
}

/// Invisible view placed inside the main Window's content; it observes
/// `WindowRouter` and calls `openWindow(id: "main")`, since AppKit
/// (`StatusItemController`) cannot call `openWindow` directly.
struct WindowOpenBridge: View {
    @ObservedObject private var router = WindowRouter.shared
    // Fully qualified: the app's own `Environment` value type shadows the
    // SwiftUI property wrapper of the same name.
    @SwiftUI.Environment(\.openWindow) private var openWindow

    var body: some View {
        Color.clear
            .frame(width: 0, height: 0)
            .onChange(of: router.openRequests) { _, _ in
                openWindow(id: "main")
                NSApp.activate(ignoringOtherApps: true)
            }
    }
}
