import Foundation

/// The handshake between the app and the share extension.
///
/// The app and the extension are separate processes with separate containers.
/// This App Group is the only storage both can see: the app mirrors its active
/// connection into these keys, and the extension reads them to know which
/// gateway and project a capture belongs to.
///
/// The group must also be enabled on both App IDs in the developer portal
/// (Signing & Capabilities → App Groups) before a device build; the simulator
/// does not enforce it. See `apps/ios/README.md`.
enum ShareGroup {
    /// App Group identifier shared by the app and the share extension.
    static let identifier = "group.com.emergent.memory"

    /// Defaults backed by the group container, or nil when the group is not
    /// available (unprovisioned build). Every caller treats nil as "no
    /// connection yet" rather than writing into a private container the other
    /// process would never see.
    static var defaults: UserDefaults? {
        UserDefaults(suiteName: identifier)
    }

    /// The group container root, or nil when the group is not available.
    static var containerURL: URL? {
        FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: identifier)
    }
}

/// The keys the app writes and the extension reads in ``ShareGroup/defaults``.
///
/// Namespaced under `alfred.share.` so a future `alfred.*` key in the app's own
/// defaults can never collide with a group key.
enum ShareConnectionKey {
    static let apiBaseURL = "alfred.share.apiBaseURL"
    static let apiKey = "alfred.share.apiKey"
    static let projectID = "alfred.share.projectID"
    static let projectName = "alfred.share.projectName"
    static let updatedAt = "alfred.share.updatedAt"
}

/// The connection a capture is sent through, as the extension sees it.
///
/// A device credential is project-scoped, so the project id is part of the
/// connection: `POST /api/projects/{projectID}/remember`.
struct ShareConnection: Equatable, Sendable {
    /// Gateway base URL (`http://host:8095`).
    let apiBaseURL: String
    /// Device credential (`emt_*`), sent as `X-API-Key`.
    let apiKey: String
    /// Project the credential was minted for.
    let projectID: String
    /// Human-readable project name, shown in the compose sheet.
    let projectName: String

    /// Whether the connection has everything a capture needs. A legacy
    /// identity-less connection (pre multi-project onboarding) cannot scope a
    /// remember call, so it is not usable here.
    var isUsable: Bool {
        !apiBaseURL.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
            && !apiKey.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
            && !projectID.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }

    /// The label shown in the compose sheet: the project name, falling back to
    /// the project id.
    var displayName: String {
        let name = projectName.trimmingCharacters(in: .whitespacesAndNewlines)
        return name.isEmpty ? projectID : name
    }

    /// Reads the connection the app last mirrored. Nil when the app has never
    /// onboarded a project, or when the group container is unavailable.
    static func load(from defaults: UserDefaults? = ShareGroup.defaults) -> ShareConnection? {
        guard let defaults else { return nil }
        let connection = ShareConnection(
            apiBaseURL: defaults.string(forKey: ShareConnectionKey.apiBaseURL) ?? "",
            apiKey: defaults.string(forKey: ShareConnectionKey.apiKey) ?? "",
            projectID: defaults.string(forKey: ShareConnectionKey.projectID) ?? "",
            projectName: defaults.string(forKey: ShareConnectionKey.projectName) ?? ""
        )
        return connection.isUsable ? connection : nil
    }

    /// Mirrors `connection` into the group so the extension can capture.
    ///
    /// Called by the app on every connection change. Passing nil clears the
    /// group (the last project was removed, so nothing on this device is
    /// allowed to remember anything).
    static func mirror(
        _ connection: ShareConnection?,
        to defaults: UserDefaults? = ShareGroup.defaults
    ) {
        guard let defaults else { return }
        guard let connection, connection.isUsable else {
            defaults.removeObject(forKey: ShareConnectionKey.apiBaseURL)
            defaults.removeObject(forKey: ShareConnectionKey.apiKey)
            defaults.removeObject(forKey: ShareConnectionKey.projectID)
            defaults.removeObject(forKey: ShareConnectionKey.projectName)
            defaults.removeObject(forKey: ShareConnectionKey.updatedAt)
            return
        }
        defaults.set(connection.apiBaseURL, forKey: ShareConnectionKey.apiBaseURL)
        defaults.set(connection.apiKey, forKey: ShareConnectionKey.apiKey)
        defaults.set(connection.projectID, forKey: ShareConnectionKey.projectID)
        defaults.set(connection.projectName, forKey: ShareConnectionKey.projectName)
        defaults.set(Date(), forKey: ShareConnectionKey.updatedAt)
    }
}
