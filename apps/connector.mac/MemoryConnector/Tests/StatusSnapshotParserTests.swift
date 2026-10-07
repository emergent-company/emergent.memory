@testable import MemoryConnector
import Testing

struct StatusSnapshotParserTests {
    private func makeStdout(instance: String = "mbp-connector",
                            version: String = "0.1.0",
                            tools: String? = "notes_search, notes_create, reminders_list",
                            hub: String = "connected — hub shows 3 tool(s), matches local set") -> String
    {
        var lines = ["memory-connector status"]
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

    @Test func connectedSnapshot() {
        let stdout = makeStdout()
        let snap = StatusSnapshot.parse(stdout: stdout, exitCode: 0)
        #expect(snap.instanceID == "mbp-connector")
        #expect(snap.version == "0.1.0")
        #expect(snap.toolNames == ["notes_search", "notes_create", "reminders_list"])
        #expect(snap.toolCount == 3)
        #expect(snap.hubLine.hasPrefix("connected — hub shows 3 tool(s)"))
        #expect(snap.hubState == .connected)
    }

    @Test func disabledToolReflectedInToolList() {
        // Engine status reflects disabled_tools: only enabled tools listed.
        let stdout = makeStdout(tools: "notes_search, reminders_list",
                                hub: "connected — hub shows 2 tool(s), matches local set")
        let snap = StatusSnapshot.parse(stdout: stdout, exitCode: 0)
        #expect(snap.toolNames == ["notes_search", "reminders_list"])
        #expect(snap.toolCount == 2)
        #expect(snap.hubState == .connected)
    }

    @Test func notConnected() {
        let stdout = makeStdout(hub: "not connected — instance not found among 0 hub session(s)")
        let snap = StatusSnapshot.parse(stdout: stdout, exitCode: 0)
        #expect(snap.hubState == .notConnected)
    }

    @Test func authFailed() {
        let stdout = makeStdout(hub: "authentication failed — the server rejected the token")
        let snap = StatusSnapshot.parse(stdout: stdout, exitCode: 0)
        #expect(snap.hubState == .authFailed)
    }

    @Test func unreachable() {
        let stdout = makeStdout(hub: "unreachable — dial tcp 127.0.0.1:1: connect: connection refused")
        let snap = StatusSnapshot.parse(stdout: stdout, exitCode: 0)
        #expect(snap.hubState == .unreachable)
    }

    @Test func noToolsPlatformNote() {
        let stdout = makeStdout(tools: nil, hub: "not connected — instance not found among 0 hub session(s)")
        let snap = StatusSnapshot.parse(stdout: stdout, exitCode: 0)
        #expect(snap.toolNames.isEmpty)
        #expect(snap.toolCount == 0)
    }

    @Test func missingConfigNonZeroExit() {
        let snap = StatusSnapshot.parse(stdout: "", exitCode: 1)
        #expect(snap.hubState == .missingConfig)
        #expect(snap.toolNames.isEmpty)
        #expect(!snap.hubLine.isEmpty)
    }

    @Test func garbageOutput() {
        let snap = StatusSnapshot.parse(stdout: "this is not status output\njust noise\n", exitCode: 0)
        #expect(snap.instanceID == "")
        #expect(snap.toolNames == [])
        #expect(snap.hubState == .unknown)
        #expect(snap.hubLine == "")
    }

    @Test func emptyOutput() {
        let snap = StatusSnapshot.parse(stdout: "", exitCode: 0)
        #expect(snap.hubState == .unknown)
        #expect(snap.toolNames == [])
    }

    @Test func engineNotRunningFixture() {
        let snap = StatusSnapshot.engineNotRunning
        #expect(snap.hubState == .unknown)
        #expect(snap.hubLine == "Engine not running")
        #expect(snap.toolCount == 0)
    }
}
