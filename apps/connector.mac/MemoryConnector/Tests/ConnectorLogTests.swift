@testable import MemoryConnector
import XCTest

/// The lifecycle logger routes through `LogSink.shared`, whose default
/// suppression drops lifecycle lines under hosted unit tests: the test bundle
/// runs inside the app process, so an unguarded write would append
/// `[lifecycle]` lines (and test-stub accounts) to the real user log at
/// `~/Library/Logs/memory-connector-app.log`.
///
/// These tests pin the environment detection and the default suppression
/// decision. The actual write path (destination honoured, whole-line atomicity,
/// distinguishability) is covered by `LogSinkTests` against injected sinks.
final class ConnectorLogTests: XCTestCase {
    // MARK: - Environment detection

    func testHostedTestDetectedByConfigurationPath() {
        XCTAssertTrue(HostedTest.isRunning(
            environment: ["XCTestConfigurationFilePath": "/tmp/x.xctestconfiguration"]
        ))
    }

    func testHostedTestDetectedByBundlePath() {
        XCTAssertTrue(HostedTest.isRunning(
            environment: ["XCTestBundlePath": "/tmp/MemoryConnectorTests.xctest"]
        ))
    }

    func testRealRunIsNotAHostedTest() {
        XCTAssertFalse(HostedTest.isRunning(environment: [:]))
        XCTAssertFalse(HostedTest.isRunning(
            environment: ["HOME": "/Users/someone", "PATH": "/usr/bin"]
        ))
    }

    /// The running suite is itself a hosted test run, so the *default* sink
    /// suppression must drop lifecycle lines here.
    func testCurrentProcessSuppressesByDefault() async throws {
        XCTAssertTrue(HostedTest.isRunning(environment: ProcessInfo.processInfo.environment))

        let dir = FileManager.default.temporaryDirectory
            .appendingPathComponent("connector-log-default-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: dir) }
        let logURL = dir.appendingPathComponent("sink.log")

        // Default suppression = HostedTest.isRunning() (true here), so the
        // write must be dropped even though the destination is a fresh temp file.
        let sink = LogSink(destination: logURL)
        await sink.append(line: "[lifecycle] account=dev:stub source=signIn\n")

        XCTAssertFalse(FileManager.default.fileExists(atPath: logURL.path),
                       "a default sink must suppress lifecycle writes under a hosted test")
    }
}
