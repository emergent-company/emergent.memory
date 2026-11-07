import AppKit
import Observation
import SwiftUI

/// AppKit menu-bar item: LEFT-click shows a `MenuBarView` popover, RIGHT-click
/// shows an `NSMenu`. Both paths expose the SAME action set (open, check for
/// updates, Settings, sign out, Quit), and sign-out is confirmed on both.
/// Replaces `MenuBarExtra`, whose label `.contextMenu` is unreliable on recent
/// macOS.
@MainActor
final class StatusItemController: NSObject {
    private let environment: AppEnvironment
    private var statusItem: NSStatusItem?
    private let popover = NSPopover()

    init(environment: AppEnvironment) {
        self.environment = environment
        super.init()
        start()
    }

    /// Creates the status item once and begins observing state for the icon.
    func start() {
        guard statusItem == nil else { return }

        let item = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
        if let button = item.button {
            button.target = self
            button.action = #selector(handleClick(_:))
            button.sendAction(on: [.leftMouseUp, .rightMouseUp])
        }
        statusItem = item
        updateIcon()
        observeState()
    }

    /// Removes the status item (called on terminate).
    func stop() {
        popover.close()
        if let statusItem {
            NSStatusBar.system.removeStatusItem(statusItem)
        }
        statusItem = nil
    }

    // MARK: - Click handling

    @objc private func handleClick(_: Any?) {
        if NSApp.currentEvent?.type == .rightMouseUp {
            showRightClickMenu()
        } else {
            togglePopover()
        }
    }

    private func togglePopover() {
        guard let button = statusItem?.button else { return }
        if popover.isShown {
            popover.performClose(nil)
            return
        }
        popover.contentViewController = NSHostingController(rootView: popoverContent())
        popover.behavior = .transient
        NSApp.activate(ignoringOtherApps: true)
        popover.show(relativeTo: button.bounds, of: button, preferredEdge: .minY)
    }

    private func popoverContent() -> some View {
        MenuBarView(
            onSignOut: { [weak self] in self?.confirmSignOutFromMenuBar() },
            onQuit: { [weak self] in self?.quit() }
        )
        .environment(environment.settings)
        .environment(environment.accountStore)
        .environment(environment.projectStore)
        .environment(environment.recentProjects)
        .environment(environment.appState)
        .environment(environment.identity)
        .environment(environment.avatarStore)
        .environment(environment.engine)
        .environment(environment.statusMonitor)
        .environment(environment.updater)
    }

    // MARK: - Right-click menu

    private func showRightClickMenu() {
        guard let statusItem, let button = statusItem.button else { return }
        statusItem.menu = buildMenu()
        button.performClick(nil)
        statusItem.menu = nil
    }

    private func buildMenu() -> NSMenu {
        let menu = NSMenu()

        // Same action set as the popover, so primary and secondary click agree.
        let open = NSMenuItem(title: String(localized: "Open Memory"),
                              action: #selector(openApplication), keyEquivalent: "")
        open.target = self
        menu.addItem(open)

        let updates = NSMenuItem(title: String(localized: "Check for Updates…"),
                                 action: #selector(checkForUpdates), keyEquivalent: "")
        updates.target = self
        updates.isEnabled = environment.updater.canCheckForUpdates
        menu.addItem(updates)

        let settings = NSMenuItem(title: String(localized: "Settings…"),
                                  action: #selector(openSettings), keyEquivalent: ",")
        settings.target = self
        menu.addItem(settings)

        if environment.accountStore.isEffectivelySignedIn {
            menu.addItem(.separator())
            let signOut = NSMenuItem(title: String(localized: "Sign out"),
                                     action: #selector(signOut), keyEquivalent: "")
            signOut.target = self
            menu.addItem(signOut)
        }

        menu.addItem(.separator())

        let quit = NSMenuItem(title: String(localized: "Quit Memory"),
                              action: #selector(quit), keyEquivalent: "q")
        quit.target = self
        menu.addItem(quit)
        return menu
    }

    @objc private func openApplication() {
        WindowRouter.shared.requestMainWindow()
        NSApp.activate(ignoringOtherApps: true)
    }

    @objc private func checkForUpdates() {
        environment.updater.checkForUpdates()
    }

    @objc private func openSettings() {
        environment.appState.selectedSidebarItem = .connection
        WindowRouter.shared.requestMainWindow()
        NSApp.activate(ignoringOtherApps: true)
    }

    @objc private func signOut() {
        confirmSignOutFromMenuBar()
    }

    /// Confirms a menu-bar sign-out before ending the session, using the same
    /// copy as the in-window dialog. Cancelling leaves the session intact.
    func confirmSignOutFromMenuBar() {
        let accountStore = environment.accountStore
        guard let id = accountStore.activeAccountID,
              let account = accountStore.accounts.first(where: { $0.id == id })
        else { return }

        let alert = NSAlert()
        alert.alertStyle = .warning
        alert.messageText = String(localized: SignOutConfirmation.title)
        alert.informativeText = String(
            localized: SignOutConfirmation.message(displayTitle: account.displayTitle)
        )
        alert.addButton(withTitle: String(
            localized: SignOutConfirmation.confirmButtonTitle(displayTitle: account.displayTitle)
        ))
        alert.addButton(withTitle: String(localized: "Cancel"))

        guard alert.runModal() == .alertFirstButtonReturn else { return }
        Task { await accountStore.signOut(accountID: id) }
    }

    @objc private func quit() {
        environment.engine.stopAll()
        environment.projectStore.stopAndClear()
        NSApp.terminate(nil)
    }

    // MARK: - Icon

    private func observeState() {
        observeEngineState()
        observeSnapshot()
    }

    /// The icon derives only from `engine.state` + `statusMonitor.snapshot`
    /// (`AppStatus.derive`), so those are the two properties observed.
    private func observeEngineState() {
        withObservationTracking {
            _ = environment.engine.state
        } onChange: { [weak self] in
            Task { @MainActor in
                guard let self else { return }
                self.updateIcon()
                self.observeEngineState()
            }
        }
    }

    private func observeSnapshot() {
        withObservationTracking {
            _ = environment.statusMonitor.snapshot
        } onChange: { [weak self] in
            Task { @MainActor in
                guard let self else { return }
                self.updateIcon()
                self.observeSnapshot()
            }
        }
    }

    private func updateIcon() {
        guard let button = statusItem?.button else { return }
        let status = environment.appStatus
        let image = NSImage(systemSymbolName: status.symbolName, accessibilityDescription: "Memory")
        image?.isTemplate = true
        button.image = image
        button.contentTintColor = NSColor(status.color)
        button.toolTip = String(localized: status.label)
    }
}
