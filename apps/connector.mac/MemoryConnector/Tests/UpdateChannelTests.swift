@testable import MemoryConnector
import XCTest

/// Update-channel selection: the per-channel Sparkle feed resolution and the
/// persistence of the user's chosen channel.
final class UpdateChannelTests: XCTestCase {
    private var suiteName = ""
    private var defaults: UserDefaults!

    override func setUpWithError() throws {
        suiteName = "mc-updatechannel-\(UUID().uuidString)"
        defaults = UserDefaults(suiteName: suiteName)!
    }

    override func tearDownWithError() throws {
        defaults.removePersistentDomain(forName: suiteName)
    }

    // MARK: - Feed URL resolution

    @MainActor
    func testFeedURLStringReturnsPerChannelURL() {
        let info: [String: Any] = [
            "MemoryUpdateFeedURLStable": "https://example.test/stable/appcast.xml",
            "MemoryUpdateFeedURLDev": "https://example.test/dev/appcast.xml",
        ]

        XCTAssertEqual(
            UpdaterModel.feedURLString(for: .stable, infoDictionary: info),
            "https://example.test/stable/appcast.xml"
        )
        XCTAssertEqual(
            UpdaterModel.feedURLString(for: .dev, infoDictionary: info),
            "https://example.test/dev/appcast.xml"
        )
    }

    @MainActor
    func testFeedURLStringFallsBackToSUFeedURLForStableOnly() {
        let info: [String: Any] = [
            "SUFeedURL": "https://example.test/legacy/appcast.xml",
        ]

        // Stable tolerates the legacy single-feed key...
        XCTAssertEqual(
            UpdaterModel.feedURLString(for: .stable, infoDictionary: info),
            "https://example.test/legacy/appcast.xml"
        )
        // ...but dev must never silently follow the stable feed.
        XCTAssertNil(UpdaterModel.feedURLString(for: .dev, infoDictionary: info))
    }

    @MainActor
    func testFeedURLStringRejectsMissingEmptyAndNonHTTPS() {
        XCTAssertNil(UpdaterModel.feedURLString(for: .stable, infoDictionary: [:]))

        let empty: [String: Any] = ["MemoryUpdateFeedURLStable": "   "]
        XCTAssertNil(UpdaterModel.feedURLString(for: .stable, infoDictionary: empty))

        let insecure: [String: Any] = ["MemoryUpdateFeedURLDev": "http://example.test/appcast.xml"]
        XCTAssertNil(UpdaterModel.feedURLString(for: .dev, infoDictionary: insecure))
    }

    @MainActor
    func testFeedURLStringTrimsWhitespace() {
        let info: [String: Any] = [
            "MemoryUpdateFeedURLDev": "  https://example.test/dev/appcast.xml\n",
        ]
        XCTAssertEqual(
            UpdaterModel.feedURLString(for: .dev, infoDictionary: info),
            "https://example.test/dev/appcast.xml"
        )
    }

    // MARK: - Persistence

    @MainActor
    func testUpdateChannelDefaultsToStable() {
        let settings = ConnectorSettings(defaults: defaults)
        XCTAssertEqual(settings.updateChannel, .stable)
        XCTAssertEqual(UpdaterModel.feedURLString(for: settings.updateChannel, infoDictionary: [
            "MemoryUpdateFeedURLStable": "https://example.test/stable/appcast.xml",
        ]), "https://example.test/stable/appcast.xml")
    }

    @MainActor
    func testSetUpdateChannelPersistsAcrossReload() {
        let settings = ConnectorSettings(defaults: defaults)
        settings.setUpdateChannel(.dev)
        XCTAssertEqual(settings.updateChannel, .dev)

        let reloaded = ConnectorSettings(defaults: defaults)
        XCTAssertEqual(reloaded.updateChannel, .dev)
    }

    @MainActor
    func testSetUpdateChannelIsNoOpWhenUnchanged() {
        let settings = ConnectorSettings(defaults: defaults)
        settings.setUpdateChannel(.stable)
        // Nothing persisted yet because the value did not change.
        XCTAssertNil(defaults.string(forKey: "connector.updateChannel"))
    }

    @MainActor
    func testClearConfigurationResetsChannelToStable() {
        let settings = ConnectorSettings(defaults: defaults)
        settings.setUpdateChannel(.dev)
        settings.clearConfiguration()

        XCTAssertEqual(settings.updateChannel, .stable)
        XCTAssertEqual(ConnectorSettings(defaults: defaults).updateChannel, .stable)
    }
}
