import SwiftUI

@main
struct MemoryConnectorApp: App {
    /// Lifecycle glue: the delegate starts the engine + status polling at
    /// launch ONLY when a project is connected, and stops the engine + status
    /// item on quit.
    @NSApplicationDelegateAdaptor(AppDelegate.self) private var appDelegate
    /// Single shared instance graph (SwiftUI + the AppKit status item).
    @StateObject private var environment = AppEnvironment.shared

    var body: some Scene {
        // Regular app main window (Dock-visible). The menu-bar item is an
        // AppKit `NSStatusItem` (StatusItemController) — no `MenuBarExtra`.
        Window("Memory", id: "main") {
            MainWindowView()
                .environmentObject(environment.settings)
                .environmentObject(environment.accountStore)
                .environmentObject(environment.projectStore)
                .environmentObject(environment.appState)
                .environmentObject(environment.identity)
                .environmentObject(environment.engine)
                .environmentObject(environment.statusMonitor)
                .background(WindowOpenBridge())
                .task { await environment.bootstrap() }
        }
    }
}
