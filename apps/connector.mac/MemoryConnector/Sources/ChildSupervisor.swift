import Darwin
import Foundation
import Observation

/// Supervises ONE embedded `memory-connector` engine process, bound to a single
/// project's config file and loopback management port.
///
/// This is the per-project unit the multi-project `EngineManager` fans out to:
/// every connected project gets its own `ChildSupervisor`, so one project's
/// crash / expired token / held port / relay outage is isolated from the rest.
/// The spawn/restart/stop/bind-failure/circuit-breaker logic is unchanged from
/// the former single-process `EngineManager`, only parameterized by port and
/// given a per-project log destination.
@MainActor
@Observable
final class ChildSupervisor {
    let projectID: String
    /// Config path handed to `relay --config`.
    let configPath: String
    /// Loopback management port handed to `relay --api-port`.
    let port: Int

    private(set) var state: EngineManager.State = .stopped
    private(set) var restartCount = 0
    private(set) var lastExitCode: Int32?
    private(set) var lastError: String?

    /// The per-project log sink (primary project uses the shared app log).
    private let sink: LogSink
    private var process: Process?
    private var policy = RestartPolicy()
    private var stoppedByUser = false
    /// A configuration error reported while a child is still terminating.
    /// `reportConfigurationError` cannot set `.failed` until the child exits, so
    /// it records the message here and `processDidExit` applies it.
    private var pendingConfigurationError: String?
    /// Set when a restart is requested while a child is live (or a transition is
    /// in flight). `processDidExit` consumes it to spawn the successor only
    /// after the previous child has released the loopback management port.
    private var pendingRestart = false
    /// Whether the queued restart is a genuine configuration-change restart and
    /// must reset the circuit breaker when the successor finally spawns.
    private var pendingRestartResetBreaker = false
    /// Backstop that kills a SIGTERM'd child which does not exit within
    /// ``EngineManager/terminateGracePeriod``, so a queued restart cannot hang
    /// forever.
    private var forceKillTask: Task<Void, Never>?

    init(projectID: String, configPath: String, port: Int, logFileURL: URL) {
        self.projectID = projectID
        self.configPath = configPath
        self.port = port
        sink = LogSink(destination: logFileURL)
    }

    /// True when a child process is currently running.
    var hasLiveProcess: Bool {
        process?.isRunning == true
    }

    // MARK: - Public lifecycle

    /// Locates the embedded engine binary and spawns it as a direct child.
    /// Safe to call from `.stopped`, `.failed`, and `.restarting`.
    ///
    /// - Parameter resetBreaker: when `true`, the restart circuit breaker
    ///   (`policy`/`restartCount`) is reset — reserved for a genuine
    ///   configuration-change restart (`restart()`). The exit-retry and
    ///   reconcile paths pass the default `false` so a crash oscillation can
    ///   still accumulate toward `giveUp`.
    func start(resetBreaker: Bool = false) {
        guard state == .stopped || state == .failed || state == .restarting else { return }
        // Never spawn into a management port a previous child still holds
        // (running or mid-termination). Any live child must exit first; queue
        // the spawn and let `processDidExit` start the successor once the child
        // has released the port.
        if process?.isRunning == true {
            pendingRestart = true
            pendingRestartResetBreaker = pendingRestartResetBreaker || resetBreaker
            state = .restarting
            if let proc = process {
                scheduleForceKillIfNeeded(proc)
            }
            return
        }
        stoppedByUser = false
        let shouldResetBreaker = resetBreaker || pendingRestartResetBreaker
        pendingRestart = false
        pendingRestartResetBreaker = false
        forceKillTask?.cancel()
        forceKillTask = nil

        guard let url = engineURL() else {
            state = .failed
            lastError = "embedded engine binary not found in app bundle"
            return
        }
        guard FileManager.default.isExecutableFile(atPath: url.path) else {
            state = .failed
            lastError = "embedded engine binary is not executable: \(url.path)"
            return
        }

        // Fail fast when the management port is already held by another
        // process. A live child was handled above, so a held port here belongs
        // to a foreign process and must not be raced.
        if ManagementPortProbe.shouldAbortStartForPortConflict(
            hasLiveProcess: false,
            portInUse: ManagementPortProbe.isPortInUse(port)
        ) {
            state = .failed
            lastError = "management port \(port) is already in use; not starting the engine"
            appendToLog("=== memory-connector engine start aborted: management port \(port) already in use ===\n")
            return
        }

        launchEngine(at: url, resetBreaker: shouldResetBreaker)
    }

