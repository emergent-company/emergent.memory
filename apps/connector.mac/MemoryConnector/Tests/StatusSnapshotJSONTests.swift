import XCTest
@testable import MemoryConnector

/// Tests for the `status --json` machine contract consumed by StatusMonitor.
final class StatusSnapshotJSONTests: XCTestCase {

    private func json(instance: String = "mbp-connector",
                      version: String = "0.1.0",
                      tools: [String] = ["linux-host-info"],
                      hubState: String = "connected",
                      hubToolCount: Int = 1,
                      localToolCount: Int = 1,
                      sessionCount: Int = 1,
                      error: String? = nil,
                      disabledTools: [[String: String]] = []) -> String {
        var detail: [String: Any] = [
            "hub_tool_count": hubToolCount,
            "local_tool_count": localToolCount,
            "session_count": sessionCount,
        ]
        if let error { detail["error"] = error }
        var doc: [String: Any] = [
            "schema_version": 1,
            "instance_id": instance,
            "version": version,
            "tools": tools,
            "hub_state": hubState,
            "hub_detail": detail,
        ]
        if !disabledTools.isEmpty { doc["disabled_tools"] = disabledTools }
        let data = try! JSONSerialization.data(withJSONObject: doc, options: [.sortedKeys])
        return String(data: data, encoding: .utf8)!
    }

    func testJSONConnected() {
        let snap = StatusSnapshot.parse(stdout: json(), exitCode: 0)
        XCTAssertEqual(snap.instanceID, "mbp-connector")
        XCTAssertEqual(snap.version, "0.1.0")
        XCTAssertEqual(snap.toolNames, ["linux-host-info"])
        XCTAssertEqual(snap.toolCount, 1)
        XCTAssertEqual(snap.hubState, .connected)
        XCTAssertEqual(snap.hubLine, "connected — hub shows 1 tool(s), matches local set")
    }

    func testJSONConnectedMismatch() {
        let snap = StatusSnapshot.parse(stdout: json(hubToolCount: 0, localToolCount: 5), exitCode: 0)
        XCTAssertEqual(snap.hubState, .connected)
        XCTAssertTrue(snap.hubLine.contains("mismatch"))
    }

    func testJSONNotConnected() {
        let snap = StatusSnapshot.parse(
            stdout: json(tools: [], hubState: "not_connected", sessionCount: 3), exitCode: 0)
        XCTAssertEqual(snap.hubState, .notConnected)
        XCTAssertTrue(snap.toolNames.isEmpty)
        XCTAssertEqual(snap.hubLine, "not connected — instance not found among 3 hub session(s)")
    }

    func testJSONAuthFailed() {
        let snap = StatusSnapshot.parse(stdout: json(hubState: "auth_failed"), exitCode: 0)
        XCTAssertEqual(snap.hubState, .authFailed)
        XCTAssertEqual(snap.hubLine, "authentication failed — the server rejected the token")
    }

    func testJSONUnreachable() {
        let snap = StatusSnapshot.parse(
            stdout: json(hubState: "unreachable", error: "dial tcp: refused"), exitCode: 0)
        XCTAssertEqual(snap.hubState, .unreachable)
        XCTAssertEqual(snap.hubLine, "unreachable — dial tcp: refused")
    }

    func testJSONMissingConfig() {
        let snap = StatusSnapshot.parse(stdout: json(hubState: "missing_config"), exitCode: 0)
        XCTAssertEqual(snap.hubState, .missingConfig)
    }

    func testJSONUnknownHubStateIsUnknown() {
        let snap = StatusSnapshot.parse(stdout: json(hubState: "something_new"), exitCode: 0)
        XCTAssertEqual(snap.hubState, .unknown)
    }

    func testJSONDisabledTools() {
        let snap = StatusSnapshot.parse(
            stdout: json(tools: ["linux-host-info"],
                         disabledTools: [
                            ["name": "linux-fs-write", "reason": "no write-capable root configured"],
                         ]),
            exitCode: 0)
        XCTAssertEqual(snap.disabledTools, [
            StatusSnapshot.DisabledTool(name: "linux-fs-write",
                                        reason: "no write-capable root configured"),
        ])
    }

    func testNonZeroExitIsMissingConfig() {
        let snap = StatusSnapshot.parse(stdout: json(), exitCode: 1)
        XCTAssertEqual(snap.hubState, .missingConfig)
    }

    func testMalformedJSONFallsBackGracefully() {
        // Starts with "{" but is not valid JSON: must not crash and must not be
        // mistaken for the text form.
        let snap = StatusSnapshot.parse(stdout: "{not json", exitCode: 0)
        XCTAssertEqual(snap.hubState, .unknown)
        XCTAssertTrue(snap.toolNames.isEmpty)
    }

    func testTextFormStillParses() {
        // The legacy text form remains supported (older engine binary).
        let stdout = """
        memory-connector status
        instance: old-connector
        version: 0.1.0
        tools (1): notes_search
        hub: connected — hub shows 1 tool(s), matches local set
        """
        let snap = StatusSnapshot.parse(stdout: stdout, exitCode: 0)
        XCTAssertEqual(snap.instanceID, "old-connector")
        XCTAssertEqual(snap.toolNames, ["notes_search"])
        XCTAssertEqual(snap.hubState, .connected)
    }
}
