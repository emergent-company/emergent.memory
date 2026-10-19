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
        let env = ProcessInfo.processInfo.environment
        let dev = Environment.dev
        XCTAssertEqual(dev.id, "dev")
        XCTAssertEqual(dev.name, "Memory Dev")
        XCTAssertEqual(dev.serverURL.absoluteString, env["MEMORY_DEV_SERVER_URL"] ?? "https://api.example.com")
        XCTAssertEqual(dev.issuer.absoluteString, env["MEMORY_DEV_ISSUER_URL"] ?? "https://zitadel.example.com")
        XCTAssertEqual(dev.clientID, env["MEMORY_DEV_CLIENT_ID"] ?? "390138928478289930")
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
}