    /// Spawns the engine binary at `url`, wiring its stdout/stderr and
    /// termination into the per-project log sink and circuit breaker.
    private func launchEngine(at url: URL, resetBreaker: Bool) {
        state = .starting
        if resetBreaker {
            policy = RestartPolicy()
            restartCount = 0
        }
        lastExitCode = nil
        lastError = nil
        pendingConfigurationError = nil

        let proc = Process()
        proc.executableURL = url
        proc.arguments = EngineManager.launchArguments(configPath: configPath, port: port)
        let stdoutPipe = Pipe()
        let stderrPipe = Pipe()
        proc.standardOutput = stdoutPipe
        proc.standardError = stderrPipe

        wireHandlers(proc: proc,
                     stdoutHandle: stdoutPipe.fileHandleForReading,
                     stderrHandle: stderrPipe.fileHandleForReading)

        do {
            try proc.run()
            process = proc
            state = .running
            appendToLog("=== memory-connector engine start ===\n")
            appendToLog("engine running (pid \(proc.processIdentifier))\n")
        } catch {
            state = .failed
            lastError = "failed to launch engine: \(error.localizedDescription)"
            appendToLog("launch failed: \(error.localizedDescription)\n")
        }
    }

    /// Wires a spawned process's stdout/stderr streams and termination into the
    /// per-project log sink and exit handling.
    private func wireHandlers(proc: Process, stdoutHandle: FileHandle, stderrHandle: FileHandle) {
        stdoutHandle.readabilityHandler = { [sink] handle in
            let data = handle.availableData
            if !data.isEmpty {
                Task { await sink.append(stdout: data) }
            }
        }
        stderrHandle.readabilityHandler = { [weak self] handle in
            let data = handle.availableData
            guard !data.isEmpty else { return }
            self?.handleStderr(data)
        }

        proc.terminationHandler = { [weak self] terminated in
            stdoutHandle.readabilityHandler = nil
            stderrHandle.readabilityHandler = nil
            // Drain synchronously (on this background thread, as before), then
            // route through the sink and flush so a final unterminated line is
            // not lost.
            let stdoutRemainder = (try? stdoutHandle.readToEnd()) ?? Data()
            let stderrRemainder = (try? stderrHandle.readToEnd()) ?? Data()
            Task { @MainActor in
                if !stdoutRemainder.isEmpty {
                    _ = await self?.sink.append(stdout: stdoutRemainder)
                }
                if !stderrRemainder.isEmpty {
                    let lines = await self?.sink.append(stderr: stderrRemainder) ?? []
                    self?.surfaceBindFailureIfAny(in: lines)
                }
                await self?.sink.flush()
                self?.processDidExit(terminated)
            }
        }
    }

    /// Stops the engine and disables auto-restart. Idempotent. Cancels any
    /// queued restart so an explicit stop always wins.
    func stop() {
        stoppedByUser = true
        pendingRestart = false
        pendingRestartResetBreaker = false
        forceKillTask?.cancel()
        forceKillTask = nil
        guard let proc = process, proc.isRunning else {
            process = nil
            state = .stopped
            return
        }
        appendToLog("=== memory-connector engine stop (SIGTERM) ===\n")
        state = .stopped
        proc.terminate() // SIGTERM — engine exits cleanly; handler confirms below
    }

    /// Surfaces a configuration error without spawning anything — e.g. a
    /// connected project's engine config is missing. Never starts a process.
    /// When a child is still terminating, the error is recorded and applied by
    /// `processDidExit` once the process is gone, so it is never silently
    /// dropped (the caller has already called `stop()` in this path).
    func reportConfigurationError(_ message: String) {
        lastError = message
        pendingConfigurationError = message
        guard process == nil else { return }
        state = .failed
    }

    /// Restarts the engine with the current configuration.
    ///
    /// A restart **sequences on the previous child's exit**: the successor is
    /// spawned only after the old process has terminated and released the
    /// loopback management port. Spawning while the old child still held the
    /// port left the new engine running with a dead management API.
    func restart() {
        switch EngineManager.restartAction(hasLiveProcess: process?.isRunning == true,
                                           isBusy: state == .starting || state == .restarting)
        {
        case .startNow:
            if state == .running {
                state = .stopped
            }
            start(resetBreaker: true)
        case .terminateThenRestart:
            pendingRestart = true
            pendingRestartResetBreaker = true
            state = .restarting
            appendToLog("=== memory-connector engine restart (SIGTERM) ===\n")
            if let proc = process {
                proc.terminate()
                scheduleForceKillIfNeeded(proc)
            }
        case .queueRestart:
            pendingRestart = true
            pendingRestartResetBreaker = true
        }
    }

