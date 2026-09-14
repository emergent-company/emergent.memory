import Combine
import Foundation

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
final class EngineManager: ObservableObject {

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

    /// Loopback port for the engine's hosted-MCP-server management API
    /// (`relay --api-port`). Bound to 127.0.0.1 by the engine; the app's
    /// ``MCPServersAPIClient`` talks to it at `http://127.0.0.1:8931`.
    ///
    /// Deliberately NOT 8890: the Diane companion's local API uses 8890 on the
    /// same host, and both apps can run side by side.
    nonisolated static let managementAPIPort = 8931

    /// The engine command line. Extracted so the argument list is testable
    /// without spawning a process.
    nonisolated static func launchArguments(configPath: String) -> [String] {
        ["relay", "--config", configPath, "--api-port", String(managementAPIPort)]
    }

    @Published private(set) var state: State = .stopped
    @Published private(set) var restartCount = 0
    @Published private(set) var lastExitCode: Int32?
    @Published private(set) var lastError: String?

    private let configPath: String
    private var process: Process?
    private var policy = RestartPolicy()
    private var stoppedByUser = false
    /// Append-only handle for engine output. Written from Process
    /// readability/termination callbacks (background threads), which are not
    /// actor-isolated, so this is deliberately not a @MainActor property.
    private nonisolated(unsafe) var logHandle: FileHandle?

    init(configPath: String) {
        self.configPath = configPath
    }

    // MARK: - Public lifecycle

    /// Locates the embedded engine binary and spawns it as a direct child.
    /// Safe to call from `.stopped`, `.failed`, and `.restarting`.
    func start() {
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

        openLogIfNeeded()
        appendToLog("=== memory-connector engine start ===\n")

        state = .starting
        policy = RestartPolicy()
        restartCount = 0
        lastExitCode = nil
        lastError = nil

        let proc = Process()
        proc.executableURL = url
        proc.arguments = Self.launchArguments(configPath: configPath)
        let stdoutPipe = Pipe()
        let stderrPipe = Pipe()
        proc.standardOutput = stdoutPipe
        proc.standardError = stderrPipe

        let stdoutHandle = stdoutPipe.fileHandleForReading
        let stderrHandle = stderrPipe.fileHandleForReading

        stdoutHandle.readabilityHandler = { [weak self] handle in
            let data = handle.availableData
            if !data.isEmpty { self?.appendToLog(data) }
        }
        stderrHandle.readabilityHandler = { [weak self] handle in
            let data = handle.availableData
            if !data.isEmpty { self?.appendToLog(data) }
        }

        proc.terminationHandler = { [weak self] terminated in
            stdoutHandle.readabilityHandler = nil
            stderrHandle.readabilityHandler = nil
            // Drain anything still buffered before the pipes close.
            if let data = try? stdoutHandle.readToEnd(), !data.isEmpty { self?.appendToLog(data) }
            if let data = try? stderrHandle.readToEnd(), !data.isEmpty { self?.appendToLog(data) }
            Task { @MainActor in
                self?.processDidExit(terminated)
            }
        }

        do {
            try proc.run()
            process = proc
            state = .running
            appendToLog("engine running (pid \(proc.processIdentifier))\n")
        } catch {
            state = .failed
            lastError = "failed to launch engine: \(error.localizedDescription)"
            appendToLog("launch failed: \(error.localizedDescription)\n")
        }
    }

    /// Stops the engine and disables auto-restart. Idempotent.
    func stop() {
        stoppedByUser = true
        appendToLog("=== memory-connector engine stop (SIGTERM) ===\n")
        guard let proc = process, proc.isRunning else {
            process = nil
            state = .stopped
            return
        }
        state = .stopped
        proc.terminate() // SIGTERM — engine exits cleanly; handler confirms below
    }

    /// Surfaces a configuration error without spawning anything — e.g. a
    /// connected project's engine config is missing. Never starts a process;
    /// no-op while a process is already running.
    func reportConfigurationError(_ message: String) {
        guard process == nil else { return }
        state = .failed
        lastError = message
    }

    /// Restarts the engine with the current configuration: if one is running
    /// (or pending), stop it and spawn a fresh process; otherwise just start.
    /// Used by `ProjectStore.connect` after it writes the connected project's
    /// config. Callers must only invoke this for a connected project.
    func restart() {
        switch state {
        case .running, .starting, .restarting:
            stop()
            start()
        case .stopped, .failed:
            start()
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
            state = .stopped
            appendToLog("engine exited \(exitCode) after user stop\n")
            return
        }

        let decision = policy.registerExit()
        restartCount = policy.restartCount
        switch decision {
        case .giveUp:
            state = .failed
            lastError = "engine exited \(restartCount) times within \(Int(policy.window))s — auto-restart stopped"
            appendToLog("circuit breaker tripped after \(restartCount) exits (code \(exitCode)); auto-restart stopped\n")
        case .restart(let delay):
            state = .restarting
            appendToLog("engine exited (code \(exitCode)); restarting in \(Int(delay))s\n")
            let seconds = delay
            Task { [weak self] in
                try? await Task.sleep(nanoseconds: UInt64(seconds * 1_000_000_000))
                guard let self, !self.stoppedByUser, self.process == nil else { return }
                self.start()
            }
        }
    }

    // MARK: - Logging

    private func openLogIfNeeded() {
        guard logHandle == nil else { return }
        do {
            try FileManager.default.createDirectory(
                at: Self.logFileURL.deletingLastPathComponent(),
                withIntermediateDirectories: true
            )
            if !FileManager.default.fileExists(atPath: Self.logFileURL.path) {
                FileManager.default.createFile(atPath: Self.logFileURL.path, contents: nil)
            }
            let handle = try FileHandle(forWritingTo: Self.logFileURL)
            handle.seekToEndOfFile()
            logHandle = handle
        } catch {
            lastError = "cannot open engine log: \(error.localizedDescription)"
        }
    }

    /// Nonisolated so Process background callbacks can append without hopping
    /// to the main actor; only touches the append-only file handle.
    private nonisolated func appendToLog(_ data: Data) {
        logHandle?.write(data)
    }

    private func appendToLog(_ line: String) {
        appendToLog(Data(line.utf8))
    }
}
