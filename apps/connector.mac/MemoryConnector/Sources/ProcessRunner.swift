import Darwin
import Foundation

/// Result of one short-lived subprocess run (engine `status` poll or an
/// Automation probe). No real process is spawned by the parser/mapping layers.
struct ProcessResult {
    let stdout: String
    let exitCode: Int32
    let timedOut: Bool
}

/// Runs short-lived subprocesses off the main actor. Used by StatusMonitor
/// (engine `status --config`) and PermissionCenter (osascript probes).
///
/// `Process`/`Pipe` are not Sendable, so they travel through @Sendable
/// closures inside `@unchecked Sendable` boxes (the app owns the only
/// reference; no shared mutable state beyond the boxed flag).
enum ProcessRunner {

    /// Runs `executable` with `arguments`, capturing combined stdout/stderr,
    /// bounded by `timeout`. On timeout the process is SIGTERM'd and, if still
    /// alive one second later, SIGKILL'd.
    static func run(executable: URL, arguments: [String], timeout: TimeInterval) async -> ProcessResult {
        await run(executable: executable, arguments: arguments, stdin: nil, timeout: timeout)
    }

    /// Variant that pipes `stdin` to the child. When `stdin` is non-nil the
    /// child's standard input is a pipe; the data is written and the writing
    /// end closed right after launch (before `waitUntilExit()`). Passing `nil`
    /// is equivalent to the three-argument overload. Captured stdout/stderr
    /// remains combined, as in the original.
    static func run(executable: URL,
                    arguments: [String],
                    stdin: Data?,
                    timeout: TimeInterval) async -> ProcessResult {
        await withCheckedContinuation { continuation in
            let box = ProcessBox(executable: executable, arguments: arguments, stdin: stdin)
            let timedOut = TimeoutFlag()

            DispatchQueue.global(qos: .userInitiated).async {
                do {
                    try box.proc.run()
                } catch {
                    continuation.resume(returning: ProcessResult(stdout: "", exitCode: 1, timedOut: false))
                    return
                }

                if let stdinPipe = box.stdinPipe {
                    stdinPipe.fileHandleForWriting.write(stdin ?? Data())
                    try? stdinPipe.fileHandleForWriting.close()
                }

                DispatchQueue.global().asyncAfter(deadline: .now() + timeout) {
                    guard box.proc.isRunning else { return }
                    timedOut.set(true)
                    box.proc.terminate() // SIGTERM first
                    DispatchQueue.global().asyncAfter(deadline: .now() + 1) {
                        guard box.proc.isRunning else { return }
                        // SIGKILL — a hung prompt must not linger.
                        Darwin.kill(box.proc.processIdentifier, SIGKILL)
                    }
                }

                box.proc.waitUntilExit()
                let data = (try? box.pipe.fileHandleForReading.readToEnd()) ?? Data()
                let stdout = String(data: data, encoding: .utf8) ?? ""
                continuation.resume(returning: ProcessResult(stdout: stdout,
                                                             exitCode: box.proc.terminationStatus,
                                                             timedOut: timedOut.value))
            }
        }
    }
}

/// Holds the non-Sendable subprocess state. App-owned; the only reference
/// lives inside one background execution at a time.
private final class ProcessBox: @unchecked Sendable {
    let proc: Process
    let pipe: Pipe
    /// Non-nil only when the caller supplied stdin; the app owns this pipe and
    /// closes its writing end after launching the child.
    let stdinPipe: Pipe?

    init(executable: URL, arguments: [String], stdin: Data?) {
        let proc = Process()
        proc.executableURL = executable
        proc.arguments = arguments
        let pipe = Pipe()
        proc.standardOutput = pipe
        proc.standardError = pipe
        if stdin != nil {
            let stdinPipe = Pipe()
            proc.standardInput = stdinPipe
            self.stdinPipe = stdinPipe
        } else {
            self.stdinPipe = nil
        }
        self.proc = proc
        self.pipe = pipe
    }
}

/// Boxed timeout flag, set from the asyncAfter killer and read after
/// waitUntilExit on the background queue.
private final class TimeoutFlag: @unchecked Sendable {
    private let lock = NSLock()
    private var _value = false

    func set(_ value: Bool) {
        lock.lock()
        _value = value
        lock.unlock()
    }

    var value: Bool {
        lock.lock()
        defer { lock.unlock() }
        return _value
    }
}
