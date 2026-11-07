import Foundation
@testable import MemoryConnector
import XCTest

/// Recent-project bookkeeping behind the project switcher's "Recent" section.
@MainActor
final class RecentProjectsStoreTests: XCTestCase {
    private var suite = ""
    private var defaults = UserDefaults(suiteName: "") ?? .standard

    override func setUpWithError() throws {
        suite = "mc-recent-projects-\(UUID().uuidString)"
        defaults = UserDefaults(suiteName: suite) ?? .standard
    }

    override func tearDownWithError() throws {
        defaults.removePersistentDomain(forName: suite)
    }

    // MARK: - Recording

    func testRecordKeepsNewestFirstWithoutDuplicates() {
        let store = RecentProjectsStore(defaults: defaults)

        store.record("p1")
        store.record("p2")
        store.record("p1")

        XCTAssertEqual(store.ids, ["p1", "p2"])
        XCTAssertEqual(defaults.stringArray(forKey: store.key), ["p1", "p2"])
    }

    func testRecordIsCapped() {
        let store = RecentProjectsStore(defaults: defaults)

        for index in 1 ... (RecentProjectsStore.maxEntries + 3) {
            store.record("p\(index)")
        }

        XCTAssertEqual(store.ids.count, RecentProjectsStore.maxEntries)
        XCTAssertEqual(store.ids.first, "p\(RecentProjectsStore.maxEntries + 3)")
    }

    func testRecordIgnoresBlankIDs() {
        let store = RecentProjectsStore(defaults: defaults)

        store.record("")
        store.record("   ")

        XCTAssertTrue(store.ids.isEmpty)
    }

    // MARK: - Scoping

    func testApplyScopeLoadsThatAccountsListOnly() {
        let store = RecentProjectsStore(defaults: defaults)
        store.applyScope(accountID: "acct-a")
        store.record("p1")

        store.applyScope(accountID: "acct-b")
        XCTAssertEqual(store.ids, [])

        store.record("p2")
        store.applyScope(accountID: "acct-a")
        XCTAssertEqual(store.ids, ["p1"])
        store.applyScope(accountID: "acct-b")
        XCTAssertEqual(store.ids, ["p2"])
    }

    func testUnscopedListUsesTheSharedKey() {
        let store = RecentProjectsStore(defaults: defaults)

        store.record("p1")

        XCTAssertEqual(store.key, RecentProjectsStore.keyPrefix)
        XCTAssertEqual(defaults.stringArray(forKey: RecentProjectsStore.keyPrefix), ["p1"])
    }

    // MARK: - Resolution

    func testResolvedDropsProjectsThatNoLongerExist() {
        let store = RecentProjectsStore(defaults: defaults)
        store.record("p1")
        store.record("gone")

        let projects = [
            ProjectInfo(id: "p1", name: "One", orgID: nil),
            ProjectInfo(id: "p2", name: "Two", orgID: nil),
        ]

        XCTAssertEqual(store.resolved(in: projects).map(\.id), ["p1"])
    }

    func testResolvedKeepsRecordOrder() {
        let store = RecentProjectsStore(defaults: defaults)
        store.record("p1")
        store.record("p2")
        store.record("p3")

        let projects = [
            ProjectInfo(id: "p1", name: "One", orgID: nil),
            ProjectInfo(id: "p2", name: "Two", orgID: nil),
            ProjectInfo(id: "p3", name: "Three", orgID: nil),
        ]

        XCTAssertEqual(store.resolved(in: projects).map(\.id), ["p3", "p2", "p1"])
    }
}
