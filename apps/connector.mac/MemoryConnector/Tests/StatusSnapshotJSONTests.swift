import Foundation
@testable import MemoryConnector
import Testing

/// Tests for the `status --json` machine contract consumed by StatusMonitor.
struct StatusSnapshotJSONTests {
    private func json(instance: String = "mbp-connector",
                      version: String = "0.1.0",
                      tools: [String] = ["linux-host-info"],
                      hubState: String = "connected",
                      hubToolCount: Int = 1,
                      localToolCount: Int = 1,
                      sessionCount: Int = 1,
                      error: String? = nil,
                      disabledTools: [[String: String]] = []) -> String
    {
        var detail: [String: Any] = [
            "hub_tool_count": hubToolCount,
            "local_tool_count": localToolCount,
            "session_count": sessionCount,
        ]
        if let error {
            detail["error"] = error
        }
        var doc: [String: Any] = [
            "schema_version": 1,
            "instance_id": instance,
            "version": version,
            "tools": tools,
            "hub_state": hubState,
            "hub_detail": detail,
        ]
        if !disabledTools.isEmpty {
            doc["disabled_tools"] = disabledTools
        }
        guard let data = try? JSONSerialization.data(withJSONObject: doc, options: [.sortedKeys]),
              let string = String(bytes: data, encoding: .utf8)
        else {
            Issue.record("status JSON fixture is not serializable")
            return "{}"
        }
        return string
    }

    @Test func jsonConnected() {
        let snap = StatusSnapshot.parse(stdout: json(), exitCode: 0)
        #expect(snap.instanceID == "mbp-connector")
        #expect(snap.version == "0.1.0")
        #expect(snap.toolNames == ["linux-host-info"])
        #expect(snap.toolCount == 1)
        #expect(snap.hubState == .connected)
        #expect(snap.hubLine == "connected — hub shows 1 tool(s), matches local set")
    }

    @Test func jsonConnectedMismatch() {
        let snap = StatusSnapshot.parse(stdout: json(hubToolCount: 0, localToolCount: 5), exitCode: 0)
        #expect(snap.hubState == .connected)
        #expect(snap.hubLine.contains("mismatch"))
    }

    @Test func jsonNotConnected() {
        let snap = StatusSnapshot.parse(
            stdout: json(tools: [], hubState: "not_connected", sessionCount: 3), exitCode: 0
        )
        #expect(snap.hubState == .notConnected)
        #expect(snap.toolNames.isEmpty)
        #expect(snap.hubLine == "not connected — instance not found among 3 hub session(s)")
    }

    @Test func jsonAuthFailed() {
        let snap = StatusSnapshot.parse(stdout: json(hubState: "auth_failed"), exitCode: 0)
        #expect(snap.hubState == .authFailed)
        #expect(snap.hubLine == "authentication failed — the server rejected the token")
    }

    @Test func jsonUnreachable() {
        let snap = StatusSnapshot.parse(
            stdout: json(hubState: "unreachable", error: "dial tcp: refused"), exitCode: 0
        )
        #expect(snap.hubState == .unreachable)
        #expect(snap.hubLine == "unreachable — dial tcp: refused")
    }

    @Test func jsonMissingConfig() {
        let snap = StatusSnapshot.parse(stdout: json(hubState: "missing_config"), exitCode: 0)
        #expect(snap.hubState == .missingConfig)
    }

    @Test func jsonUnknownHubStateIsUnknown() {
        let snap = StatusSnapshot.parse(stdout: json(hubState: "something_new"), exitCode: 0)
        #expect(snap.hubState == .unknown)
    }

    @Test func jsonDisabledTools() {
        let snap = StatusSnapshot.parse(
            stdout: json(tools: ["linux-host-info"],
                         disabledTools: [
                             ["name": "linux-fs-write", "reason": "no write-capable root configured"],
                         ]),
            exitCode: 0
        )
        #expect(snap.disabledTools == [
            StatusSnapshot.DisabledTool(name: "linux-fs-write",
                                        reason: "no write-capable root configured"),
        ])
    }

    @Test func nonZeroExitIsMissingConfig() {
        let snap = StatusSnapshot.parse(stdout: json(), exitCode: 1)
        #expect(snap.hubState == .missingConfig)
    }

    @Test func malformedJSONFallsBackGracefully() {
        // Starts with "{" but is not valid JSON: must not crash and must not be
        // mistaken for the text form.
        let snap = StatusSnapshot.parse(stdout: "{not json", exitCode: 0)
        #expect(snap.hubState == .unknown)
        #expect(snap.toolNames.isEmpty)
    }

    @Test func textFormStillParses() {
        // The legacy text form remains supported (older engine binary).
        let stdout = """
        memory-connector status
        instance: old-connector
        version: 0.1.0
        tools (1): notes_search
        hub: connected — hub shows 1 tool(s), matches local set
        """
        let snap = StatusSnapshot.parse(stdout: stdout, exitCode: 0)
        #expect(snap.instanceID == "old-connector")
        #expect(snap.toolNames == ["notes_search"])
        #expect(snap.hubState == .connected)
    }
}
