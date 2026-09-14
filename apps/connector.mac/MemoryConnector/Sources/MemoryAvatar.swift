import AppKit
import SwiftUI

/// Process-wide, main-actor avatar store. Single source of truth for the
/// signed-in user's picture so the toolbar control and the account card show
/// the SAME image: the bytes are fetched once per account key and every
/// `MemoryAvatar` observes this store.
///
/// Keyed by the account identifier (`sub` → `email`) — never the caller's
/// view-local values — so toolbar and page can never diverge. Raw `NSImage`
/// lives on the main actor (the fetch already hops here); no actor needed.
@MainActor
final class AvatarStore: ObservableObject {
    static let shared = AvatarStore()

    @Published private(set) var images: [String: NSImage] = [:]
    /// Keys the server reported as "no picture" (HTTP 404) — don't refetch.
    @Published private(set) var missing: Set<String> = []

    private init() {}

    /// The one account key both the toolbar and the card compute.
    static func accountKey(for identity: OIDCUserInfo?) -> String? {
        if let sub = identity?.sub, !sub.isEmpty { return sub }
        if let email = identity?.email, !email.isEmpty { return email }
        return nil
    }

    func image(for key: String) -> NSImage? { images[key] }
    func isMissing(_ key: String) -> Bool { missing.contains(key) }
    func hasEntry(_ key: String) -> Bool { images[key] != nil || missing.contains(key) }

    func store(_ image: NSImage, for key: String) {
        images[key] = image
        missing.remove(key)
    }

    func storeMissing(for key: String) {
        missing.insert(key)
        images[key] = nil
    }

    /// Clears everything (sign-out / user change) so a new account never sees
    /// stale artwork.
    func clear() {
        images.removeAll()
        missing.removeAll()
    }
}

/// Circular account avatar. When signed in it loads the user's real picture
/// from Memory (`GET /api/user/avatar`) via the shared `AvatarStore`; otherwise
/// — or while loading, or if the user has no picture — it falls back to the
/// `AccountAvatar` initials circle in Memory's colours.
struct MemoryAvatar: View {
    @EnvironmentObject private var accountStore: AccountStore
    @EnvironmentObject private var projectStore: ProjectStore
    @ObservedObject private var store = AvatarStore.shared

    let initials: String
    let size: CGFloat
    /// Optional fallback key used only when no signed-in identity is available.
    private let explicitCacheKey: String?

    init(initials: String, size: CGFloat = 44, cacheKey: String? = nil) {
        self.initials = initials
        self.size = size
        self.explicitCacheKey = cacheKey
    }

    var body: some View {
        ZStack {
            if let image = store.image(for: effectiveKey) {
                Image(nsImage: image)
                    .resizable()
                    .scaledToFill()
            } else {
                AccountAvatar(initials: initials, size: size)
            }
        }
        .frame(width: size, height: size)
        .clipShape(Circle())
        .task(id: taskID) { await load() }
    }

    // MARK: - Loading

    /// The shared account key (same for toolbar + card); falls back to the
    /// caller's key/initials only when no identity is available yet.
    private var effectiveKey: String {
        if let key = AvatarStore.accountKey(for: accountStore.activeIdentity) { return key }
        if let explicitCacheKey, !explicitCacheKey.isEmpty { return explicitCacheKey }
        return initials.isEmpty ? "signed-out" : initials
    }

    /// Re-runs the load when the user or sign-in state changes.
    private var taskID: String { "\(effectiveKey)|\(accountStore.isEffectivelySignedIn)" }

    private func load() async {
        guard accountStore.isEffectivelySignedIn else {
            // Only clear when we're the one rendering the signed-out state;
            // `AvatarStore` is shared and clearing is idempotent.
            store.clear()
            return
        }

        let key = effectiveKey
        if store.hasEntry(key) { return } // image or cached 404 — nothing to do

        do {
            let token = try await accountStore.currentAccessToken()
            let data = try await MemoryAPIClient(serverURL: accountStore.activeEnvironment?.serverURLString ?? "", token: token)
                .avatarData(projectID: projectStore.activeProjectID ?? "")
            if let data, !data.isEmpty, let image = NSImage(data: data) {
                store.store(image, for: key)
            } else {
                store.storeMissing(for: key)
            }
        } catch {
            // Keep the initials fallback; the key changes on the next user/session.
        }
    }
}
