import XCTest
@testable import MemoryConnector

/// The engine must expose its loopback management API so the hosted-MCP-server
/// UI can reach it. Asserted on the argument list (no process spawned).
final class EngineArgumentsTests: XCTestCase {

    func testLaunchArgumentsIncludeManagementAPIPort() {
        let arguments = EngineManager.launchArguments(configPath: "/tmp/memory-connector.yml")

        XCTAssertEqual(Array(arguments.prefix(3)), ["relay", "--config", "/tmp/memory-connector.yml"])
        XCTAssertTrue(arguments.contains("--api-port"), "engine must expose the management API")

        guard let index = arguments.firstIndex(of: "--api-port"),
              arguments.indices.contains(index + 1) else {
            return XCTFail("--api-port missing a value: \(arguments)")
        }
        XCTAssertEqual(arguments[index + 1], "8931")
        XCTAssertEqual(arguments[index + 1], String(EngineManager.managementAPIPort))
    }
}
