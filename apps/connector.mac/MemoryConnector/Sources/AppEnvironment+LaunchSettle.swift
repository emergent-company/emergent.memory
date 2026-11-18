import AppKit
import Foundation

/// The single launch/wake settle: session → connection → data → engine.
///
/// Split from `AppEnvironment.swift` so neither file outgrows the SwiftLint
/// file/type limits; the settle's own bookkeeping (`startupFlight`,
/// `settleFlight`, `wakeObserver`) stays on the type because an extension cannot
/// add stored properties.
extension AppEnvironment {
    /// The single launch path: engine → session → connection → data → engine.
    ///
    /// Called by the app delegate (the one place guaranteed to run on a
    /// menu-bar launch) and by the main window's `.task`; whichever gets there
    /// first does the work and the other joins the same run, so a launch
    /// settles exactly once. The flight is cleared when the run ends, so a later
    /// trigger (the window being reopened) settles and refreshes again.
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
        await startupFlight.run { [weak self] in
            guard let self else { return }
            await runStartup()
        }
    }

    private func runStartup() async {
        // 0. Engine first, from the config already on disk: a slow, wedged or
        // timed-out CLI must never keep a previously-working LOCAL engine down
        // while the settle below retries (3 attempts can take ~4.7 min). The
        // children are gated on the config binding the connected project, so a
        // stale config still cannot start.
        syncEngineWithConnections()
        guard accountStore.isSignedIn else { return }
        // 1. Session: renew (or silently re-auth) before anything reads a token.
        await accountStore.ensureConnectorSession()
        // 2. Connection: re-assert every connected project, bounded retries.
        await settleConnection()
        // 3. Data, then the engine set (the session is already settled above).
        await reloadActiveAccountData(reconcileEngine: false, settleSession: false)
        // Reconcile again: the settle may have disconnected a project whose
        // failure was permanent, or the reload may have changed which configs
        // still bind.
        syncEngineWithConnections()
        // 4. Identity is cosmetic and idempotent: fill it in last.
        await accountStore.backfillIdentityIfNeeded()
    }

    /// A machine that slept past its access token's life needs the same settle a
    /// cold launch gets.
    ///
    /// A wake during launch joins the launch instead of settling again: it is
    /// already doing the same work, and settling twice would double the CLI calls
    /// and the retry count. A launch arriving during a wake shares the connection
    /// settle through `settleFlight`, which is the part that must never overlap.
    func wakeReconcile() async {
        if startupFlight.isRunning {
            await startupFlight.join()
            return
        }
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
        await settleFlight.run { [weak self] in
            guard let self else { return .noneConnected }
            let outcome = await reassertRetryPolicy.run { [projectStore] in
                await projectStore.reassertConnection()
            }
            ConnectorLog.lifecycle("connectionSettle outcome=\(outcome.rawValue)")
            return outcome
        }
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
