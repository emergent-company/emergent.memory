import Foundation

/// Minimal lifecycle logger. Appends `[lifecycle]`-prefixed lines to the SAME
/// file `EngineManager.logFileURL` uses, so account/scope transitions interleave
/// with the engine's own `engine start` / `engine stop` output and the two can
/// be correlated in one place.
///
/// Open/append/close per line — no long-lived handle to leak, and independent of
/// `EngineManager`'s streaming handle. Best-effort: failures are swallowed,
/// because logging must never fail an account transition.
enum ConnectorLog {

    /// Appends a `[lifecycle]` line (with a trailing newline).
    static func lifecycle(_ message: String) {
        append("[lifecycle] \(message)\n")
    }

    private static func append(_ line: String) {
        let url = EngineManager.logFileURL
        do {
            try FileManager.default.createDirectory(
                at: url.deletingLastPathComponent(),
                withIntermediateDirectories: true
            )
            if !FileManager.default.fileExists(atPath: url.path) {
                FileManager.default.createFile(atPath: url.path, contents: nil)
            }
            let handle = try FileHandle(forWritingTo: url)
            defer { try? handle.close() }
            handle.seekToEndOfFile()
            handle.write(Data(line.utf8))
        } catch {
            // Best-effort: never surface a logging failure to the caller.
        }
    }
}
