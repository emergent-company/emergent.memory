import XCTest
@testable import MemoryConnector

final class ProjectProfileStoreTests: XCTestCase {

    private var suiteName = ""
    private var defaults: UserDefaults!

    override func setUpWithError() throws {
        suiteName = "mc-profiles-\(UUID().uuidString)"
        defaults = UserDefaults(suiteName: suiteName)!
    }

    override func tearDownWithError() throws {
        defaults.removePersistentDomain(forName: suiteName)
    }

    func testRoundTripAndAllIDs() {
        let store = ProjectProfileStore(defaults: defaults)
        store.save(ProjectProfile(disabledTools: ["notes_create"], instanceID: "p1-connector"), for: "p1")
        store.save(ProjectProfile(disabledTools: [], instanceID: nil), for: "p2")

        XCTAssertEqual(store.allProjectIDs(), ["p1", "p2"])
        XCTAssertEqual(store.profile(for: "p1"),
                       ProjectProfile(disabledTools: ["notes_create"], instanceID: "p1-connector"))
        XCTAssertEqual(store.profile(for: "p2"), ProjectProfile(disabledTools: [], instanceID: nil))
    }

    func testIsolationBetweenProjects() {
        let store = ProjectProfileStore(defaults: defaults)
        store.save(ProjectProfile(disabledTools: ["a"], instanceID: "A"), for: "p1")
        store.save(ProjectProfile(disabledTools: ["b"], instanceID: "B"), for: "p2")
        store.save(ProjectProfile(disabledTools: ["c"], instanceID: "C"), for: "p1")

        XCTAssertEqual(store.profile(for: "p1")?.disabledTools, ["c"])
        XCTAssertEqual(store.profile(for: "p2")?.disabledTools, ["b"])
        XCTAssertEqual(store.profile(for: "p2")?.instanceID, "B")
    }

    func testMissingKeyIsEmpty() {
        let store = ProjectProfileStore(defaults: defaults)
        XCTAssertNil(store.profile(for: "nope"))
        XCTAssertTrue(store.allProjectIDs().isEmpty)
    }

    // MARK: - All-tools-OFF default for brand-new projects

    func testNewProfileDefaultsToAllCatalogDisabled() {
        let profile = ProjectProfileStore.newProfile(instanceID: "p1-connector")
        XCTAssertEqual(Set(profile.disabledTools), Set(ToolCatalog.tools.map(\.id)))
        XCTAssertEqual(profile.instanceID, "p1-connector")
    }

    func testProfileOrDefaultFallsBackToAllDisabledWithoutPersisting() {
        let store = ProjectProfileStore(defaults: defaults)
        let fresh = store.profileOrDefault(for: "nope")

        XCTAssertEqual(Set(fresh.disabledTools), Set(ToolCatalog.tools.map(\.id)))
        XCTAssertNil(fresh.instanceID)
        XCTAssertNil(store.profile(for: "nope"), "fallback must not be written")
        XCTAssertTrue(store.allProjectIDs().isEmpty)
    }

    func testStoredProfileWinsOverDefault() {
        let store = ProjectProfileStore(defaults: defaults)
        store.save(ProjectProfile(disabledTools: [], instanceID: "p1-connector"), for: "p1")

        XCTAssertEqual(store.profileOrDefault(for: "p1").disabledTools, [],
                       "an explicitly stored empty (all ON) set is respected")
    }

    func testCorruptBlobIsToleratedAndRecoverable() throws {
        defaults.set(Data("not json at all".utf8), forKey: ProjectProfileStore.key)
        let store = ProjectProfileStore(defaults: defaults)

        XCTAssertNil(store.profile(for: "p1"))
        XCTAssertTrue(store.allProjectIDs().isEmpty)

        // Saving over a corrupt blob recovers cleanly.
        store.save(ProjectProfile(disabledTools: ["reminders_list"], instanceID: "X"), for: "p1")
        XCTAssertEqual(store.profile(for: "p1")?.instanceID, "X")
    }

    func testClearRemovesOnlyTargetProject() {
        let store = ProjectProfileStore(defaults: defaults)
        store.save(ProjectProfile(disabledTools: [], instanceID: "A"), for: "p1")
        store.save(ProjectProfile(disabledTools: [], instanceID: "B"), for: "p2")

        store.clear(projectID: "p1")

        XCTAssertNil(store.profile(for: "p1"))
        XCTAssertEqual(store.profile(for: "p2")?.instanceID, "B")
        XCTAssertEqual(store.allProjectIDs(), ["p2"])
    }

    // MARK: - Explicit connection

    func testConnectedDefaultsFalseAndRoundTrips() {
        let store = ProjectProfileStore(defaults: defaults)
        store.save(ProjectProfile(disabledTools: [], instanceID: "p1"), for: "p1")
        XCTAssertEqual(store.profile(for: "p1")?.connected, false)
        XCTAssertNil(store.connectedProjectID())

        store.setConnected(true, for: "p1")
        XCTAssertEqual(store.profile(for: "p1")?.connected, true)
        XCTAssertEqual(store.connectedProjectID(), "p1")
    }

    func testSetConnectedClearsEveryOtherProject() {
        let store = ProjectProfileStore(defaults: defaults)
        store.setConnected(true, for: "p1")
        store.setConnected(true, for: "p2")

        XCTAssertEqual(store.connectedProjectID(), "p2")
        XCTAssertEqual(store.profile(for: "p1")?.connected, false)
        XCTAssertEqual(store.profile(for: "p2")?.connected, true)

        store.setConnected(false, for: "p2")
        XCTAssertNil(store.connectedProjectID())
    }

    func testSetConnectedCreatesAllDisabledProfileWhenAbsent() {
        let store = ProjectProfileStore(defaults: defaults)
        let profile = store.setConnected(true, for: "new")

        XCTAssertEqual(profile.connected, true)
        XCTAssertEqual(Set(profile.disabledTools), Set(ToolCatalog.tools.map(\.id)))
        XCTAssertNil(profile.instanceID)
    }

    func testLegacyStoredProfileWithoutConnectedDecodesFalse() throws {
        // A profile written before `connected` existed must decode as false and
        // keep its disabledTools/instanceID.
        let legacy = #"{"p1":{"disabledTools":["notes_create"],"instanceID":"p1-connector"}}"#
        defaults.set(Data(legacy.utf8), forKey: ProjectProfileStore.key)

        let store = ProjectProfileStore(defaults: defaults)
        let profile = store.profile(for: "p1")
        XCTAssertEqual(profile?.connected, false)
        XCTAssertEqual(profile?.disabledTools, ["notes_create"])
        XCTAssertEqual(profile?.instanceID, "p1-connector")
        XCTAssertNil(store.connectedProjectID())
    }

    // MARK: - All tools in one call

    func testSetAllToolsSetsWholeCatalogAndPreservesOtherFields() {
        let store = ProjectProfileStore(defaults: defaults)
        store.setConnected(true, for: "p1")
        store.save(ProjectProfile(disabledTools: ["notes_create"], instanceID: "p1-instance",
                                  connected: true), for: "p1")

        let disabled = store.setAllTools(enabled: false, for: "p1")
        XCTAssertEqual(Set(disabled.disabledTools), Set(ToolCatalog.tools.map(\.id)))
        XCTAssertEqual(disabled.instanceID, "p1-instance")
        XCTAssertEqual(disabled.connected, true)

        let enabled = store.setAllTools(enabled: true, for: "p1")
        XCTAssertEqual(enabled.disabledTools, [])
        XCTAssertEqual(store.profile(for: "p1")?.connected, true)
    }
}
