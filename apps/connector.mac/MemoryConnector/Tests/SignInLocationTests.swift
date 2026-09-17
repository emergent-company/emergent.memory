import XCTest
@testable import MemoryConnector

/// Guards the per-surface sign-in policy: Production is the primary target and
/// is offered everywhere, Development is offered only by About, and the
/// Development environment itself still exists for already-signed-in accounts.
final class SignInLocationTests: XCTestCase {

    func testPrimaryIsProduction() {
        XCTAssertEqual(Environment.primary, .prod)
    }

    func testEveryLocationOffersPrimary() {
        for location in SignInLocation.allCases {
            XCTAssertTrue(Environment.signInEnvironments(for: location).contains(.primary),
                          "\(location) must offer the primary environment")
        }
    }

    func testDevelopmentIsOfferedOnlyByAbout() {
        for location in SignInLocation.allCases {
            let offered = Environment.signInEnvironments(for: location)
            if location == .about {
                XCTAssertTrue(offered.contains(.dev), "About must offer Development")
            } else {
                XCTAssertFalse(offered.contains(.dev),
                               "\(location) must not offer Development")
            }
        }
    }

    func testPrimarySurfacesOfferProductionOnly() {
        for location in SignInLocation.allCases where location != .about {
            XCTAssertEqual(Environment.signInEnvironments(for: location).map(\.id), ["prod"],
                           "\(location) must offer Production only")
        }
    }

    func testAboutOffersBothBuiltIns() {
        XCTAssertEqual(Environment.signInEnvironments(for: .about).map(\.id), ["prod", "dev"])
    }

    func testAllStillContainsBothBuiltIns() {
        XCTAssertEqual(Environment.all.map(\.id), ["prod", "dev"])
        XCTAssertTrue(Environment.all.contains(.prod))
        XCTAssertTrue(Environment.all.contains(.dev))
    }
}
