import Darwin
import Foundation

/// Serializes every log write — engine stdout, engine stderr, and app lifecycle
/// lines — through a single append-only handle so concurrent writers cannot
/// interleave or truncate lines. Actor-isolated: the handle and the per-stream
/// line-framing buffers are owned here, replacing the `nonisolated(unsafe)`
/// shared mutable log state previously used by `EngineManager`/`ConnectorLog`.
actor LogSink {
    /// Process-wide sink for the shared app log at
    /// `~/Library/Logs/memory-connector-app.log`.
    static let shared = LogSink(destination: EngineManager.logFileURL)

    private let destination: URL
    private let shouldSuppress: @Sendable () -> Bool
    private var handle: FileHandle?
    /// Unterminated bytes from each stream, framed into whole lines so a line
    /// split across pipe reads is still written atomically. Kept per-stream so
    /// stdout and stderr lines never merge into each other.
    private var stdoutPending = Data()
    private var stderrPending = Data()

    /// - Parameters:
    ///   - destination: the log file to append to.
    ///   - shouldSuppress: when true, lifecycle lines are dropped (hosted test
    ///     runs must not pollute the real user log).
    init(destination: URL,
         shouldSuppress: @escaping @Sendable () -> Bool = { HostedTest.isRunning() })
    {
        self.destination = destination
        self.shouldSuppress = shouldSuppress
    }

    /// Appends one complete lifecycle/control line (caller includes the trailing
    /// newline). Suppressed when `shouldSuppress` is true.
    func append(line: String) {
        guard !shouldSuppress() else { return }
        write(Data(line.utf8))
    }

    /// Appends engine stdout bytes, line-framing so only whole lines are
    /// written. Trailing unterminated bytes stay buffered until the next chunk
    /// or `flush()`.
    func append(stdout data: Data) {
        _ = frame(data, into: &stdoutPending)
    }

    /// Appends engine stderr bytes, line-framing as for stdout, and returns the
    /// complete lines (newline stripped) so the caller can scan them.
    func append(stderr data: Data) -> [String] {
        frame(data, into: &stderrPending)
    }

    /// Writes any buffered trailing bytes (used when the engine exits so a final
    /// unterminated line is not lost).
    func flush() {
        if !stdoutPending.isEmpty {
            write(stdoutPending)
            stdoutPending = Data()
        }
        if !stderrPending.isEmpty {
            write(stderrPending)
            stderrPending = Data()
        }
    }

    // MARK: - Internals

    /// Line-frames `data` against `pending`, writing each complete line and
    /// collecting the decoded line strings. `pending` retains the trailing
    /// unterminated bytes.
    private func frame(_ data: Data, into pending: inout Data) -> [String] {
        pending.append(data)
        var lines: [String] = []
        let newline = Data([0x0A])
        while let range = pending.range(of: newline) {
            let lineData = pending.subdata(in: pending.startIndex ..< range.lowerBound)
            pending.removeSubrange(pending.startIndex ... range.lowerBound)
            write(lineData + newline)
            if let text = String(data: lineData, encoding: .utf8) {
                lines.append(text)
            }
        }
        return lines
    }

    private func write(_ data: Data) {
        guard let handle = openIfNeeded() else { return }
        handle.write(data)
    }

    /// Lazily opens the append-only handle, creating the parent directory.
    /// Returns nil (dropping the write) when the destination cannot be opened;
    /// logging is best-effort and must never fail a caller.
    private func openIfNeeded() -> FileHandle? {
        if let handle {
            return handle
        }
        do {
            try FileManager.default.createDirectory(
                at: destination.deletingLastPathComponent(),
                withIntermediateDirectories: true
            )
            // O_APPEND makes every write an atomic append at the OS level.
            let fileDescriptor = open(destination.path, O_WRONLY | O_APPEND | O_CREAT, 0o600)
            guard fileDescriptor >= 0 else { return nil }
            let handle = FileHandle(fileDescriptor: fileDescriptor)
            self.handle = handle
            return handle
        } catch {
            return nil
        }
    }
}

/// Minimal lifecycle logger. Appends `[lifecycle]`-prefixed lines to the SAME
/// file `EngineManager.logFileURL` uses, so account/scope transitions interleave
/// with the engine's own `engine start` / `engine stop` output and the two can
/// be correlated in one place.
///
/// Writes are serialized through `LogSink.shared` (one append-only handle) so
/// the app's lifecycle line is always whole and distinguishable from the
/// engine's own lines. Best-effort: failures are swallowed, because logging must
/// never fail an account transition. Suppressed under hosted unit tests so a
/// test run does not append `[lifecycle]` lines (and test-stub accounts) to the
/// real user log at `~/Library/Logs/memory-connector-app.log`.
enum ConnectorLog {
    /// Appends a `[lifecycle]` line (with a trailing newline).
    static func lifecycle(_ message: String) {
        Task { await LogSink.shared.append(line: "[lifecycle] \(message)\n") }
    }
}
