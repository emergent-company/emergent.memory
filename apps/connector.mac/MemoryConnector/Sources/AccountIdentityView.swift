import SwiftUI

// MARK: - Signed-in identity presentation

extension OIDCUserInfo {
    /// Display name precedence: `name` → `preferred_username` → email local
    /// part. Empty when the provider returned none.
    var displayName: String {
        if let name, !name.isEmpty { return name }
        if let preferredUsername, !preferredUsername.isEmpty { return preferredUsername }
        if let email, let local = email.split(separator: "@").first, !local.isEmpty {
            return String(local)
        }
        return ""
    }

    var displayEmail: String { email ?? "" }

    var initials: String { MemoryIdentity.initials(from: displayName) }
}

/// Small pill that names the environment an account belongs to, so prod and
/// dev accounts are never confused wherever they are listed.
struct EnvironmentBadge: View {
    let label: String
    let isDev: Bool

    init(environment: Environment) {
        self.label = environment.shortLabel
        self.isDev = environment.id == Environment.dev.id
    }

    init(label: String, isDev: Bool = false) {
        self.label = label
        self.isDev = isDev
    }

    var body: some View {
        Text(label)
            .font(.caption2.weight(.semibold))
            .padding(.horizontal, 6)
            .padding(.vertical, 2)
            .foregroundStyle(tint)
            .background(Capsule().fill(tint.opacity(0.15)))
            .overlay(Capsule().strokeBorder(tint.opacity(0.35)))
            .fixedSize()
    }

    private var tint: Color {
        isDev ? .orange : .memoryPrimary
    }
}

/// One reusable account row used by the toolbar menu, the menu-bar popover,
/// and the account page so all surfaces agree. The identity is the account's
/// EMAIL address (`switcherTitle`), with the environment as a trailing badge
/// ("Prod"/"Dev") and a checkmark for the active account. No initials avatar
/// is shown by default (the switcher must not rely on an avatar/initials as
/// the identity); the Manage Accounts page opts back into the avatar.
struct AccountRowLabel: View {
    let account: Account
    /// Shows the initials avatar. Off by default so switcher rows are
    /// email-first; the account page turns it on.
    var showsAvatar: Bool = false
    var avatarSize: CGFloat = 24
    var isActive: Bool = false
    /// Hides the trailing checkmark (used where the active state is shown
    /// elsewhere, e.g. the menu-bar active header).
    var showsCheckmark: Bool = true

    var body: some View {
        HStack(spacing: 8) {
            if showsAvatar {
                AccountAvatar(initials: account.initials, size: avatarSize)
            }
            Text(account.switcherTitle)
                .fontWeight(isActive ? .semibold : .regular)
                .lineLimit(1)
                .truncationMode(.middle)
            Spacer(minLength: 12)
            EnvironmentBadge(label: account.environmentLabel,
                             isDev: account.environment?.id == Environment.dev.id)
            if showsCheckmark {
                // NB: do NOT use `.opacity(isActive ? 1 : 0)` here. A `Menu`
                // flattens its item labels to AppKit NSMenuItems and drops
                // rendering modifiers, so the opacity is ignored and every row
                // draws a checkmark. Include the glyph only for the active row;
                // the clear placeholder keeps the trailing badge aligned.
                if isActive {
                    Image(systemName: "checkmark")
                } else {
                    Color.clear.frame(width: 12, height: 12)
                }
            }
        }
    }
}

// MARK: - Shared account identity views

/// Initials avatar using Memory's account-avatar colours (brass `primary`
/// background, `primary-content` glyph). Used as the fallback when no picture
/// is available, and directly wherever a static avatar is enough.
struct AccountAvatar: View {
    let initials: String
    var size: CGFloat = 44

    var body: some View {
        ZStack {
            Circle()
                .fill(Color.memoryPrimary)
            if initials.isEmpty {
                Image(systemName: "person.fill")
                    .font(.system(size: size * 0.42))
                    .foregroundStyle(Color.memoryPrimaryContent)
            } else {
                Text(initials)
                    .font(.system(size: size * 0.4, weight: .semibold, design: .rounded))
                    .foregroundStyle(Color.memoryPrimaryContent)
            }
        }
        .frame(width: size, height: size)
    }
}

/// Identity row: initials avatar + display name + email. Used by both the
/// Project & Account account card and the Connection page's signed-in row.
struct AccountIdentityRow: View {
    let initials: String
    let name: String
    let email: String
    var size: CGFloat = 44

    init(initials: String, name: String, email: String, size: CGFloat = 44) {
        self.initials = initials
        self.name = name
        self.email = email
        self.size = size
    }

    init(identity: OIDCUserInfo, size: CGFloat = 44) {
        self.init(initials: identity.initials,
                  name: identity.displayName.isEmpty ? identity.displayEmail : identity.displayName,
                  email: identity.displayEmail,
                  size: size)
    }

    var body: some View {
        HStack(alignment: .center, spacing: 12) {
            MemoryAvatar(initials: initials,
                         size: size,
                         cacheKey: email.isEmpty ? nil : email)
            VStack(alignment: .leading, spacing: 3) {
                Text(name)
                    .font(size >= 44 ? .title3.weight(.semibold) : .headline)
                if !email.isEmpty {
                    Text(email)
                        .font(.callout)
                        .foregroundStyle(.secondary)
                        .textSelection(.enabled)
                }
            }
            Spacer(minLength: 0)
        }
    }
}
