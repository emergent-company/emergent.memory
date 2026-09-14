import Foundation

/// Notes/Reminders Automation (TCC) permission handling.
///
/// macOS exposes no programmatic Automation-status API, so the probe is the
/// truth: run a harmless osascript that targets the app — the first run
/// triggers the one-time system prompt; later runs exit immediately. Outcome
/// mapping and persistence are pure and unit-tested; only `runProbe` spawns a
/// real process (never invoked from tests).
enum PermissionCenter {

    enum Service: String, CaseIterable {
        case notes
        case reminders

        var displayName: String {
            switch self {
            case .notes: return "Notes"
            case .reminders: return "Reminders"
            }
        }

        var systemImage: String {
            switch self {
            case .notes: return "note.text"
            case .reminders: return "checklist"
            }
        }

        /// Harmless first touch that still requires Automation permission.
        var probeScript: String {
            switch self {
            case .notes: return "tell application \"Notes\" to get name"
            case .reminders: return "tell application \"Reminders\" to get name of every list"
            }
        }
    }

    enum PermissionState: Equatable {
        case unknown        // never probed (or nothing learned yet)
        case requested      // a probe ran and a system prompt may be pending
        case granted
        case denied

        var label: String {
            switch self {
            case .unknown: return "Not granted yet"
            case .requested: return "Waiting for macOS prompt…"
            case .granted: return "Granted"
            case .denied: return "Denied"
            }
        }
    }

    /// Pure mapping of a probe outcome to a permission state.
    /// `wasRequested` disambiguates the timeout case: a probe that timed out
    /// right after the app asked for access usually means the system prompt is
    /// still on screen (.requested), while an unrelated timeout stays unknown.
    static func state(fromProbe exitCode: Int32, stderr: String, timedOut: Bool, wasRequested: Bool) -> PermissionState {
        if !timedOut && exitCode == 0 {
            return .granted
        }
        if timedOut {
            return wasRequested ? .requested : .unknown
        }
        let lower = stderr.lowercased()
        if lower.contains("-1743") || lower.contains("not allowed") || lower.contains("not authorized") {
            return .denied
        }
        return wasRequested ? .requested : .unknown
    }

    // MARK: - Probe runner (real process; not used by unit tests)

    struct ProbeOutcome {
        let exitCode: Int32
        let stderr: String
        let timedOut: Bool
    }

    /// Long timeout: the first probe may wait for the user to click the
    /// one-time Automation prompt.
    static func runProbe(_ service: Service, timeout: TimeInterval = 30) async -> ProbeOutcome {
        let result = await ProcessRunner.run(
            executable: URL(fileURLWithPath: "/usr/bin/osascript"),
            arguments: ["-e", service.probeScript],
            timeout: timeout
        )
        return ProbeOutcome(exitCode: result.exitCode, stderr: result.stdout, timedOut: result.timedOut)
    }

    // MARK: - Persistence (last known state + request bookkeeping)

    static func stateKey(for service: Service) -> String {
        "connector.permission.\(service.rawValue).state"
    }

    static func requestedAtKey(for service: Service) -> String {
        "connector.permission.\(service.rawValue).requestedAt"
    }

    static func loadState(_ service: Service, defaults: UserDefaults = .standard) -> PermissionState {
        guard let raw = defaults.string(forKey: stateKey(for: service)) else { return .unknown }
        switch raw {
        case "unknown": return .unknown
        case "requested": return .requested
        case "granted": return .granted
        case "denied": return .denied
        default: return .unknown
        }
    }

    static func saveState(_ state: PermissionState, for service: Service, defaults: UserDefaults = .standard) {
        defaults.set(stateLabelKey(state), forKey: stateKey(for: service))
    }

    static func markRequested(_ service: Service, at date: Date = Date(), defaults: UserDefaults = .standard) {
        defaults.set(date.timeIntervalSince1970, forKey: requestedAtKey(for: service))
        saveState(.requested, for: service, defaults: defaults)
    }

    static func hasRequested(_ service: Service, defaults: UserDefaults = .standard) -> Bool {
        defaults.object(forKey: requestedAtKey(for: service)) != nil
    }

    private static func stateLabelKey(_ state: PermissionState) -> String {
        switch state {
        case .unknown: return "unknown"
        case .requested: return "requested"
        case .granted: return "granted"
        case .denied: return "denied"
        }
    }
}
