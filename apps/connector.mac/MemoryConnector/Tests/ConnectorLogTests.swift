import XCTest
@testable import MemoryConnector

/// The lifecycle logger must stay silent under hosted unit tests: the test
/// bundle runs inside the app process, so an unguarded write would append
/// `[lifecycle]` lines (and test-stub accounts) to the real user log at
/// `~/Library/Logs/memory-connector-app.log`.
///
/// These tests inject both the destination and the suppression decision. A suite
/// that only checked `isHostedTest` would stay green if the `guard` in `append`
/// were deleted, leaving hosted runs free to pollute the real log again.
final class ConnectorLogTests: XCTestCase {

    private var tempDir: URL!
    private var logURL: URL!
    private var originalDestination: URL!
    private var originalSuppression: (() -> Bool)!

    override func setUpWithError() throws {
        tempDir = FileManager.default.temporaryDirectory
            .appendingPathComponent("connector-log-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: tempDir, withIntermediateDirectories: true)
        logURL = tempDir.appendingPathComponent("lifecycle.log")
        try "sentinel\n".write(to: logURL, atomically: true, encoding: .utf8)

        originalDestination = ConnectorLog.destinationURL
        originalSuppression = ConnectorLog.isSuppressed
        ConnectorLog.destinationURL = logURL
    }

    override func tearDownWithError() throws {
        ConnectorLog.destinationURL = originalDestination
        ConnectorLog.isSuppressed = originalSuppression
        try? FileManager.default.removeItem(at: tempDir)
    }

    // MARK: - Environment detection

    func testHostedTestDetectedByConfigurationPath() {
        XCTAssertTrue(ConnectorLog.isHostedTest(
            environment: ["XCTestConfigurationFilePath": "/tmp/x.xctestconfiguration"]))
    }

    func testHostedTestDetectedByBundlePath() {
        XCTAssertTrue(ConnectorLog.isHostedTest(
            environment: ["XCTestBundlePath": "/tmp/MemoryConnectorTests.xctest"]))
    }

    func testRealRunIsNotAHostedTest() {
        XCTAssertFalse(ConnectorLog.isHostedTest(environment: [:]))
        XCTAssertFalse(ConnectorLog.isHostedTest(
            environment: ["HOME": "/Users/someone", "PATH": "/usr/bin"]))
    }

    /// The running suite is itself a hosted test run, so the *default* decision
    /// must suppress here.
    func testCurrentProcessSuppressesByDefault() {
        XCTAssertTrue(ConnectorLog.isHostedTest(environment: ProcessInfo.processInfo.environment))
        XCTAssertTrue(ConnectorLog.isSuppressed())
    }

    // MARK: - Actual writes

    /// Regression: if the suppression guard in `append` is removed or bypassed, a
    /// real `lifecycle` call writes and this fails. This is the assertion the
    /// detection-only tests could not make.
    func testLifecycleCallWritesNothingWhenSuppressed() throws {
        ConnectorLog.isSuppressed = { true }
        let before = try Data(contentsOf: logURL)

        ConnectorLog.lifecycle("account=dev:stub@example.test source=signIn")

        XCTAssertEqual(try Data(contentsOf: logURL), before,
                       "a suppressed lifecycle() call must leave the destination untouched")
    }

    /// Proves the write path works and the destination is honoured, so the
    /// suppression assertion above cannot pass vacuously.
    func testLifecycleCallAppendsToDestinationWhenNotSuppressed() throws {
        ConnectorLog.isSuppressed = { false }

        ConnectorLog.lifecycle("account=real source=switchTo")

        let text = try String(contentsOf: logURL, encoding: .utf8)
        XCTAssertTrue(text.hasPrefix("sentinel\n"), "must append, not truncate: \(text)")
        XCTAssertTrue(text.contains("[lifecycle] account=real source=switchTo"),
                      "expected the lifecycle line, got: \(text)")
    }
}
