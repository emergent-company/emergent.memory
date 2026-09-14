import Foundation
import Testing
import UIKit
@testable import Memory

/// Tests for the device self-introduction manifest sent during one-time setup
/// (OpenSpec "device-self-introduction", iOS tasks 3.2/3.3/4.2).
///
/// The suite is `@MainActor` because `DeviceInfo` snapshots UIKit/Foundation
/// singletons (`UIDevice.current`, `Bundle.main`); the app target defaults new
/// declarations to `MainActor` isolation.
@Suite
@MainActor
struct DeviceInfoTests {
    // MARK: - DeviceInfo (tasks 3.2, 3.3)

    /// A live provider must always fill the core identifying fields. The exact
    /// device string depends on the simulator/host, so only non-emptiness is
    /// asserted.
    @Test func liveManifestHasCoreFields() {
        let manifest = DeviceInfo().manifest
        #expect(manifest.platform?.isEmpty == false)
        #expect(manifest.modelId?.isEmpty == false)
        #expect(manifest.osName?.isEmpty == false)
        #expect(manifest.osVersion?.isEmpty == false)
    }

    /// The platform value must match the running environment's derivation
    /// (`.pad` idiom ⇒ "ipados", otherwise "ios").
    @Test func livePlatformMatchesEnvironmentIdiom() {
        let info = DeviceInfo()
        let expected = UIDevice.current.userInterfaceIdiom == .pad ? "ipados" : "ios"
        #expect(info.manifest.platform == expected)
    }

    /// Pure derivation rule: idiom wins; systemName containing "iPad" is the
    /// tiebreak when the idiom is not `.pad`/`.mac`.
    @Test func derivesPlatformFromSystemNameAndIdiom() {
        #expect(DeviceInfo.platform(systemName: "iOS", userInterfaceIdiom: .phone) == "ios")
        #expect(DeviceInfo.platform(systemName: "iOS", userInterfaceIdiom: .pad) == "ipados")
        #expect(DeviceInfo.platform(systemName: "iPadOS", userInterfaceIdiom: .pad) == "ipados")
        #expect(DeviceInfo.platform(systemName: "iOS", userInterfaceIdiom: .mac) == "macos")
    }

    /// The form factor must follow the idiom.
    @Test func derivesFormFactorFromIdiom() {
        #expect(DeviceInfo.formFactor(for: .phone) == "phone")
        #expect(DeviceInfo.formFactor(for: .pad) == "tablet")
        #expect(DeviceInfo.formFactor(for: .mac) == "desktop")
    }

    @Test func liveFormFactorMatchesEnvironmentIdiom() {
        let info = DeviceInfo()
        #expect(info.manifest.formFactor == DeviceInfo.formFactor(for: UIDevice.current.userInterfaceIdiom))
    }

    /// Known hardware identifiers map to their marketing name.
    @Test func knownModelMapsToMarketingName() {
        #expect(DeviceInfo.modelDisplay(for: "iPhone15,2") == "iPhone 14 Pro")
        #expect(DeviceInfo.modelDisplay(for: "iPad14,1") == "iPad mini (6th generation)")
    }

    /// Unmapped identifiers fall back to the raw id (the durable field).
    @Test func unknownModelFallsBackToRawIdentifier() {
        #expect(DeviceInfo.modelDisplay(for: "AppleTV6,2") == "AppleTV6,2")
    }

    // MARK: - MemorySetupClient payload encoding (task 4.2)

    /// The setup request body must nest the full manifest under the `device`
    /// key with the exact camelCase keys from the contract.
    @Test func setupPayloadEncodesDeviceManifest() throws {
        let manifest = DeviceManifest(
            platform: "ios",
            formFactor: "phone",
            name: "Test iPhone",
            modelId: "iPhone15,2",
            modelDisplay: "iPhone 14 Pro",
            osName: "iOS",
            osVersion: "18.0",
            appVersion: "1.2.3",
            appBuild: "7"
        )
        let data = try JSONEncoder().encode(MemorySetupClient.Payload(token: "tok123", device: manifest))
        let object = try JSONSerialization.jsonObject(with: data)
        let json = try #require(object as? [String: Any])

        #expect(json["token"] as? String == "tok123")
        let device = try #require(json["device"] as? [String: Any])
        #expect(device["platform"] as? String == "ios")
        #expect(device["formFactor"] as? String == "phone")
        #expect(device["name"] as? String == "Test iPhone")
        #expect(device["modelId"] as? String == "iPhone15,2")
        #expect(device["modelDisplay"] as? String == "iPhone 14 Pro")
        #expect(device["osName"] as? String == "iOS")
        #expect(device["osVersion"] as? String == "18.0")
        #expect(device["appVersion"] as? String == "1.2.3")
        #expect(device["appBuild"] as? String == "7")
        #expect(device.count == 9)
    }

    /// `device` is optional: a nil manifest must keep the body `{"token": ...}`
    /// exactly like today (backward compatible).
    @Test func setupPayloadOmitsDeviceWhenNil() throws {
        let data = try JSONEncoder().encode(MemorySetupClient.Payload(token: "tok123"))
        let object = try JSONSerialization.jsonObject(with: data)
        let json = try #require(object as? [String: Any])
        #expect(json["token"] as? String == "tok123")
        #expect(json["device"] == nil)
    }

    /// Unset manifest fields must not appear in the JSON at all.
    @Test func manifestOmitsUnsetKeys() throws {
        let data = try JSONEncoder().encode(DeviceManifest(platform: "ios"))
        let object = try JSONSerialization.jsonObject(with: data)
        let json = try #require(object as? [String: Any])
        #expect(json["platform"] as? String == "ios")
        #expect(json["modelId"] == nil)
        #expect(json["name"] == nil)
        #expect(json["osVersion"] == nil)
    }

    /// A live `DeviceInfo` manifest must round-trip through the setup payload
    /// encoding (the field set the app really sends).
    @Test func liveManifestEncodesThroughSetupPayload() throws {
        let info = DeviceInfo()
        let data = try JSONEncoder().encode(MemorySetupClient.Payload(token: "tok123", device: info.manifest))
        let object = try JSONSerialization.jsonObject(with: data)
        let json = try #require(object as? [String: Any])
        let device = try #require(json["device"] as? [String: Any])
        #expect(device["platform"] as? String == info.manifest.platform)
        #expect(device["modelId"] as? String == info.manifest.modelId)
        #expect(device["osName"] as? String == info.manifest.osName)
        #expect(device["osVersion"] as? String == info.manifest.osVersion)
    }
}
