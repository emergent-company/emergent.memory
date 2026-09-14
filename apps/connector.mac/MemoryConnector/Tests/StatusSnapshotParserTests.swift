import XCTest
@testable import MemoryConnector

final class StatusSnapshotParserTests: XCTestCase {

    private func makeStdout(instance: String = "mbp-connector",
                            version: String = "0.1.0",
                            tools: String? = "notes_search, notes_create, reminders_list",
                            hub: String = "connected — hub shows 3 tool(s), matches local set") -> String {
        var lines: [String] = ["memory-connector status"]
        lines.append("instance: \(instance)")
        lines.append("version: \(version)")
        if let tools {
            lines.append("tools (\(tools.split(separator: ",").count)): \(tools)")
        } else {
            lines.append("tools: none (Notes/Reminders tools unavailable)")
        }
        lines.append("hub: \(hub)")
        return lines.joined(separator: "\n") + "\n"
    }

    func testConnectedSnapshot() {
        let stdout = makeStdout()
        let snap = StatusSnapshot.parse(stdout: stdout, exitCode: 0)
        XCTAssertEqual(snap.instanceID, "mbp-connector")
        XCTAssertEqual(snap.version, "0.1.0")
        XCTAssertEqual(snap.toolNames, ["notes_search", "notes_create", "reminders_list"])
        XCTAssertEqual(snap.toolCount, 3)
        XCTAssertTrue(snap.hubLine.hasPrefix("connected — hub shows 3 tool(s)"))
        XCTAssertEqual(snap.hubState, .connected)
    }

    func testDisabledToolReflectedInToolList() {
        // Engine status reflects disabled_tools: only enabled tools listed.
        let stdout = makeStdout(tools: "notes_search, reminders_list",
                                hub: "connected — hub shows 2 tool(s), matches local set")
        let snap = StatusSnapshot.parse(stdout: stdout, exitCode: 0)
        XCTAssertEqual(snap.toolNames, ["notes_search", "reminders_list"])
        XCTAssertEqual(snap.toolCount, 2)
        XCTAssertEqual(snap.hubState, .connected)
    }

    func testNotConnected() {
        let stdout = makeStdout(hub: "not connected — instance not found among 0 hub session(s)")
        let snap = StatusSnapshot.parse(stdout: stdout, exitCode: 0)
        XCTAssertEqual(snap.hubState, .notConnected)
    }

    func testAuthFailed() {
        let stdout = makeStdout(hub: "authentication failed — the server rejected the token")
        let snap = StatusSnapshot.parse(stdout: stdout, exitCode: 0)
        XCTAssertEqual(snap.hubState, .authFailed)
    }

    func testUnreachable() {
        let stdout = makeStdout(hub: "unreachable — dial tcp 127.0.0.1:1: connect: connection refused")
        let snap = StatusSnapshot.parse(stdout: stdout, exitCode: 0)
        XCTAssertEqual(snap.hubState, .unreachable)
    }

    func testNoToolsPlatformNote() {
        let stdout = makeStdout(tools: nil, hub: "not connected — instance not found among 0 hub session(s)")
        let snap = StatusSnapshot.parse(stdout: stdout, exitCode: 0)
        XCTAssertTrue(snap.toolNames.isEmpty)
        XCTAssertEqual(snap.toolCount, 0)
    }

    func testMissingConfigNonZeroExit() {
        let snap = StatusSnapshot.parse(stdout: "", exitCode: 1)
        XCTAssertEqual(snap.hubState, .missingConfig)
        XCTAssertTrue(snap.toolNames.isEmpty)
        XCTAssertFalse(snap.hubLine.isEmpty)
    }

    func testGarbageOutput() {
        let snap = StatusSnapshot.parse(stdout: "this is not status output\njust noise\n", exitCode: 0)
        XCTAssertEqual(snap.instanceID, "")
        XCTAssertEqual(snap.toolNames, [])
        XCTAssertEqual(snap.hubState, .unknown)
        XCTAssertEqual(snap.hubLine, "")
    }

    func testEmptyOutput() {
        let snap = StatusSnapshot.parse(stdout: "", exitCode: 0)
        XCTAssertEqual(snap.hubState, .unknown)
        XCTAssertEqual(snap.toolNames, [])
    }

    func testEngineNotRunningFixture() {
        let snap = StatusSnapshot.engineNotRunning
        XCTAssertEqual(snap.hubState, .unknown)
        XCTAssertEqual(snap.hubLine, "Engine not running")
        XCTAssertEqual(snap.toolCount, 0)
    }
}
