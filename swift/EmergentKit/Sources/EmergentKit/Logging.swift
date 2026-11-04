import EmergentBridge
import Foundation
import OSLog

// MARK: - Log bridge

private let bridgeLogger = Logger(subsystem: "ai.emergent.sdk", category: "GoRuntime")

/// Configures the Go bridge to route its internal log output through Apple's
/// unified logging system (`os.Logger` / OSLog).
///
/// Call this once at app startup **before** creating any ``EmergentClient``:
///
/// ```swift
/// EmergentLogging.configure()
/// ```
///
/// After this call, Go-side log messages will appear in Xcode's Console and
/// the system log, tagged with subsystem `"ai.emergent.sdk"` and category
/// `"GoRuntime"`.
public enum EmergentLogging {

    /// Registers the Swift log callback with the Go bridge.
    public static func configure() {
        RegisterLogCallback { level, messagePtr in
            let message = messagePtr.map { String(cString: $0) } ?? "(nil)"
            switch level {
            case 0:  bridgeLogger.debug("\(message, privacy: .public)")
            case 1:  bridgeLogger.info("\(message, privacy: .public)")
            case 2:  bridgeLogger.warning("\(message, privacy: .public)")
            default: bridgeLogger.error("\(message, privacy: .public)")
            }
        }
    }
}
