import Foundation

/// Detection of a UI-test launch, plus the isolated storage locations that such
/// a launch must use.
///
/// A UI test launches the app as a SEPARATE process, so `HostedTest.isRunning`
/// (env-var based) is FALSE there. Isolation must therefore key off a launch
/// *argument* instead: `-uiTestMode`. In that mode the app runs against an
/// isolated profile — a temp Application Support directory and an isolated
/// `UserDefaults` suite — and never touches the real config, Keychain, or
/// signed-in account state.
///
/// Everything here is inert when `-uiTestMode` is absent: `isActive` is the
/// single gate, and the derived locations are only consulted behind it.
enum UITestMode {
    /// True when the process was launched with the `-uiTestMode` argument.
    static var isActive: Bool {
        ProcessInfo.processInfo.arguments.contains("-uiTestMode")
    }

    /// Profile id from `MEMORY_UI_TEST_PROFILE`, giving each UI-test run its own
    /// private Application Support directory (the UI test sets it to a fresh
    /// UUID per launch).
    static var profileID: String {
        ProcessInfo.processInfo.environment["MEMORY_UI_TEST_PROFILE"] ?? "default"
    }

    /// Isolated Application Support root for the current profile. Stands in for
    /// `AppSecretStore.defaultBaseDirectory` (`~/.config/memory-connector/`), so
    /// the account index, per-account profiles, and legacy session are never
    /// read or written from the real user directory.
    static var rootDirectory: URL {
        FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
            .appendingPathComponent("MemoryConnectorUITests", isDirectory: true)
            .appendingPathComponent(profileID, isDirectory: true)
    }

    /// Isolated engine config path, mirroring `EngineManager.defaultConfigPath`
    /// but under the profile's Application Support directory. The file never
    /// exists on a fresh run, so `importExistingConfigIfNeeded` reads it and
    /// no-ops — it can never adopt the real `~/.config/memory-connector.yml`.
    static var engineConfigPath: String {
        rootDirectory.appendingPathComponent("memory-connector.yml").path
    }

    /// Isolated `UserDefaults` suite for the current profile. Never `.standard`,
    /// so persisted keys (server URL, instance id, disabled tools, active
    /// account id) come from a throwaway domain, not the real app defaults.
    static var defaults: UserDefaults {
        // `UserDefaults(suiteName:)` returns nil only for an empty suite name;
        // `profileID` is non-empty by construction. The fallback still never
        // resolves to `.standard`, so isolation is never silently lost.
        if let suite = UserDefaults(suiteName: "MemoryConnectorUITests.\(profileID)") {
            return suite
        }
        return UserDefaults(suiteName: "MemoryConnectorUITests.fallback")
            ?? UserDefaults(suiteName: "MemoryConnectorUITests")!
    }
}
