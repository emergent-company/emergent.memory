import Combine
import Foundation

/// Polls the embedded engine's own `status --config` subcommand and publishes
/// the parsed snapshot. Poll cadence: 2s burst while the engine freshly
/// started and the hub is not yet connected (up to 20 attempts), relaxing to
/// 5s. Polls are skipped entirely when the engine is not running — no engine
/// process spawns then.
@MainActor
final class StatusMonitor: ObservableObject {

    static let shared = StatusMonitor()

    @Published private(set) var snapshot: StatusSnapshot?
    @Published private(set) var lastChecked: Date?
    @Published private(set) var isChecking = false

    private static let pollInterval: TimeInterval = 5
    private static let burstInterval: TimeInterval = 2
    private static let maxBurstAttempts = 20
    private static let statusTimeout: TimeInterval = 10

    private var task: Task<Void, Never>?
    private var burstAttempts = 0
    private var burstStart: Date?

    private init() {}

    /// Starts the polling loop. Idempotent.
    func start() {
        guard task == nil else { return }
        task = Task { [weak self] in
            while !Task.isCancelled {
                guard let self else { return }
                await self.tick()
                let delay = self.nextInterval()
                try? await Task.sleep(for: .seconds(delay))
            }
        }
    }

    func stop() {
        task?.cancel()
        task = nil
    }

    /// Publishes the "engine not running" snapshot without starting any
    /// process. Called when the engine is intentionally stopped (no connected
    /// project) so the footer/picker never show a stale `connected` snapshot.
    func markStopped() {
        snapshot = StatusSnapshot.engineNotRunning
        lastChecked = Date()
    }

    /// One-off status check triggered from the UI (Dashboard "Check"), on top
    /// of the polling loop. A no-op while the engine is not running.
    func refreshNow() {
        guard case .running = EngineManager.shared.state else { return }
        Task { [weak self] in await self?.poll() }
    }

    // MARK: - Engine status command

    /// Nonisolated so it can run off the main actor.
    private nonisolated func statusCommand() async -> ProcessResult {
        let engineURL = Bundle.main.url(forResource: "memory-connector", withExtension: nil)
        guard let engineURL else {
            return ProcessResult(stdout: "", exitCode: 1, timedOut: false)
        }
        // Prefer the machine-readable contract; if the engine does not support
        // `--json` (older binary), fall back to the legacy text form.
        let json = await ProcessRunner.run(
            executable: engineURL,
            arguments: ["status", "--json", "--config", EngineManager.defaultConfigPath],
            timeout: Self.statusTimeout
        )
        if json.exitCode == 0 { return json }
        return await ProcessRunner.run(
            executable: engineURL,
            arguments: ["status", "--config", EngineManager.defaultConfigPath],
            timeout: Self.statusTimeout
        )
    }

    private func tick() async {
        switch EngineManager.shared.state {
        case .running:
            // A fresh engine start resets the burst window.
            if burstStart == nil {
                burstStart = Date()
                burstAttempts = 0
            }
            burstAttempts += 1
            await poll()
        default:
            burstStart = nil
            burstAttempts = 0
            snapshot = StatusSnapshot.engineNotRunning
            lastChecked = Date()
        }
    }

    private func poll() async {
        isChecking = true
        defer { isChecking = false }
        let result = await statusCommand()
        snapshot = StatusSnapshot.parse(stdout: result.stdout, exitCode: result.exitCode)
        lastChecked = Date()
    }

    /// 2s while bursting (engine up, hub not yet connected), else 5s.
    private func nextInterval() -> TimeInterval {
        if let burstStart,
           burstAttempts < Self.maxBurstAttempts,
           Date().timeIntervalSince(burstStart) < 40,
           snapshot?.hubState != .connected {
            return Self.burstInterval
        }
        return Self.pollInterval
    }
}
