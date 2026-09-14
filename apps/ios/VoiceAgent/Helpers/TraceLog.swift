import Foundation

/// Room-keyed JSONL session trace written to the app's Documents directory.
///
/// Local breadcrumb log (not Sentry). One JSON object per line:
/// `{"ts":"<ISO8601 UTC>","identity":"<participantIdentity>","room":"<room or empty>","event":"<event>",...fields}`
///
/// A coding agent fetches the file from the simulator and reconstructs the
/// exact user journey + failure point.
enum TraceLog {
    // `nonisolated`: the app builds with default MainActor isolation (see
    // `Log.swift`), but the token source's SDK callbacks run off the main
    // actor, so `log` must be callable from any context.
    nonisolated static let fileName = "memory-trace.jsonl"

    /// Serializes file appends so lines never interleave or tear.
    nonisolated private static let queue = DispatchQueue(label: "com.emergent.memory.tracelog")
    /// Stable per-install identity, read once. Same value as
    /// `MemoryConfig().participantIdentity` (and same `alfred.participantIdentity`
    /// key + `memory-ios-<UUID>` generation), but read directly from
    /// `UserDefaults` because `MemoryConfig`'s initializer is MainActor-isolated
    /// while `log` is called from nonisolated contexts.
    nonisolated private static let identity: String = {
        let key = "alfred.participantIdentity" // MemoryConfig.participantIdentityKey
        let defaults = UserDefaults.standard
        if let stored = defaults.string(forKey: key), !stored.isEmpty {
            return stored
        }
        let generated = "memory-ios-\(UUID().uuidString)"
        defaults.set(generated, forKey: key)
        return generated
    }()

    /// Appends one JSON line. `room` is empty until the LiveKit room is known.
    ///
    /// The record is built synchronously on the caller's thread (so `log` works
    /// from any isolation context); only the file append runs on the serial
    /// background queue.
    nonisolated static func log(_ event: String, _ fields: [String: Any] = [:], room: String = "") {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        var record: [String: Any] = [
            "ts": formatter.string(from: Date()),
            "identity": identity,
            "room": room,
            "event": event,
        ]
        for (key, value) in fields { record[key] = value }

        guard let data = try? JSONSerialization.data(withJSONObject: record),
              let line = String(data: data, encoding: .utf8) else { return }

        let fileName = Self.fileName
        // Best-effort append; failure degrades to a silent no-op.
        queue.async {
            do {
                let url = FileManager.default.urls(for: .documentDirectory, in: .userDomainMask)[0]
                    .appendingPathComponent(fileName)
                let payload = (line + "\n").data(using: .utf8) ?? Data()
                if FileManager.default.fileExists(atPath: url.path) {
                    let handle = try FileHandle(forWritingTo: url)
                    defer { try? handle.close() }
                    try handle.seekToEnd()
                    try handle.write(contentsOf: payload)
                } else {
                    try payload.write(to: url)
                }
            } catch {
                // Tracing must never break the app.
            }
        }
    }
}
