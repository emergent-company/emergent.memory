import Foundation
@testable import MemoryConnector
import XCTest

final class EnvironmentTests: XCTestCase {
    func testProdConstants() {
        let prod = Environment.prod
        XCTAssertEqual(prod.id, "prod")
        XCTAssertEqual(prod.name, "Memory")
        XCTAssertEqual(prod.serverURL.absoluteString, "https://memory.emergent-company.ai")
        XCTAssertEqual(prod.issuer.absoluteString, "https://auth.emergent-company.ai")
        XCTAssertEqual(prod.clientID, "390138006318678019")
    }

    func testDevConstants() {
        let dev = Environment.dev
        XCTAssertEqual(dev.id, "dev")
        XCTAssertEqual(dev.name, "Memory Dev")
        // Value-shape behaviour (absent/empty/whitespace/malformed) lives in the
        // Swift Testing suite `EnvironmentDevOverridesTests`; here we only pin
        // that the real built-in is usable whatever the launch environment says.
        XCTAssertNotNil(dev.serverURL.host)
        XCTAssertNotNil(dev.issuer.host)
        XCTAssertFalse(dev.clientID.isEmpty)
    }

    func testAllIsProdThenDev() {
        XCTAssertEqual(Environment.all.map(\.id), ["prod", "dev"])
    }

    func testLookupById() {
        XCTAssertEqual(Environment.environment(id: "prod"), .prod)
        XCTAssertEqual(Environment.environment(id: "dev"), .dev)
        XCTAssertNil(Environment.environment(id: "staging"))
    }

    func testEnvironmentsAreDistinct() {
        XCTAssertNotEqual(Environment.prod, Environment.dev)
        XCTAssertNotEqual(Environment.prod.serverURL, Environment.dev.serverURL)
        XCTAssertNotEqual(Environment.prod.issuer, Environment.dev.issuer)
        XCTAssertNotEqual(Environment.prod.clientID, Environment.dev.clientID)
    }

    // MARK: - Web console links

    /// The connector mirrors the console's own scoped routing so "Open in
    /// Memory web" lands on the same project the picker just switched to.
    func testProjectWebURLUsesConsoleScope() {
        let url = Environment.prod.projectWebURL(orgID: "org-1", projectID: "proj-2")
        XCTAssertEqual(url?.absoluteString,
                       "https://memory.emergent-company.ai/o/org-1/p/proj-2/agents")
    }

    func testProjectWebURLRequiresOrgAndProject() {
        XCTAssertNil(Environment.prod.projectWebURL(orgID: nil, projectID: "proj-2"))
        XCTAssertNil(Environment.prod.projectWebURL(orgID: "  ", projectID: "proj-2"))
        XCTAssertNil(Environment.prod.projectWebURL(orgID: "org-1", projectID: ""))
    }

    /// Prod serves the console from the API origin.
    func testProdConsoleURLMatchesServerURL() {
        XCTAssertEqual(Environment.prod.consoleURL, Environment.prod.serverURL)
    }

    /// The hosted dev deployment splits them (`admin.dev…` fronts `api.dev…`),
    /// so a deep link must not reuse the API host.
    func testDevConsoleURLMapsAPIHostToAdminHost() {
        let dev = Environment.makeDev(environment: [
            "MEMORY_DEV_SERVER_URL": "https://api.dev.emergent-company.ai",
        ])

        XCTAssertEqual(dev.consoleURL.absoluteString, "https://admin.dev.emergent-company.ai")
        XCTAssertEqual(dev.projectWebURL(orgID: "org-1", projectID: "p2")?.absoluteString,
                       "https://admin.dev.emergent-company.ai/o/org-1/p/p2/agents")
    }

    /// A local gateway serves API and console on one origin.
    func testDevConsoleURLKeepsANonAPILocalOrigin() {
        let dev = Environment.makeDev(environment: [
            "MEMORY_DEV_SERVER_URL": "http://localhost:3002",
        ])

        XCTAssertEqual(dev.consoleURL.absoluteString, "http://localhost:3002")
    }

    func testDevConsoleURLCanBeOverridden() {
        let dev = Environment.makeDev(environment: [
            "MEMORY_DEV_SERVER_URL": "https://api.dev.emergent-company.ai",
            "MEMORY_DEV_CONSOLE_URL": "https://console.internal.example",
        ])

        XCTAssertEqual(dev.consoleURL.absoluteString, "https://console.internal.example")
    }

    /// A blank or unparseable override falls back to the derived host, like
    /// every other dev value.
    func testDevConsoleURLIgnoresBlankOverride() {
        let dev = Environment.makeDev(environment: [
            "MEMORY_DEV_SERVER_URL": "https://api.dev.emergent-company.ai",
            "MEMORY_DEV_CONSOLE_URL": "  ",
        ])

        XCTAssertEqual(dev.consoleURL.absoluteString, "https://admin.dev.emergent-company.ai")
    }
}
