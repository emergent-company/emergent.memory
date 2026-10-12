import AppKit

/// App-lifecycle glue: starts the engine once the app is up (ONLY when a
/// project is connected and its config exists) and guarantees it is stopped
/// (SIGTERM) before the app quits, whichever path the user takes (menu Quit,
/// Cmd-Q, system shutdown).
@MainActor
final class AppDelegate: NSObject, NSApplicationDelegate {
    func applicationDidFinishLaunching(_: Notification) {
        // Hosted unit tests run inside the app process — never spawn the
        // engine or status polling there.
        let env = ProcessInfo.processInfo.environment
        guard !HostedTest.isRunning(environment: env) else { return }
        // A UI-test launch runs the app as a separate process (the hosted-test
        // check is false there); it must never start the engine or the status
        // poller. The isolated profile already yields no connected projects, so
        // this guard is belt-and-suspenders that also skips the status item.
        guard !UITestMode.isActive else { return }
        let app = AppEnvironment.shared
        // Gated: start engine children + status polling ONLY for connected
        // projects with valid configs; otherwise stop and publish "not running"
        // so a stale `~/.config/memory-connector.yml` never reconnects.
        app.syncEngineWithConnections()
        app.statusItemController.start()
    }

    func applicationWillTerminate(_: Notification) {
        AppEnvironment.shared.statusItemController.stop()
        EngineManager.shared.stopAll()
    }

    /// Menu-bar agent: closing the settings window must never quit the app.
    func applicationShouldTerminateAfterLastWindowClosed(_: NSApplication) -> Bool {
        false
    }

    /// Dock-icon click / app reopen: surface the main window. If a window is
    /// already available make it key, otherwise ask SwiftUI to open it.
    func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows _: Bool) -> Bool {
        NSApp.activate(ignoringOtherApps: true)
        if let window = sender.windows.first(where: { $0.canBecomeKey }) {
            window.makeKeyAndOrderFront(nil)
        } else {
            WindowRouter.shared.requestMainWindow()
        }
        return true
    }
}
