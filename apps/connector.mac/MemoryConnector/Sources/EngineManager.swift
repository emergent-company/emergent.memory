import Foundation
import Observation

/// Supervises the embedded `memory-connector` engine process.
///
/// The engine binary ships inside the app bundle (Contents/Resources, built by
/// the `Build embedded engine binary` run-script phase) and is spawned ONLY as
/// a direct `Process` child — no launchd, no NSWorkspace — so Automation (TCC)
/// grants stay attributed to this app. stdout/stderr are piped into
/// `~/Library/Logs/memory-connector-app.log`. Unexpected exits are handled by
/// a `RestartPolicy` circuit breaker (3 restarts within 60s, then `.failed`);
/// `stop()` (SIGTERM) suppresses restarting and leaves the engine's own clean
/// SIGTERM path to exit 0.
@MainActor
@Observable
final class EngineManager {
    /// Process lifecycle for the menu-bar UI.
    enum State: Equatable {
        case stopped
        case starting
        case running
        case restarting
        case failed
    }

    /// Shared instance owned by the app; the app delegate stops it on quit.
    static let shared = EngineManager(configPath: defaultConfigPath)

    /// Config path handed to `relay --config`. Section 4 materializes this
    /// file before each engine (re)start; until then the engine exits cleanly
    /// (code 1, "run init first") and the circuit breaker absorbs it.
    nonisolated static let defaultConfigPath = NSHomeDirectory() + "/.config/memory-connector.yml"

    nonisolated static let logFileURL = URL(fileURLWithPath: NSHomeDirectory())
        .appendingPathComponent("Library/Logs/memory-connector-app.log")

    /// The engine command line. Extracted so the argument list is testable
    /// without spawning a process.
    nonisolated static func launchArguments(configPath: String) -> [String] {
        ["relay", "--config", configPath, "--api-port", String(ManagementPortProbe.port)]
    }

    private(set) var state: State = .stopped
    private(set) var restartCount = 0
    private(set) var lastExitCode: Int32?
    private(set) var lastError: String?

    private let configPath: String
    private var process: Process?
    private var policy = RestartPolicy()
    private var stoppedByUser = false
    /// A configuration error reported while a child is still terminating.
    /// `reportConfigurationError` cannot set `.failed` until the child exits, so
    /// it records the message here and `processDidExit` applies it.
    private var pendingConfigurationError: String?

    init(configPath: String) {
        self.configPath = configPath
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
        stoppedByUser = false

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
        // process. Only probed when no live child exists (`isRunning`), so a
        // just-SIGTERM'd-but-still-running child cannot false-positive the
        // probe, while a stopped-but-not-yet-cleared child is still probed.
        if process?.isRunning != true,
           ManagementPortProbe.shouldAbortStartForPortConflict(
               hasLiveProcess: false,
               portInUse: ManagementPortProbe.portInUse()
           )
        {
            state = .failed
            lastError = "management port \(ManagementPortProbe.port) is already in use; not starting the engine"
            appendToLog("=== memory-connector engine start aborted: management port \(ManagementPortProbe.port) already in use ===\n")
            return
        }

        launchEngine(at: url, resetBreaker: resetBreaker)
    }

    /// Spawns the engine binary at `url`, wiring its stdout/stderr and
    /// termination into the log sink and circuit breaker.
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
        proc.arguments = Self.launchArguments(configPath: configPath)
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
    /// log sink and exit handling.
    private func wireHandlers(proc: Process, stdoutHandle: FileHandle, stderrHandle: FileHandle) {
        stdoutHandle.readabilityHandler = { handle in
            let data = handle.availableData
            if !data.isEmpty {
                Task { await LogSink.shared.append(stdout: data) }
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
                    _ = await LogSink.shared.append(stdout: stdoutRemainder)
                }
                if !stderrRemainder.isEmpty {
                    let lines = await LogSink.shared.append(stderr: stderrRemainder)
                    self?.surfaceBindFailureIfAny(in: lines)
                }
                await LogSink.shared.flush()
                self?.processDidExit(terminated)
            }
        }
    }

    /// Stops the engine and disables auto-restart. Idempotent.
    func stop() {
        stoppedByUser = true
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

    /// Restarts the engine with the current configuration: if one is running
    /// (or pending), stop it and spawn a fresh process; otherwise just start.
    /// Used by `ProjectStore.connect` after it writes the connected project's
    /// config. Callers must only invoke this for a connected project.
    func restart() {
        switch state {
        case .running, .starting, .restarting:
            stop()
            start(resetBreaker: true)
        case .stopped, .failed:
            start(resetBreaker: true)
        }
    }

    // MARK: - Engine discovery

    /// The engine binary embedded by the `Build embedded engine binary`
    /// run-script phase (Contents/Resources/memory-connector).
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
        process = nil
        lastExitCode = exitCode

        if stoppedByUser {
            // A configuration error reported while the child was terminating is
            // applied here once the process is gone, so it is never dropped.
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

    /// Detects the engine's own management-API bind failure in a stderr chunk:
    /// the engine prints "listening" unconditionally and keeps running with a
    /// dead management API after a failed bind, so the failure must be surfaced
    /// rather than left only in the raw log. Matches "management API" together
    /// with "address already in use" or "bind:".
    private nonisolated static func looksLikeManagementPortBindFailure(in chunk: Data) -> Bool {
        guard let text = String(data: chunk, encoding: .utf8)?.lowercased() else { return false }
        return text.contains("management api")
            && (text.contains("address already in use") || text.contains("bind:"))
    }

    /// Routes a raw stderr chunk through the log sink and surfaces a
    /// management-port bind failure once a complete line matches. Nonisolated so
    /// the Process stderr readability callback (a background thread) can call it
    /// without hopping to the main actor.
    private nonisolated func handleStderr(_ data: Data) {
        Task { @MainActor [weak self] in
            let lines = await LogSink.shared.append(stderr: data)
            self?.surfaceBindFailureIfAny(in: lines)
        }
    }

    /// Surfaces a management-port bind failure once a complete stderr line
    /// matches. The log sink line-frames stderr (so a diagnostic split across
    /// pipe reads is never missed) and returns the whole lines here.
    private func surfaceBindFailureIfAny(in lines: [String]) {
        for line in lines where Self.looksLikeManagementPortBindFailure(in: Data(line.utf8)) {
            surfaceManagementPortBindFailure()
        }
    }

    /// Surfaces a management-port bind failure (marker line + `lastError`).
    private func surfaceManagementPortBindFailure() {
        lastError = "engine management API could not bind port \(ManagementPortProbe.port)"
        appendToLog("=== engine management API bind failure: port \(ManagementPortProbe.port) already in use ===\n")
    }

    // MARK: - Logging

    /// Routes one complete control line (trailing newline included) to the
    /// shared serialized sink. Fire-and-forget: logging must never fail an
    /// operation.
    private func appendToLog(_ line: String) {
        Task { await LogSink.shared.append(line: line) }
    }
}
