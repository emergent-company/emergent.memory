import Darwin
import Foundation
import UIKit

/// Device self-introduction manifest sent to the gateway inside the one-time
/// setup request body: `{"token": ..., "device": {...}}`.
///
/// Every key is an optional string (see the "device-self-introduction" OpenSpec
/// change, section 2, for the gateway contract). The raw hardware identifier in
/// `modelId` is the durable discriminator; every other field is best-effort
/// display sugar. Synthesized coding keys match the camelCase JSON keys exactly.
struct DeviceManifest: Codable, Equatable, Sendable {
    /// `"ios"` | `"ipados"` | `"macos"` (gateway also reserves android/web).
    let platform: String?
    /// `"phone"` | `"tablet"` | `"desktop"`.
    let formFactor: String?
    /// User-assigned device name (`UIDevice.name`; may read generic on iOS 16+).
    let name: String?
    /// Raw hardware identifier (e.g. `"iPhone15,2"`).
    let modelId: String?
    /// Best-effort marketing name from the in-repo table; the raw id when
    /// unmapped.
    let modelDisplay: String?
    /// OS name (e.g. `"iOS"`).
    let osName: String?
    /// OS version (e.g. `"18.3.1"`).
    let osVersion: String?
    /// `CFBundleShortVersionString` of the app.
    let appVersion: String?
    /// `CFBundleVersion` of the app.
    let appBuild: String?

    /// All fields optional; pass only what is known.
    init(
        platform: String? = nil,
        formFactor: String? = nil,
        name: String? = nil,
        modelId: String? = nil,
        modelDisplay: String? = nil,
        osName: String? = nil,
        osVersion: String? = nil,
        appVersion: String? = nil,
        appBuild: String? = nil
    ) {
        self.platform = platform
        self.formFactor = formFactor
        self.name = name
        self.modelId = modelId
        self.modelDisplay = modelDisplay
        self.osName = osName
        self.osVersion = osVersion
        self.appVersion = appVersion
        self.appBuild = appBuild
    }
}

/// Snapshot of the current device for the setup self-introduction.
///
/// Reads `UIDevice.current` for the user-assigned name and OS identity, the
/// raw hardware identifier via `sysctlbyname("hw.machine")`, and
/// `Bundle.main` for the app version/build. Deliberately dependency-free: the
/// identifier→marketing-name table below is small and intentionally goes stale
/// (every September); the raw `modelId` is what stays reliable, and unmapped
/// identifiers fall back to it.
struct DeviceInfo: Sendable {
    /// The manifest to attach to the setup request (`device` field).
    let manifest: DeviceManifest

    /// Builds the manifest from the live device and the main bundle.
    ///
    /// Both parameters are injectable for tests; the defaults read the
    /// running device/app.
    init(device: UIDevice = .current, bundle: Bundle = .main) {
        let modelID = Self.hardwareModelIdentifier(device: device)
        manifest = DeviceManifest(
            platform: Self.platform(
                systemName: device.systemName,
                userInterfaceIdiom: device.userInterfaceIdiom
            ),
            formFactor: Self.formFactor(for: device.userInterfaceIdiom),
            name: Self.optional(device.name),
            modelId: Self.optional(modelID),
            modelDisplay: Self.optional(Self.modelDisplay(for: modelID)),
            osName: Self.optional(device.systemName),
            osVersion: Self.optional(device.systemVersion),
            appVersion: Self.optional(
                bundle.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String
            ),
            appBuild: Self.optional(
                bundle.object(forInfoDictionaryKey: "CFBundleVersion") as? String
            )
        )
    }

    // MARK: - Derivation (pure, unit-tested)

    /// Derives the gateway `platform` value. The idiom wins (`.pad` ⇒
    /// `"ipados"`, `.mac` ⇒ `"macos"`); a systemName containing "iPad" is the
    /// tiebreak for non-pad idioms.
    static func platform(systemName: String, userInterfaceIdiom: UIUserInterfaceIdiom) -> String {
        switch userInterfaceIdiom {
        case .pad:
            return "ipados"
        case .mac:
            return "macos"
        default:
            return systemName.lowercased().contains("ipad") ? "ipados" : "ios"
        }
    }

    /// Derives the optional `formFactor` value from the idiom; unusual idioms
    /// (`.tv`, `.carPlay`, ...) yield nil so the field stays out of the JSON.
    static func formFactor(for idiom: UIUserInterfaceIdiom) -> String? {
        switch idiom {
        case .phone:
            return "phone"
        case .pad:
            return "tablet"
        case .mac:
            return "desktop"
        default:
            return nil
        }
    }

    /// Maps a raw hardware identifier to its marketing name, falling back to
    /// the raw identifier when the table has no entry.
    static func modelDisplay(for modelID: String) -> String {
        marketingNames[modelID] ?? modelID
    }

    // MARK: - Hardware identifier

    /// Raw hardware identifier via `sysctlbyname("hw.machine")`
    /// (e.g. `"iPhone15,2"`); falls back to `device.model` on failure or when
    /// empty.
    static func hardwareModelIdentifier(device: UIDevice = .current) -> String {
        var size = 0
        guard sysctlbyname("hw.machine", nil, &size, nil, 0) == 0, size > 0 else {
            return device.model
        }
        var machine = [CChar](repeating: 0, count: size)
        guard sysctlbyname("hw.machine", &machine, &size, nil, 0) == 0 else {
            return device.model
        }
        // sysctl wrote a NUL-terminated string; drop everything from the
        // terminator, then decode the remaining bytes as UTF-8.
        let end = machine.firstIndex(of: 0) ?? machine.endIndex
        let bytes = machine[..<end].map { UInt8(bitPattern: $0) }
        let identifier = String(decoding: bytes, as: UTF8.self)
        return identifier.isEmpty ? device.model : identifier
    }

