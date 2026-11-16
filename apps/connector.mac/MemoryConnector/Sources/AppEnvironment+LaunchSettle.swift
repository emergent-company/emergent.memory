import AppKit
import Foundation

/// The single launch/wake settle: session → connection → data → engine.
///
/// Split from `AppEnvironment.swift` so neither file outgrows the SwiftLint
/// file/type limits; the settle's own bookkeeping (`startupTask`, `wakeObserver`)
/// stays on the type because an extension cannot add stored properties.
extension AppEnvironment {
    /// The single launch path: session → connection → data → engine.
    ///
    /// Called by the app delegate (the one place guaranteed to run on a
    /// menu-bar launch) and by the main window's `.task`; whichever gets there
    /// first does the work and the other awaits the same run, so a launch
    /// settles exactly once.
    ///
    /// What each outcome does at launch:
    ///
    /// - session `.valid` → the connection settle runs with a fresh token;
    /// - session `.offline` (CLI missing, killed, timeout) → nothing is signed
    ///   out; the connection settle classifies whatever `projects use` reports;
    /// - session `.needsReauth` → `ensureConnectorSession` makes ONE silent
    ///   `prompt=none` attempt (with `login_hint` = the account email). If the
    ///   provider declines, the `needs_reauth` mark and the interactive sign-in
    ///   affordance stay exactly as before, and the connection settle treats the
    ///   auth failure as "keep every connection" — never a teardown;
    /// - connection `.transientFailure` → retried twice (2 s, then 8 s) and then
    ///   left alone, with the connections, configs and engine intact;
    /// - connection `.permanentFailure` → only that project is disconnected.
    func startup() async {
        if let existing = startupTask {
            await existing.value
            return
        }
        let task = Task { @MainActor [weak self] in
            guard let self else { return }
            await runStartup()
        }
        startupTask = task
        await task.value
    }

    private func runStartup() async {
        guard accountStore.isSignedIn else {
            syncEngineWithConnections()
            return
        }
        // 1. Session: renew (or silently re-auth) before anything reads a token.
        await accountStore.ensureConnectorSession()
        // 2. Connection: re-assert every connected project, bounded retries.
        await settleConnection()
        // 3. Data, then the engine set (the session is already settled above).
        await reloadActiveAccountData(reconcileEngine: false, settleSession: false)
        syncEngineWithConnections()
        // 4. Identity is cosmetic and idempotent: fill it in last.
        await accountStore.backfillIdentityIfNeeded()
    }

    /// A machine that slept past its access token's life needs the same settle a
    /// cold launch gets, without the once-per-launch guard.
    func wakeReconcile() async {
        guard accountStore.isSignedIn else { return }
        await accountStore.ensureConnectorSession()
        await settleConnection()
        syncEngineWithConnections()
    }

    /// Re-asserts every connected project through the injected retry policy and
    /// logs the outcome. Never tears a connection down itself: that decision
    /// belongs to `ProjectStore`, and only a permanent failure takes one down.
    @discardableResult
    private func settleConnection() async -> ProjectStore.ReassertOutcome {
        let outcome = await reassertRetryPolicy.run { [projectStore] in
            await projectStore.reassertConnection()
        }
        ConnectorLog.lifecycle("connectionSettle outcome=\(outcome.rawValue)")
        return outcome
    }

    /// Registers the system-wake settle. Skipped inside hosted/UI tests, where
    /// nothing may reconcile against real state.
    ///
    /// Internal rather than private because `init` (in `AppEnvironment.swift`)
    /// calls it; an extension in another file cannot reach a `private` member.
    func observeWake() {
        guard !HostedTest.isRunning(environment: ProcessInfo.processInfo.environment),
              !UITestMode.isActive
        else {
            return
        }
        wakeObserver = NSWorkspace.shared.notificationCenter.addObserver(
            forName: NSWorkspace.didWakeNotification,
            object: nil,
            queue: .main
        ) { [weak self] _ in
            Task { @MainActor in
                await self?.wakeReconcile()
            }
        }
    }
}
