@testable import MemoryConnector
import XCTest

/// The aggregate `StatusMonitor` snapshot across multiple per-project engine
/// children: hub-state merge and the count-based hub line.
final class StatusMonitorAggregateTests: XCTestCase {
    private func snapshot(_ hubState: StatusSnapshot.HubState,
                          tools: [String] = ["notes_create"]) -> StatusSnapshot
    {
        StatusSnapshot(instanceID: "inst",
                       version: "0.1.0",
                       toolNames: tools,
                       hubLine: "",
                       hubState: hubState)
    }

    func testAggregateEmptySetIsNil() {
        XCTAssertNil(StatusMonitor.aggregate([:]))
    }

    func testAggregateSingleProject() {
        let aggregate = StatusMonitor.aggregate(["p1": snapshot(.connected)])
        XCTAssertEqual(aggregate?.hubState, .connected)
        XCTAssertEqual(aggregate?.hubLine, "1 project connected")
        XCTAssertEqual(aggregate?.instanceID, "inst")
        XCTAssertEqual(aggregate?.toolNames, ["notes_create"])
    }

    func testAggregateAllConnectedMergesToolNames() {
        let aggregate = StatusMonitor.aggregate([
            "p1": snapshot(.connected, tools: ["notes_create"]),
            "p2": snapshot(.connected, tools: ["reminders_list", "notes_create"]),
        ])
        XCTAssertEqual(aggregate?.hubState, .connected)
        XCTAssertEqual(aggregate?.hubLine, "2 projects connected")
        XCTAssertEqual(aggregate?.toolNames, ["notes_create", "reminders_list"])
        XCTAssertEqual(aggregate?.instanceID, "", "multi-project aggregate has no single instance id")
    }

    func testAggregateMixedStatesSurfacesAuthFailure() {
        let aggregate = StatusMonitor.aggregate([
            "p1": snapshot(.connected),
            "p2": snapshot(.authFailed),
        ])
        XCTAssertEqual(aggregate?.hubState, .authFailed)
        XCTAssertEqual(aggregate?.hubLine, "1 of 2 projects connected")
    }

    func testAggregatePartialConnectionIsNotConnected() {
        let aggregate = StatusMonitor.aggregate([
            "p1": snapshot(.connected),
            "p2": snapshot(.notConnected),
        ])
        XCTAssertEqual(aggregate?.hubState, .notConnected)
    }

    func testAggregateHubStatePrecedence() {
        XCTAssertEqual(StatusMonitor.aggregateHubState([.authFailed, .unreachable]), .authFailed)
        XCTAssertEqual(StatusMonitor.aggregateHubState([.unreachable, .missingConfig]), .unreachable)
        XCTAssertEqual(StatusMonitor.aggregateHubState([.missingConfig, .notConnected]), .missingConfig)
        XCTAssertEqual(StatusMonitor.aggregateHubState([.notConnected]), .notConnected)
        XCTAssertEqual(StatusMonitor.aggregateHubState([]), .unknown)
    }
}
