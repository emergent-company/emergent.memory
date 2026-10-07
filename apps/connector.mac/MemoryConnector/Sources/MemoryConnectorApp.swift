import AppKit
import SwiftUI

@main
struct MemoryConnectorApp: App {
    /// Lifecycle glue: the delegate starts the engine + status polling at
    /// launch ONLY when a project is connected, and stops the engine + status
    /// item on quit.
    @NSApplicationDelegateAdaptor(AppDelegate.self) private var appDelegate
    /// Single shared instance graph (SwiftUI + the AppKit status item).
    private let environment = AppEnvironment.shared

    var body: some Scene {
        // Regular app main window (Dock-visible). The menu-bar item is an
        // AppKit `NSStatusItem` (StatusItemController) — no `MenuBarExtra`.
        Window("Memory", id: "main") {
            MainWindowView()
                .environment(environment.settings)
                .environment(environment.accountStore)
                .environment(environment.projectStore)
                .environment(environment.appState)
                .environment(environment.identity)
                .environment(environment.avatarStore)
                .environment(environment.engine)
                .environment(environment.statusMonitor)
                .environment(environment.updater)
                .background(WindowOpenBridge())
                .task { await environment.bootstrap() }
        }
        .commands {
            // Standard application menu: Settings (⌘,) and About work while the
            // app is frontmost. Both route to the existing main window/page.
            CommandGroup(replacing: .appSettings) {
                Button("Settings…") {
                    openMainWindow(page: .connection)
                }
                .keyboardShortcut(",", modifiers: .command)
            }
            CommandGroup(replacing: .appInfo) {
                Button("About Memory") {
                    openMainWindow(page: .about)
                }
            }
        }
    }

    /// Raises the main window (opening it if needed) on the given sidebar page.
    private func openMainWindow(page: SidebarItem) {
        environment.appState.selectedSidebarItem = page
        WindowRouter.shared.requestMainWindow()
        NSApp.activate(ignoringOtherApps: true)
    }
}
