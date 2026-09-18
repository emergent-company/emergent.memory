import Darwin
import Foundation

/// Minimal lifecycle logger. Appends `[lifecycle]`-prefixed lines to the SAME
/// file `EngineManager.logFileURL` uses, so account/scope transitions interleave
/// with the engine's own `engine start` / `engine stop` output and the two can
/// be correlated in one place.
///
/// Open/append/close per line — no long-lived handle to leak, and independent of
/// `EngineManager`'s streaming handle. Both handles open with `O_APPEND` so the
/// two writers cannot reorder or overwrite each other's bytes. Best-effort:
/// failures are swallowed, because logging must never fail an account transition.
///
/// Writes are suppressed under hosted unit tests: the test bundle runs inside
/// the app process, so without this guard a test run would append `[lifecycle]`
/// lines (and test-stub accounts) to the real user's log at
/// `~/Library/Logs/memory-connector-app.log`, polluting the file the
/// observability lines exist to make readable.
enum ConnectorLog {

    /// Destination for lifecycle lines. Defaults to the shared app log so the
    /// lines interleave with the engine's own output; injectable so tests can
    /// point it at a temporary file and assert on real writes.
    nonisolated(unsafe) static var destinationURL: URL = EngineManager.logFileURL

    /// Whether lifecycle writes are suppressed for this process. Defaults to the
    /// hosted-test check; injectable because a test process is *always* a hosted
    /// test run, so the write path would otherwise be unreachable from tests.
    nonisolated(unsafe) static var isSuppressed: () -> Bool = {
        ConnectorLog.isHostedTest(environment: ProcessInfo.processInfo.environment)
    }

    /// Appends a `[lifecycle]` line (with a trailing newline).
    static func lifecycle(_ message: String) {
        append("[lifecycle] \(message)\n")
    }

    /// Whether the given environment describes a hosted unit-test run. Pure so it
    /// is unit-testable; mirrors the hosted-test check
    /// `AppEnvironment.defaultLegacyMigrator()` uses to avoid touching real
    /// Keychain/config state from a test.
    static func isHostedTest(environment: [String: String]) -> Bool {
        environment["XCTestConfigurationFilePath"] != nil
            || environment["XCTestBundlePath"] != nil
    }

    private static func append(_ line: String) {
        guard !isSuppressed() else { return }
        let url = destinationURL
        do {
            try FileManager.default.createDirectory(
                at: url.deletingLastPathComponent(),
                withIntermediateDirectories: true
            )
            // O_APPEND makes every write an atomic append, so this per-line
            // handle cannot race `EngineManager`'s streaming handle.
            let fd = open(url.path, O_WRONLY | O_APPEND | O_CREAT, 0o600)
            guard fd >= 0 else { return }
            let handle = FileHandle(fileDescriptor: fd)
            defer { try? handle.close() }
            handle.write(Data(line.utf8))
        } catch {
            // Best-effort: never surface a logging failure to the caller.
        }
    }
}
