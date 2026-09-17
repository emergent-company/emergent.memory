import XCTest
@testable import MemoryConnector

/// The lifecycle logger must stay silent under hosted unit tests: the test
/// bundle runs inside the app process, so an unguarded write would append
/// `[lifecycle]` lines (and test-stub accounts) to the real user log at
/// `~/Library/Logs/memory-connector-app.log`.
final class ConnectorLogTests: XCTestCase {

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

    /// The running suite itself is a hosted test run, so the guard must be
    /// active here — this is the regression that polluted the real log.
    func testCurrentProcessIsRecognisedAsHostedTest() {
        XCTAssertTrue(ConnectorLog.isHostedTest(
            environment: ProcessInfo.processInfo.environment))
    }
}