    /// Kills `proc` if it has not exited within ``EngineManager/terminateGracePeriod``,
    /// so a hung child cannot wedge a queued restart.
    private func scheduleForceKillIfNeeded(_ proc: Process) {
        forceKillTask?.cancel()
        forceKillTask = Task { [weak self] in
            try? await Task.sleep(for: .seconds(EngineManager.terminateGracePeriod))
            guard !Task.isCancelled, let self else { return }
            guard process === proc, proc.isRunning else { return }
            appendToLog("engine did not exit after SIGTERM; killing it\n")
            kill(proc.processIdentifier, SIGKILL)
        }
    }

    // MARK: - Engine discovery

    private func engineURL() -> URL? {
        Bundle.main.url(forResource: "memory-connector", withExtension: nil)
    }

    // MARK: - Termination / circuit breaker

    /// Handles a process exit. The identity check drops termination callbacks
    /// from a process that stop()/restart() already replaced, so a stale
    /// handler can never trip the breaker or restart against the new process.
    private func processDidExit(_ terminated: Process) {
        let exitCode = terminated.terminationStatus
        if let current = process, current !== terminated {
            return // stale callback from a superseded process
        }
        forceKillTask?.cancel()
        forceKillTask = nil
        process = nil
        lastExitCode = exitCode

        if pendingRestart {
            pendingRestart = false
            appendToLog("engine exited \(exitCode); restarting with current configuration\n")
            start()
            return
        }

        if stoppedByUser {
            if let pending = pendingConfigurationError {
                state = .failed
                lastError = pending
                appendToLog("engine stopped (configuration error): \(pending)\n")
            } else {
                state = .stopped
                appendToLog("engine exited \(exitCode) after user stop\n")
            }
            return
        }

        let decision = policy.registerExit()
        restartCount = policy.restartCount
        switch decision {
        case .giveUp:
            state = .failed
            lastError = "engine exited \(restartCount) times within \(Int(policy.window))s — auto-restart stopped"
            appendToLog("circuit breaker tripped after \(restartCount) exits (code \(exitCode)); auto-restart stopped\n")
        case let .restart(delay):
            state = .restarting
            appendToLog("engine exited (code \(exitCode)); restarting in \(Int(delay))s\n")
            let seconds = delay
            Task { [weak self] in
                try? await Task.sleep(nanoseconds: UInt64(seconds * 1_000_000_000))
                guard let self, !self.stoppedByUser, process == nil else { return }
                start()
            }
        }
    }

    // MARK: - Management port bind failure surfacing

    /// Detects the engine's own management-API bind failure in a stderr chunk.
    private nonisolated static func looksLikeManagementPortBindFailure(in chunk: Data) -> Bool {
        guard let text = String(data: chunk, encoding: .utf8)?.lowercased() else { return false }
        return text.contains("management api")
            && (text.contains("address already in use") || text.contains("bind:"))
    }

    /// Routes a raw stderr chunk through the per-project log sink and surfaces
    /// a management-port bind failure once a complete line matches.
    private nonisolated func handleStderr(_ data: Data) {
        Task { @MainActor [weak self] in
            let lines = await self?.sink.append(stderr: data) ?? []
            self?.surfaceBindFailureIfAny(in: lines)
        }
    }

    private func surfaceBindFailureIfAny(in lines: [String]) {
        for line in lines where Self.looksLikeManagementPortBindFailure(in: Data(line.utf8)) {
            surfaceManagementPortBindFailure()
        }
    }

    private func surfaceManagementPortBindFailure() {
        lastError = "engine management API could not bind port \(port)"
        appendToLog("=== engine management API bind failure: port \(port) already in use ===\n")
    }

    // MARK: - Logging

    /// Routes one complete control line (trailing newline included) to the
    /// per-project log sink. Fire-and-forget: logging must never fail an
    /// operation.
    private func appendToLog(_ line: String) {
        Task { await sink.append(line: line) }
    }
}
