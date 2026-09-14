import AppKit

/// App-lifecycle glue: starts the engine once the app is up (ONLY when a
/// project is connected and its config exists) and guarantees it is stopped
/// (SIGTERM) before the app quits, whichever path the user takes (menu Quit,
/// Cmd-Q, system shutdown).
@MainActor
final class AppDelegate: NSObject, NSApplicationDelegate {

    func applicationDidFinishLaunching(_ notification: Notification) {
        // Hosted unit tests run inside the app process — never spawn the
        // engine or status polling there.
        let env = ProcessInfo.processInfo.environment
        let isTesting = env["XCTestConfigurationFilePath"] != nil || env["XCTestBundlePath"] != nil
        guard !isTesting else { return }
        let app = AppEnvironment.shared
        // Gated: start the engine + status polling ONLY for a connected project
        // with a valid config; otherwise stop and publish "not running" so a
        // stale `~/.config/memory-connector.yml` never reconnects.
        app.syncEngineWithConnection()
        app.statusItemController.start()
    }

    func applicationWillTerminate(_ notification: Notification) {
        AppEnvironment.shared.statusItemController.stop()
        EngineManager.shared.stop()
    }

    /// Menu-bar agent: closing the settings window must never quit the app.
    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool {
        false
    }

    /// Dock-icon click / app reopen: surface the main window. If a window is
    /// already available make it key, otherwise ask SwiftUI to open it.
    func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows flag: Bool) -> Bool {
        NSApp.activate(ignoringOtherApps: true)
        if let window = sender.windows.first(where: { $0.canBecomeKey }) {
            window.makeKeyAndOrderFront(nil)
        } else {
            WindowRouter.shared.requestMainWindow()
        }
        return true
    }
}
