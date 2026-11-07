import Foundation
import SwiftUI

/// The account context panel: the signed-in identity at the top, every stored
/// account below it, then the account commands.
///
/// The identity block is the answer to "who am I right now" without opening the
/// menu; the rows are the answer to "switch me to that one". Both are why this is
/// a popover panel rather than a menu — a menu row has one text line and one
/// glyph slot, so the avatar, the display name and the environment had nowhere
/// to go and were flattened into `you@example.com · Prod`.
struct AccountSwitcherPanel: View {
    @SwiftUI.Environment(AccountStore.self) private var accountStore

    var onDismiss: (() -> Void)?
    var onAddAccount: () -> Void
    var onManageAccounts: () -> Void
    var onSignOut: (Account) -> Void

    @State private var highlightedID: String?
    @FocusState private var isFocused: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            header
            if accountStore.accounts.count > 1 {
                SwitcherSectionHeader("Accounts")
                ForEach(accountStore.accounts) { account in
                    row(account)
                }
            }
            SwitcherDivider()
            actions
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .focusable()
        .focused($isFocused)
        .focusEffectDisabled()
        .onMoveCommand(perform: move)
        .onKeyPress(.return) {
            guard let id = highlightedID, id != accountStore.activeAccountID else { return .ignored }
            switchAccount(to: id)
            return .handled
        }
        .onAppear(perform: prepareKeyboard)
    }

    // MARK: - Header

    @ViewBuilder
    private var header: some View {
        if let account = accountStore.activeAccount {
            SwitcherIdentityHeader(
                icon: .avatar(initials: account.initials, cacheKey: account.email),
                title: account.displayTitle,
                subtitle: subtitle(for: account),
                badge: showsEnvironmentLabels ? account.environmentLabel : nil,
                isDevBadge: account.environment?.id == Environment.dev.id
            )
        } else {
            SwitcherIdentityHeader(
                icon: .symbol("person.crop.circle"),
                title: String(localized: "Signed out"),
                subtitle: nil
            )
        }
    }

    // MARK: - Rows

    private func row(_ account: Account) -> some View {
        SwitcherRow(
            icon: .avatar(initials: account.initials, cacheKey: account.email),
            title: account.switcherTitle,
            subtitle: showsEnvironmentLabels ? account.environmentLabel : nil,
            isActive: account.id == accountStore.activeAccountID,
            isHighlighted: highlightedID == account.id,
            isEnabled: !accountStore.isSigningIn,
            action: { switchAccount(to: account.id) }
        )
    }

    // MARK: - Actions

    @ViewBuilder
    private var actions: some View {
        SwitcherActionRow(
            title: "Add account…",
            systemImage: "person.badge.plus",
            isEnabled: !accountStore.isSigningIn,
            action: {
                onDismiss?()
                onAddAccount()
            }
        )
        SwitcherActionRow(
            title: "Manage accounts…",
            systemImage: "gearshape",
            action: {
                onDismiss?()
                onManageAccounts()
            }
        )
        SwitcherActionRow(
            title: "Sign out",
            systemImage: "rectangle.portrait.and.arrow.right",
            isDestructive: true,
            isEnabled: !accountStore.isSigningIn,
            action: {
                guard let account = accountStore.activeAccount else { return }
                onDismiss?()
                onSignOut(account)
            }
        )
    }

    // MARK: - Keyboard

    /// Seeds the highlight on the active account and takes focus. Deferred by
    /// one runloop turn: during `onAppear` the popover window is not key yet and
    /// a synchronous focus assignment is silently dropped, leaving arrow keys and
    /// Return dead with no visible symptom.
    private func prepareKeyboard() {
        highlightedID = accountStore.activeAccountID
        DispatchQueue.main.async { isFocused = true }
    }

    private func move(_ direction: MoveCommandDirection) {
        let ids = accountStore.accounts.map(\.id)
        guard !ids.isEmpty else { return }
        switch direction {
        case .down:
            guard let current = highlightedID, let index = ids.firstIndex(of: current) else {
                highlightedID = ids.first
                return
            }
            highlightedID = ids[min(index + 1, ids.count - 1)]
        case .up:
            guard let current = highlightedID, let index = ids.firstIndex(of: current) else {
                highlightedID = ids.last
                return
            }
            highlightedID = ids[max(index - 1, 0)]
        default:
            break
        }
    }

    // MARK: - Derived

    /// Environment labels only earn their space when the account list actually
    /// spans more than one environment.
    private var showsEnvironmentLabels: Bool {
        Set(accountStore.accounts.map(\.environmentID)).count > 1
    }

    /// The email is the identity line; when the display name falls back to the
    /// email it would just repeat the title, so it is dropped.
    private func subtitle(for account: Account) -> String? {
        guard let email = account.email, !email.isEmpty, email != account.displayTitle else {
            return nil
        }
        return email
    }

    // MARK: - Actions

    private func switchAccount(to id: String) {
        guard id != accountStore.activeAccountID else { return }
        onDismiss?()
        Task { try? await accountStore.switchTo(accountID: id) }
    }
}
