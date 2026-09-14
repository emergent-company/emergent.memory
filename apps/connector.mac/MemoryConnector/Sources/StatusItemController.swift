import AppKit
import Combine
import SwiftUI

/// AppKit menu-bar item: LEFT-click shows a `MenuBarView` popover, RIGHT-click
/// shows an `NSMenu` (Settings…, Sign out when signed in, Quit). Replaces
/// `MenuBarExtra`, whose label `.contextMenu` is unreliable on recent macOS.
@MainActor
final class StatusItemController: NSObject, ObservableObject {
    private let environment: AppEnvironment
    private var statusItem: NSStatusItem?
    private let popover = NSPopover()
    private var cancellables = Set<AnyCancellable>()

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
        cancellables.removeAll()
        popover.close()
        if let statusItem {
            NSStatusBar.system.removeStatusItem(statusItem)
        }
        statusItem = nil
    }

    // MARK: - Click handling

    @objc private func handleClick(_ sender: Any?) {
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
        MenuBarView()
            .environmentObject(environment.settings)
            .environmentObject(environment.accountStore)
            .environmentObject(environment.projectStore)
            .environmentObject(environment.appState)
            .environmentObject(environment.identity)
            .environmentObject(environment.engine)
            .environmentObject(environment.statusMonitor)
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

        let settings = NSMenuItem(title: "Settings…", action: #selector(openSettings), keyEquivalent: ",")
        settings.target = self
        menu.addItem(settings)

        if environment.accountStore.isEffectivelySignedIn {
            let signOut = NSMenuItem(title: "Sign out", action: #selector(signOut), keyEquivalent: "")
            signOut.target = self
            menu.addItem(signOut)
        }

        menu.addItem(.separator())

        let quit = NSMenuItem(title: "Quit Memory", action: #selector(quit), keyEquivalent: "q")
        quit.target = self
        menu.addItem(quit)
        return menu
    }

    @objc private func openSettings() {
        WindowRouter.shared.requestMainWindow()
        NSApp.activate(ignoringOtherApps: true)
    }

    @objc private func signOut() {
        let accountStore = environment.accountStore
        Task {
            guard let id = accountStore.activeAccountID else { return }
            await accountStore.signOut(accountID: id)
        }
    }

    @objc private func quit() {
        environment.engine.stop()
        environment.projectStore.stopAndClear()
        NSApp.terminate(nil)
    }

    // MARK: - Icon

    private func observeState() {
        let publishers: [ObservableObjectPublisher] = [
            environment.engine.objectWillChange,
            environment.statusMonitor.objectWillChange,
            environment.accountStore.objectWillChange,
            environment.projectStore.objectWillChange,
        ]
        Publishers.MergeMany(publishers)
            .receive(on: RunLoop.main)
            .sink { [weak self] in self?.updateIcon() }
            .store(in: &cancellables)
    }

    private func updateIcon() {
        guard let button = statusItem?.button else { return }
        let status = environment.appStatus
        let image = NSImage(systemSymbolName: status.symbolName, accessibilityDescription: "Memory")
        image?.isTemplate = true
        button.image = image
        button.contentTintColor = NSColor(status.color)
        button.toolTip = status.label
    }
}