    // MARK: - Helpers

    /// Trims a string and collapses empties to nil so absent fields stay out
    /// of the encoded JSON.
    private static func optional(_ value: String?) -> String? {
        guard let value else { return nil }
        let trimmed = value.trimmingCharacters(in: .whitespacesAndNewlines)
        return trimmed.isEmpty ? nil : trimmed
    }

    // MARK: - Identifier → marketing name (best-effort, in-repo)

    private static let marketingNames: [String: String] = [
        // iPhone
        "iPhone8,1": "iPhone 6s",
        "iPhone8,2": "iPhone 6s Plus",
        "iPhone8,4": "iPhone SE (1st generation)",
        "iPhone9,1": "iPhone 7",
        "iPhone9,2": "iPhone 7 Plus",
        "iPhone9,3": "iPhone 7",
        "iPhone9,4": "iPhone 7 Plus",
        "iPhone10,1": "iPhone 8",
        "iPhone10,2": "iPhone 8 Plus",
        "iPhone10,3": "iPhone X",
        "iPhone10,4": "iPhone 8",
        "iPhone10,5": "iPhone 8 Plus",
        "iPhone10,6": "iPhone X",
        "iPhone11,2": "iPhone XS",
        "iPhone11,4": "iPhone XS Max",
        "iPhone11,6": "iPhone XS Max",
        "iPhone11,8": "iPhone XR",
        "iPhone12,1": "iPhone 11",
        "iPhone12,3": "iPhone 11 Pro",
        "iPhone12,5": "iPhone 11 Pro Max",
        "iPhone12,8": "iPhone SE (2nd generation)",
        "iPhone13,1": "iPhone 12 mini",
        "iPhone13,2": "iPhone 12",
        "iPhone13,3": "iPhone 12 Pro",
        "iPhone13,4": "iPhone 12 Pro Max",
        "iPhone14,2": "iPhone 13 Pro",
        "iPhone14,3": "iPhone 13 Pro Max",
        "iPhone14,4": "iPhone 13 mini",
        "iPhone14,5": "iPhone 13",
        "iPhone14,6": "iPhone SE (3rd generation)",
        "iPhone14,7": "iPhone 14",
        "iPhone14,8": "iPhone 14 Plus",
        "iPhone15,2": "iPhone 14 Pro",
        "iPhone15,3": "iPhone 14 Pro Max",
        "iPhone15,4": "iPhone 15",
        "iPhone15,5": "iPhone 15 Plus",
        "iPhone16,1": "iPhone 15 Pro",
        "iPhone16,2": "iPhone 15 Pro Max",
        "iPhone17,1": "iPhone 16 Pro",
        "iPhone17,2": "iPhone 16 Pro Max",
        "iPhone17,3": "iPhone 16",
        "iPhone17,4": "iPhone 16 Plus",
        "iPhone17,5": "iPhone 16e",
        // iPad
        "iPad11,1": "iPad mini (5th generation)",
        "iPad11,2": "iPad mini (5th generation)",
        "iPad11,3": "iPad Air (3rd generation)",
        "iPad11,4": "iPad Air (3rd generation)",
        "iPad11,6": "iPad (8th generation)",
        "iPad11,7": "iPad (8th generation)",
        "iPad12,1": "iPad (9th generation)",
        "iPad12,2": "iPad (9th generation)",
        "iPad13,1": "iPad Air (4th generation)",
        "iPad13,2": "iPad Air (4th generation)",
        "iPad13,4": "iPad Pro 11-inch (3rd generation)",
        "iPad13,5": "iPad Pro 11-inch (3rd generation)",
        "iPad13,6": "iPad Pro 11-inch (3rd generation)",
        "iPad13,7": "iPad Pro 11-inch (3rd generation)",
        "iPad13,8": "iPad Pro 12.9-inch (5th generation)",
        "iPad13,9": "iPad Pro 12.9-inch (5th generation)",
        "iPad13,10": "iPad Pro 12.9-inch (5th generation)",
        "iPad13,11": "iPad Pro 12.9-inch (5th generation)",
        "iPad13,16": "iPad Air (5th generation)",
        "iPad13,17": "iPad Air (5th generation)",
        "iPad13,18": "iPad (10th generation)",
        "iPad13,19": "iPad (10th generation)",
        "iPad14,1": "iPad mini (6th generation)",
        "iPad14,2": "iPad mini (6th generation)",
        "iPad14,3": "iPad Pro 11-inch (4th generation)",
        "iPad14,4": "iPad Pro 11-inch (4th generation)",
        "iPad14,5": "iPad Pro 12.9-inch (6th generation)",
        "iPad14,6": "iPad Pro 12.9-inch (6th generation)",
        "iPad14,8": "iPad Air 11-inch (M2)",
        "iPad14,9": "iPad Air 11-inch (M2)",
        "iPad14,10": "iPad Air 13-inch (M2)",
        "iPad14,11": "iPad Air 13-inch (M2)",
        "iPad16,1": "iPad mini (7th generation)",
        "iPad16,2": "iPad mini (7th generation)",
        "iPad16,3": "iPad Pro 11-inch (M4)",
        "iPad16,4": "iPad Pro 11-inch (M4)",
        "iPad16,5": "iPad Pro 13-inch (M4)",
        "iPad16,6": "iPad Pro 13-inch (M4)",
    ]
}
