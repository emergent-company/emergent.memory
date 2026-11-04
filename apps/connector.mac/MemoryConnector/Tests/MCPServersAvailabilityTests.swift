@testable import MemoryConnector
import XCTest

/// The Hosted MCP Servers page's availability rule and its user-facing error
/// mapping: internal engine/transport detail must never reach the page.
final class MCPServersAvailabilityTests: XCTestCase {
    // MARK: - Availability resolution

    @MainActor
    func testRunningWithConnectedProjectIsAvailable() {
        XCTAssertEqual(
            MCPServersAvailability.resolve(engineState: .running, hasConnectedProject: true),
            .available
        )
    }

    @MainActor
    func testRunningWithoutConnectedProjectPromptsConnect() {
        XCTAssertEqual(
            MCPServersAvailability.resolve(engineState: .running, hasConnectedProject: false),
            .noConnectedProject
        )
    }

    @MainActor
    func testStartingAndRestartingAreConnecting() {
        XCTAssertEqual(
            MCPServersAvailability.resolve(engineState: .starting, hasConnectedProject: true),
            .connecting
        )
        XCTAssertEqual(
            MCPServersAvailability.resolve(engineState: .restarting, hasConnectedProject: true),
            .connecting
        )
    }

    @MainActor
    func testStoppedIsUnavailableWithProjectElseConnect() {
        XCTAssertEqual(
            MCPServersAvailability.resolve(engineState: .stopped, hasConnectedProject: true),
            .unavailable
        )
        XCTAssertEqual(
            MCPServersAvailability.resolve(engineState: .stopped, hasConnectedProject: false),
            .noConnectedProject
        )
    }

    @MainActor
    func testFailedIsUnavailable() {
        XCTAssertEqual(
            MCPServersAvailability.resolve(engineState: .failed, hasConnectedProject: true),
            .unavailable
        )
    }

    func testOnlyAvailableAllowsRequests() {
        XCTAssertTrue(MCPServersAvailability.available.isAvailable)
        XCTAssertFalse(MCPServersAvailability.connecting.isAvailable)
        XCTAssertFalse(MCPServersAvailability.noConnectedProject.isAvailable)
        XCTAssertFalse(MCPServersAvailability.unavailable.isAvailable)
    }

    // MARK: - Error message mapping

    @MainActor
    func testUnreachableErrorIsCollapsedToGenericMessage() {
        let message = MCPServersStore.userMessage(
            for: MCPServersAPIError.unreachable("Could not connect to the server.")
        )
        XCTAssertEqual(message, "The connector isn't available right now.")
        XCTAssertFalse(message.contains("management API"))
        XCTAssertFalse(message.contains("127.0.0.1"))
    }

    @MainActor
    func testServerErrorPassesThroughVerbatim() {
        XCTAssertEqual(
            MCPServersStore.userMessage(for: MCPServersAPIError.server(409, "mcp server \"x\" already exists")),
            "mcp server \"x\" already exists"
        )
    }
}
