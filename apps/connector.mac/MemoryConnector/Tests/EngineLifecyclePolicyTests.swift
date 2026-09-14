import XCTest
@testable import MemoryConnector

/// The "should the engine run?" gate: engine runs iff a project is connected
/// AND its config exists. Stale configs for non-connected projects must never
/// start it.
final class EngineLifecyclePolicyTests: XCTestCase {

    private var tempRoot = FileManager.default.temporaryDirectory
    private var configURL = FileManager.default.temporaryDirectory.appendingPathComponent("config.yml")

    override func setUpWithError() throws {
        tempRoot = FileManager.default.temporaryDirectory
            .appendingPathComponent("mc-lifecycle-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: tempRoot, withIntermediateDirectories: true)
        configURL = tempRoot.appendingPathComponent("memory-connector.yml")
    }

    override func tearDownWithError() throws {
        try? FileManager.default.removeItem(at: tempRoot)
    }

    private func writeConfigFile() throws {
        try EngineConfigWriter.write(configURL: configURL, content: """
        server_url: https://api.example.test
        token: emt_x
        instance_id: host-connector
        project_id: p1
        """)
    }

    func testNoConnectedProjectDoesNotStartEvenWithStaleConfig() throws {
        try writeConfigFile() // stale config left on disk from a previous project

        let decision = EngineLifecyclePolicy.decision(connectedProjectID: nil, configURL: configURL)
        XCTAssertEqual(decision, .noConnectedProject)
        XCTAssertFalse(EngineLifecyclePolicy.shouldRun(connectedProjectID: nil, configURL: configURL))
    }

    func testConnectedProjectWithConfigRuns() throws {
        try writeConfigFile()

        let decision = EngineLifecyclePolicy.decision(connectedProjectID: "p1", configURL: configURL)
        XCTAssertEqual(decision, .run)
        XCTAssertTrue(EngineLifecyclePolicy.shouldRun(connectedProjectID: "p1", configURL: configURL))
    }

    func testConnectedProjectMissingConfigIsError() {
        // No file written.
        let decision = EngineLifecyclePolicy.decision(connectedProjectID: "p1", configURL: configURL)
        XCTAssertEqual(decision, .missingConfig(projectID: "p1"))
        XCTAssertFalse(EngineLifecyclePolicy.shouldRun(connectedProjectID: "p1", configURL: configURL))
    }

    func testConnectedProjectWithInvalidConfigIsError() throws {
        // File exists but has no server_url → not a usable config.
        try EngineConfigWriter.write(configURL: configURL, content: "token: emt_x\ninstance_id: h\n")

        let decision = EngineLifecyclePolicy.decision(connectedProjectID: "p1", configURL: configURL)
        XCTAssertEqual(decision, .missingConfig(projectID: "p1"))
        XCTAssertFalse(EngineLifecyclePolicy.shouldRun(connectedProjectID: "p1", configURL: configURL))
    }

    func testNoConnectionTakesPrecedenceOverMissingConfig() {
        // Disconnected + missing file is still "no connected project", not an
        // error: we simply keep the engine stopped.
        let decision = EngineLifecyclePolicy.decision(connectedProjectID: nil, configURL: configURL)
        XCTAssertEqual(decision, .noConnectedProject)
        XCTAssertFalse(EngineLifecyclePolicy.shouldRun(connectedProjectID: nil, configURL: configURL))
    }
}
