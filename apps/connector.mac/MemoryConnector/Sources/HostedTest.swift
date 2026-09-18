import Foundation

/// Whether this process is a hosted unit-test run.
///
/// The test bundle runs *inside* the app process, so anything that would touch
/// the real user's state must be skipped under tests: the engine must not start
/// (`AppDelegate`), the legacy migrator must not touch the real
/// Keychain/session/config (`AppEnvironment`), and lifecycle logging must not
/// append to the real user log (`ConnectorLog`). Keeping the check here gives
/// those call sites a single source of truth.
enum HostedTest {

    /// Detects a hosted XCTest run from the process environment. Pure so it is
    /// unit-testable without a real test harness.
    static func isRunning(
        environment: [String: String] = ProcessInfo.processInfo.environment
    ) -> Bool {
        environment["XCTestConfigurationFilePath"] != nil
            || environment["XCTestBundlePath"] != nil
    }
}
