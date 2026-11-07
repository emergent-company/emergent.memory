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
                .environment(environment.recentProjects)
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

            // Every toolbar control is also a menu command: the toolbar panels
            // can be narrowed into overflow or customised away, and this is the
            // only keyboard-driven path to switching context. Toggles give the
            // system's own checkmark for the active item, the same state the
            // panel rows show.
            CommandMenu("Project") {
                ForEach(Array(environment.projectStore.projects.prefix(9).enumerated()),
                        id: \.element.id)
                { index, project in
                    Toggle(isOn: Binding(
                        get: { project.id == environment.projectStore.activeProjectID },
                        set: { _ in switchProject(project.id) }
                    )) {
                        Text(verbatim: projectName(project))
                    }
                    .keyboardShortcut(KeyEquivalent(Character("\(index + 1)")), modifiers: .command)
                }
                if !environment.projectStore.projects.isEmpty {
                    Divider()
                }
                Button("Manage Projects…") {
                    openMainWindow(page: .project)
                }
                .keyboardShortcut("p", modifiers: [.command, .shift])
                Button("Open in Memory Web") {
                    openWebProject()
                }
                .disabled(activeProjectWebURL == nil)
                Divider()
                Button("Refresh Projects") {
                    refreshProjects()
                }
                .disabled(environment.projectStore.state == .loading)
            }

            CommandMenu("Account") {
                ForEach(environment.accountStore.accounts) { account in
                    Toggle(isOn: Binding(
                        get: { account.id == environment.accountStore.activeAccountID },
                        set: { _ in switchAccount(account.id) }
                    )) {
                        Text(verbatim: account.switcherTitle)
                    }
                }
                if environment.accountStore.accounts.count > 1 {
                    Divider()
                }
                Button("Add Account…") {
                    addAccount()
                }
                .disabled(environment.accountStore.isSigningIn)
                Button("Manage Accounts…") {
                    openMainWindow(page: .project)
                }
                .keyboardShortcut("a", modifiers: [.command, .shift])
                if let active = environment.accountStore.activeAccount {
                    Divider()
                    // A menu command is explicit: it names the account and acts.
                    // The toolbar keeps its confirmation because that control is
                    // one stray click away.
                    Button("Sign Out of \(active.switcherTitle)") {
                        signOut(active)
                    }
                }
            }
        }
    }

    // MARK: - Menu actions

    private func projectName(_ project: ProjectInfo) -> String {
        let name = (project.name ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        return name.isEmpty ? String(localized: "Untitled project") : name
    }

    private func switchProject(_ id: String) {
        Task {
            let token = await (try? environment.accountStore.currentAccessToken()) ?? ""
            await environment.projectStore.selectProject(id, accessToken: token)
            if case .error = environment.projectStore.state {
                return
            }
            environment.recentProjects.record(id)
        }
    }

    private func switchAccount(_ id: String) {
        guard id != environment.accountStore.activeAccountID else { return }
        Task { try? await environment.accountStore.switchTo(accountID: id) }
    }

    private func addAccount() {
        Task {
            let target = Environment.signInEnvironments(for: .windowHeader).first ?? Environment.primary
            guard await (try? environment.accountStore.signIn(environment: target)) != nil else {
                return
            }
            let token = await (try? environment.accountStore.currentAccessToken()) ?? ""
            await environment.projectStore.loadProjects(accessToken: token)
        }
    }

    private func signOut(_ account: Account) {
        Task { await environment.accountStore.signOut(accountID: account.id) }
    }

    private func refreshProjects() {
        Task {
            let token = await (try? environment.accountStore.currentAccessToken()) ?? ""
            await environment.projectStore.loadProjects(accessToken: token)
        }
    }

    private var activeProjectWebURL: URL? {
        guard let project = environment.projectStore.activeProject else { return nil }
        return environment.accountStore.activeAccount?.environment?
            .projectWebURL(orgID: project.orgID, projectID: project.id)
    }

    private func openWebProject() {
        guard let url = activeProjectWebURL else { return }
        NSWorkspace.shared.open(url)
    }

    /// Raises the main window (opening it if needed) on the given sidebar page.
    private func openMainWindow(page: SidebarItem) {
        environment.appState.selectedSidebarItem = page
        WindowRouter.shared.requestMainWindow()
        NSApp.activate(ignoringOtherApps: true)
    }
}
