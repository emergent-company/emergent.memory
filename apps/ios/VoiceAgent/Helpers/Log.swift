import os

/// Centralized logging for the Memory iOS app.
///
/// Subsystem `com.emergent.memory`, one category per area. Emit at `.info` (always
/// persisted) for lifecycle/flow events and `.error` for failures; `.debug`
/// only for high-volume noise. Visible via `log stream --predicate
/// 'subsystem == "com.emergent.memory"'`, Xcode's console, or
/// `tools/ios-debug-mac.sh log`.
enum Log {
    // `nonisolated`: the app builds with default MainActor isolation, but
    // some call sites are nonisolated (e.g. the token source's SDK callbacks).
    // `os.Logger` is Sendable, so the statics are safe to share.
    nonisolated static let nav     = Logger(subsystem: "com.emergent.memory", category: "navigation")
    nonisolated static let session = Logger(subsystem: "com.emergent.memory", category: "session")
    nonisolated static let net     = Logger(subsystem: "com.emergent.memory", category: "network")
    nonisolated static let agents  = Logger(subsystem: "com.emergent.memory", category: "agents")
    nonisolated static let qr      = Logger(subsystem: "com.emergent.memory", category: "qr")
}
