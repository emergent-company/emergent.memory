@testable import MemoryConnector
import XCTest

/// The accessible names used for empty-title and icon-only controls must never
/// be empty, so VoiceOver always has a subject to announce.
final class AccessibleNamesTests: XCTestCase {
    func testAccountNameIsNotEmpty() {
        XCTAssertFalse(String(localized: AccessibleNames.account).isEmpty)
    }

    func testAllLocalToolsNameIsNotEmpty() {
        XCTAssertFalse(String(localized: AccessibleNames.allLocalTools).isEmpty)
    }

    func testServerEnabledNamesTheServer() {
        let name = String(localized: AccessibleNames.serverEnabled("filesystem"))
        XCTAssertFalse(name.isEmpty)
        XCTAssertTrue(name.contains("filesystem"))
    }

    func testServerEnabledFallsBackWhenNameIsBlank() {
        XCTAssertEqual(String(localized: AccessibleNames.serverEnabled("  ")), "Enable server")
        XCTAssertEqual(String(localized: AccessibleNames.serverEnabled("")), "Enable server")
    }

    func testProjectConnectionNamesTheProject() {
        let name = String(localized: AccessibleNames.projectConnection("Demo"))
        XCTAssertFalse(name.isEmpty)
        XCTAssertTrue(name.contains("Demo"))
    }

    func testProjectConnectionFallsBackWhenNoProject() {
        XCTAssertEqual(String(localized: AccessibleNames.projectConnection(nil)), "Connect project")
        XCTAssertEqual(String(localized: AccessibleNames.projectConnection("  ")), "Connect project")
    }
}
