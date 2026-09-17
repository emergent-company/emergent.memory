import Combine
import Darwin
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

    /// Injectable probe: true when something is already listening on the
    /// loopback management port. Settable so tests can stub the socket probe;
    /// the default attempts a TCP connect to `127.0.0.1:8931`.
    nonisolated(unsafe) static var managementPortInUse: () -> Bool = {
        EngineManager.tcpConnect(host: "127.0.0.1", port: UInt16(EngineManager.managementAPIPort))
    }

    /// The pure "may we spawn?" decision for a held management port: abort only
    /// when there is no live child AND the port is in use. A live child (e.g.
    /// `restart()`'s just-SIGTERM'd process) never aborts, so its lingering
    /// socket cannot false-positive the probe.
    nonisolated static func shouldAbortStartForPortConflict(hasLiveProcess: Bool,
                                                            portInUse: Bool) -> Bool {
        !hasLiveProcess && portInUse
    }

    /// Best-effort synchronous TCP connect probe: true when the connect
    /// succeeds (something is listening on `host:port`).
    nonisolated private static func tcpConnect(host: String, port: UInt16) -> Bool {
        let fd = socket(AF_INET, SOCK_STREAM, 0)
        guard fd >= 0 else { return false }
        defer { Darwin.close(fd) }

        var addr = sockaddr_in()
        addr.sin_len = UInt8(MemoryLayout<sockaddr_in>.size)
        addr.sin_family = sa_family_t(AF_INET)
        addr.sin_port = port.bigEndian
        addr.sin_addr.s_addr = inet_addr(host)

        let result = withUnsafePointer(to: &addr) {
            $0.withMemoryRebound(to: sockaddr.self, capacity: 1) {
                Darwin.connect(fd, $0, socklen_t(MemoryLayout<sockaddr_in>.size))
            }
        }
        return result == 0
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

        openLogIfNeeded()

        // Fail fast when the management port is already held by another
        // process. Only probed when we hold no live child, so restart()'s
        // just-SIGTERM'd child cannot false-positive the probe.
        if process == nil,
           Self.shouldAbortStartForPortConflict(hasLiveProcess: false,
                                                portInUse: Self.managementPortInUse()) {
            state = .failed
            lastError = "management port \(Self.managementAPIPort) is already in use; not starting the engine"
            appendToLog("=== memory-connector engine start aborted: management port \(Self.managementAPIPort) already in use ===\n")
            return
        }

        appendToLog("=== memory-connector engine start ===\n")

        state = .starting
        if resetBreaker {
            policy = RestartPolicy()
            restartCount = 0
        }
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
            guard !data.isEmpty else { return }
            self?.appendToLog(data)
            if Self.looksLikeManagementPortBindFailure(in: data) {
                Task { @MainActor [weak self] in
                    self?.surfaceManagementPortBindFailure()
                }
            }
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

    // MARK: - Management port bind failure surfacing

    /// Detects the engine's own management-API bind failure in a stderr chunk:
    /// the engine prints "listening" unconditionally and keeps running with a
    /// dead management API after a failed bind, so the failure must be surfaced
    /// rather than left only in the raw log. Matches "management API" together
    /// with "address already in use" or "bind:".
    nonisolated private static func looksLikeManagementPortBindFailure(in chunk: Data) -> Bool {
        guard let text = String(data: chunk, encoding: .utf8)?.lowercased() else { return false }
        return text.contains("management api")
            && (text.contains("address already in use") || text.contains("bind:"))
    }

    /// Surfaces a management-port bind failure (marker line + `lastError`).
    private func surfaceManagementPortBindFailure() {
        lastError = "engine management API could not bind port \(Self.managementAPIPort)"
        appendToLog("=== engine management API bind failure: port \(Self.managementAPIPort) already in use ===\n")
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
