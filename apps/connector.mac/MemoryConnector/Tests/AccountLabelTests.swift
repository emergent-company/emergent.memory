import XCTest
@testable import MemoryConnector

/// Label rules shared by the account switcher, menu-bar popover, and account
/// page: the environment badge must always distinguish prod from dev, and the
/// row title must never fall back to the bare initials.
final class AccountLabelTests: XCTestCase {

    func testEnvironmentLabelMapsBuiltInEnvironments() {
        let prod = Account(id: "prod:u1", environmentID: "prod", email: "a@example.test")
        let dev = Account(id: "dev:u1", environmentID: "dev", email: "b@example.test")
        XCTAssertEqual(prod.environmentLabel, "Prod")
        XCTAssertEqual(dev.environmentLabel, "Dev")
    }

    func testEnvironmentLabelFallsBackToCapitalizedID() {
        let custom = Account(id: "staging:u1", environmentID: "staging")
        XCTAssertEqual(custom.environmentLabel, "Staging")
    }

    func testDisplayTitlePrefersNameThenEmailThenEnvironment() {
        let named = Account(id: "prod:u1", environmentID: "prod",
                            email: "alice@example.test", displayName: "Alice Example")
        XCTAssertEqual(named.displayTitle, "Alice Example")

        let emailOnly = Account(id: "prod:u2", environmentID: "prod",
                                email: "bob@example.test", displayName: nil)
        XCTAssertEqual(emailOnly.displayTitle, "bob@example.test")

        // Never a generic "Signed in": fall back to the environment label.
        let blank = Account(id: "prod:u3", environmentID: "prod",
                            email: "  ", displayName: "  ")
        XCTAssertEqual(blank.displayTitle, "Prod")

        let dev = Account(id: "dev:u4", environmentID: "dev")
        XCTAssertEqual(dev.displayTitle, "Dev")
    }

    func testSwitcherTitleAlwaysShowsEmailWhenPresent() {
        // Even with a display name, the switcher identity is the email.
        let named = Account(id: "prod:u1", environmentID: "prod",
                            email: "alice@example.test", displayName: "Alice Example")
        XCTAssertEqual(named.switcherTitle, "alice@example.test")

        let emailOnly = Account(id: "prod:u2", environmentID: "prod",
                                email: "bob@example.test")
        XCTAssertEqual(emailOnly.switcherTitle, "bob@example.test")

        // No email in the index: fall back to the environment label, never
        // the bare initials or "Signed in".
        let blank = Account(id: "dev:u3", environmentID: "dev")
        XCTAssertEqual(blank.switcherTitle, "Dev")
    }

    func testSubtitleShowsEmailOnlyAlongsideADistinctName() {
        let named = Account(id: "prod:u1", environmentID: "prod",
                            email: "alice@example.test", displayName: "Alice Example")
        XCTAssertEqual(named.subtitle, "alice@example.test")

        let emailOnly = Account(id: "prod:u2", environmentID: "prod", email: "bob@example.test")
        XCTAssertNil(emailOnly.subtitle, "email is the title, not the subtitle")

        let nameless = Account(id: "prod:u3", environmentID: "prod", displayName: "Alice")
        XCTAssertNil(nameless.subtitle, "no email to show")
    }

    func testInitialsComeFromDisplayNameOrEmail() {
        let named = Account(id: "prod:u1", environmentID: "prod", displayName: "Alice Example")
        XCTAssertEqual(named.initials, "AE")

        let emailOnly = Account(id: "prod:u2", environmentID: "prod", email: "bob@example.test")
        XCTAssertEqual(emailOnly.initials, "B")

        let blank = Account(id: "prod:u3", environmentID: "prod")
        XCTAssertEqual(blank.initials, "")
    }
}
