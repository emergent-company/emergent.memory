import Foundation

/// File-backed store for the app's secrets, under
/// `~/.config/memory-connector/`:
///   - `accounts.json`        — non-secret signed-in account index
///   - `accounts/<id>/…`      — per-account project profiles
///   - `session.json`         — legacy session file (migration source only)
///
/// Files are identity-independent, so they survive ad-hoc rebuilds without
/// Keychain prompts (the engine already keeps its `emt_*` token in a 0600 YAML
/// file). Directory mode 0700, files 0600 (atomic writes). Reads are tolerant:
/// missing/corrupt input yields nil so callers can fall back / migrate.
struct AppSecretStore: Sendable {
    static let sessionFileName = "session.json"

    let baseDirectory: URL

    init(baseDirectory: URL = AppSecretStore.defaultBaseDirectory) {
        self.baseDirectory = baseDirectory
    }

    static var defaultBaseDirectory: URL {
        URL(fileURLWithPath: NSHomeDirectory())
            .appendingPathComponent(".config/memory-connector", isDirectory: true)
    }

    func fileURL(_ name: String) -> URL {
        baseDirectory.appendingPathComponent(name)
    }

    /// Tolerant read: missing or unreadable files return nil.
    func readData(_ name: String) -> Data? {
        try? Data(contentsOf: fileURL(name))
    }

    /// Atomic write with 0600 set on the file (and 0700 on a created parent).
    func writeData(_ data: Data, to name: String) throws {
        let fm = FileManager.default
        if !fm.fileExists(atPath: baseDirectory.path) {
            try fm.createDirectory(at: baseDirectory, withIntermediateDirectories: true,
                                   attributes: [.posixPermissions: 0o700])
        }
        let url = fileURL(name)
        try data.write(to: url, options: .atomic)
        try fm.setAttributes([.posixPermissions: 0o600], ofItemAtPath: url.path)
    }

    /// Best-effort delete (missing file is fine).
    func delete(_ name: String) {
        try? FileManager.default.removeItem(at: fileURL(name))
    }
}
