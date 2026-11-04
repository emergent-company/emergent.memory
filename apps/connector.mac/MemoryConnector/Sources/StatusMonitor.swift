import Foundation
import Observation

/// Polls each embedded engine child's own `status --config` subcommand and
/// publishes a per-project snapshot plus an aggregate. Poll cadence: 2s burst
/// while a child freshly started and the hub is not yet connected (up to 20
/// attempts), relaxing to 5s. Polls are skipped entirely when no child is
/// running — no engine process spawns then.
@MainActor
@Observable
final class StatusMonitor {
    static let shared = StatusMonitor()

    /// The aggregate snapshot across all children (drives the menu bar + footer
    /// status derivation).
    private(set) var snapshot: StatusSnapshot?
    /// Per-project snapshots, keyed by project id.
    private(set) var snapshots: [String: StatusSnapshot] = [:]
    private(set) var lastChecked: Date?
    private(set) var isChecking = false

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
                await tick()
                let delay = nextInterval()
                try? await Task.sleep(for: .seconds(delay))
            }
        }
    }

    func stop() {
        task?.cancel()
        task = nil
    }

    /// Publishes the "engine not running" snapshot without starting any
    /// process. Called when every child is intentionally stopped (no connected
    /// project) so the footer/picker never show a stale `connected` snapshot.
    func markStopped() {
        snapshot = StatusSnapshot.engineNotRunning
        snapshots = [:]
        lastChecked = Date()
    }

    /// One-off status check triggered from the UI (Dashboard "Check"), on top
    /// of the polling loop. A no-op while there are no children.
    func refreshNow() {
        let children = EngineManager.shared.children
        guard !children.isEmpty else { return }
        Task { [weak self] in await self?.poll(children) }
    }

    // MARK: - Engine status command

    /// Nonisolated so it can run off the main actor.
    private nonisolated func statusCommand(configPath: String) async -> ProcessResult {
        let engineURL = Bundle.main.url(forResource: "memory-connector", withExtension: nil)
        guard let engineURL else {
            return ProcessResult(stdout: "", exitCode: 1, timedOut: false)
        }
        // Prefer the machine-readable contract; if the engine does not support
        // `--json` (older binary), fall back to the legacy text form.
        let json = await ProcessRunner.run(
            executable: engineURL,
            arguments: ["status", "--json", "--config", configPath],
            timeout: Self.statusTimeout
        )
        if json.exitCode == 0 {
            return json
        }
        return await ProcessRunner.run(
            executable: engineURL,
            arguments: ["status", "--config", configPath],
            timeout: Self.statusTimeout
        )
    }

    private func tick() async {
        let children = EngineManager.shared.children
        guard !children.isEmpty else {
            burstStart = nil
            burstAttempts = 0
            snapshot = StatusSnapshot.engineNotRunning
            snapshots = [:]
            lastChecked = Date()
            return
        }
        // A fresh engine start resets the burst window.
        if burstStart == nil {
            burstStart = Date()
            burstAttempts = 0
        }
        burstAttempts += 1
        await poll(children)
    }

    private func poll(_ children: [String: ChildSupervisor]) async {
        isChecking = true
        defer { isChecking = false }
        var perProject: [String: StatusSnapshot] = [:]
        for (projectID, child) in children.sorted(by: { $0.key < $1.key }) {
            let result = await statusCommand(configPath: child.configPath)
            perProject[projectID] = StatusSnapshot.parse(stdout: result.stdout, exitCode: result.exitCode)
        }
        snapshots = perProject
        snapshot = Self.aggregate(perProject)
        lastChecked = Date()
    }

    /// 2s while bursting (a child up, hub not yet connected), else 5s.
    private func nextInterval() -> TimeInterval {
        if let burstStart,
           burstAttempts < Self.maxBurstAttempts,
           Date().timeIntervalSince(burstStart) < 40,
           snapshot?.hubState != .connected
        {
            return Self.burstInterval
        }
        return Self.pollInterval
    }

    // MARK: - Aggregate

    /// Merges per-project snapshots into a single representative snapshot: the
    /// union of tool names, an aggregate hub state, and a count-based hub line.
    /// Returns `nil` for an empty set (callers publish `engineNotRunning`
    /// instead).
    nonisolated static func aggregate(_ snapshots: [String: StatusSnapshot]) -> StatusSnapshot? {
        guard !snapshots.isEmpty else { return nil }
        let values = snapshots.values
        let toolNames = Set(values.flatMap(\.toolNames)).sorted()
        let states = values.map(\.hubState)
        let hubState = aggregateHubState(states)
        let connected = states.filter { $0 == .connected }.count
        let total = values.count
        let hubLine = connected == total
            ? "\(total) \(total == 1 ? "project" : "projects") connected"
            : "\(connected) of \(total) \(total == 1 ? "project" : "projects") connected"
        let disabledTools = values.flatMap(\.disabledTools)
        let instanceID = total == 1 ? (values.first?.instanceID ?? "") : ""
        let version = values.first?.version ?? ""
        return StatusSnapshot(instanceID: instanceID,
                              version: version,
                              toolNames: toolNames,
                              hubLine: hubLine,
                              hubState: hubState,
                              disabledTools: disabledTools)
    }

    /// Aggregate hub state: connected when every child is connected; otherwise
    /// the most severe non-connected state (auth failure, then unreachable,
    /// then missing config, then plain not-connected).
    nonisolated static func aggregateHubState(_ states: [StatusSnapshot.HubState]) -> StatusSnapshot.HubState {
        guard !states.isEmpty else { return .unknown }
        if states.allSatisfy({ $0 == .connected }) {
            return .connected
        }
        if states.contains(.authFailed) {
            return .authFailed
        }
        if states.contains(.unreachable) {
            return .unreachable
        }
        if states.contains(.missingConfig) {
            return .missingConfig
        }
        return .notConnected
    }
}
