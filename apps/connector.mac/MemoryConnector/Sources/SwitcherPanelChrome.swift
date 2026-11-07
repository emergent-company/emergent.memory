import SwiftUI

// Shared building blocks for the two toolbar context panels.
//
// The panels are real views inside a system `.popover`, not `Menu`/`NSMenu`
// content. That is the whole point: a `Menu` flattens its rows into
// `NSMenuItem`s, which fixes the row height, reserves the single leading glyph
// slot for the checkmark, and gives no way to set padding, a two-line label or
// a hover fill — the reason the previous controls read as a bare list of
// email addresses pressed against the edge.
//
// Spacing follows the Mac layout guidelines: small secondary text for the
// description line, separators inset to the text edge with breathing room above
// and below, and rows inset from the panel edge so a hover fill never touches
// the border.

// MARK: - Section header

/// Group label above a run of rows ("Recent", "Accounts", an organisation).
struct SwitcherSectionHeader: View {
    private let title: Text

    init(_ title: LocalizedStringResource) {
        self.title = Text(title)
    }

    /// Runtime data (an organisation name) — never a localization key.
    init(verbatim title: String) {
        self.title = Text(verbatim: title)
    }

    var body: some View {
        title
            .font(.caption.weight(.semibold))
            .foregroundStyle(.secondary)
            .lineLimit(1)
            .truncationMode(.tail)
            .padding(.horizontal, 10)
            .padding(.top, 10)
            .padding(.bottom, 2)
            .frame(maxWidth: .infinity, alignment: .leading)
    }
}

// MARK: - Divider

/// Separator between row groups. Inset to the text edge and surrounded by the
/// spacing the layout guidelines ask for, instead of AppKit's fixed menu-item
/// gaps.
struct SwitcherDivider: View {
    var body: some View {
        Divider()
            .padding(.horizontal, 10)
            .padding(.vertical, 8)
    }
}

// MARK: - Row icon

/// The optional leading glyph of a row or header.
enum SwitcherRowIcon {
    case none
    case avatar(initials: String, cacheKey: String?)
    case symbol(String)

    /// Views are main-actor isolated, and `MemoryAvatar`'s initializer is
    /// inferred `@MainActor` from its stored state.
    @MainActor
    @ViewBuilder
    func view(size: CGFloat) -> some View {
        switch self {
        case .none:
            EmptyView()
        case let .avatar(initials, cacheKey):
            MemoryAvatar(initials: initials, size: size, cacheKey: cacheKey)
        case let .symbol(name):
            ZStack {
                RoundedRectangle(cornerRadius: size * 0.24, style: .continuous)
                    .fill(Color.primary.opacity(0.07))
                Image(systemName: name)
                    .font(.system(size: size * 0.46, weight: .medium))
                    .foregroundStyle(.secondary)
            }
            .frame(width: size, height: size)
        }
    }
}

// MARK: - Identity header

/// The top block of a panel: who/what the panel is about, on two or three
/// lines. Primary line 13 pt semibold, description lines 11 pt secondary — the
/// guide's "optional description" pattern.
struct SwitcherIdentityHeader: View {
    let icon: SwitcherRowIcon
    let title: String
    var subtitle: String?
    /// Small badge after the title (the environment label, when it matters).
    var badge: String?
    var isDevBadge = false
    /// A third line, e.g. the connection state.
    var note: String?

    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            icon.view(size: 34)
            VStack(alignment: .leading, spacing: 1) {
                HStack(spacing: 6) {
                    Text(verbatim: title)
                        .font(.callout.weight(.semibold))
                        .lineLimit(1)
                        .truncationMode(.middle)
                    if let badge {
                        EnvironmentBadge(label: badge, isDev: isDevBadge)
                    }
                }
                if let subtitle, !subtitle.isEmpty {
                    Text(verbatim: subtitle)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                        .truncationMode(.middle)
                }
                if let note, !note.isEmpty {
                    Text(verbatim: note)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
            }
            Spacer(minLength: 0)
        }
        .padding(.horizontal, 10)
        .padding(.top, 10)
        .padding(.bottom, 8)
    }
}

// MARK: - Selectable row

/// One selectable row: optional leading icon, a primary line, an optional
/// secondary line, and trailing state (connected dot, active checkmark).
///
/// The checkmark is drawn only for the active row and a clear spacer keeps the
/// trailing edge aligned — modifier-based hiding is what went wrong inside a
/// `Menu`, and it is safe here only because this is a real view.
struct SwitcherRow: View {
    let icon: SwitcherRowIcon
    let title: String
    var subtitle: String?
    var isActive = false
    var isConnected = false
    var isHighlighted = false
    var isEnabled = true
    let action: () -> Void

    @State private var isHovering = false

    var body: some View {
        Button(action: action) {
            HStack(spacing: 8) {
                if case .none = icon {
                    EmptyView()
                } else {
                    icon.view(size: 22)
                }
                VStack(alignment: .leading, spacing: 0) {
                    Text(verbatim: title)
                        .font(.callout.weight(isActive ? .semibold : .regular))
                        .lineLimit(1)
                        .truncationMode(.middle)
                    if let subtitle, !subtitle.isEmpty {
                        Text(verbatim: subtitle)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                            .lineLimit(1)
                            .truncationMode(.tail)
                    }
                }
                Spacer(minLength: 8)
                if isConnected {
                    // Same colour as the trigger's connection glyph: one state,
                    // one visual, on both surfaces.
                    Circle()
                        .fill(Color.green)
                        .frame(width: 6, height: 6)
                        .accessibilityHidden(true)
                }
                Image(systemName: "checkmark")
                    .font(.system(size: 11, weight: .semibold))
                    .opacity(isActive ? 1 : 0)
                    .accessibilityHidden(true)
            }
            .padding(.horizontal, 8)
            // A minimum, not a fixed height: a larger text size makes the two
            // lines taller and the row has to grow with them.
            .frame(minHeight: subtitle == nil ? 28 : 36)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(
                RoundedRectangle(cornerRadius: 6, style: .continuous)
                    .fill(fill)
            )
            .contentShape(RoundedRectangle(cornerRadius: 6, style: .continuous))
        }
        .buttonStyle(.plain)
        .disabled(!isEnabled)
        .onHover { isHovering = $0 }
        .accessibilityLabel(Text(verbatim: title))
        .accessibilityValue(Text(verbatim: accessibilityValue))
        .accessibilityAddTraits(isActive ? [.isSelected] : [])
    }

    private var fill: Color {
        if isHighlighted || isHovering {
            return Color.primary.opacity(0.09)
        }
        return .clear
    }

    private var accessibilityValue: String {
        var parts: [String] = []
        if let subtitle, !subtitle.isEmpty {
            parts.append(subtitle)
        }
        if isConnected {
            parts.append(String(localized: "Connected"))
        }
        return parts.joined(separator: ", ")
    }
}

// MARK: - Action row

/// A command at the bottom of a panel. Same hover treatment as a selectable
/// row so the whole panel reads as one list, but never shows a selection state.
struct SwitcherActionRow: View {
    let title: LocalizedStringResource
    let systemImage: String
    var isDestructive = false
    var isEnabled = true
    var isHighlighted = false
    let action: () -> Void

    @State private var isHovering = false

    var body: some View {
        Button(action: action) {
            HStack(spacing: 8) {
                Image(systemName: systemImage)
                    .font(.system(size: 11, weight: .medium))
                    .frame(width: 14)
                    .foregroundStyle(isDestructive ? AnyShapeStyle(Color.red) : AnyShapeStyle(.secondary))
                Text(title)
                    .font(.callout)
                    .foregroundStyle(isDestructive ? AnyShapeStyle(Color.red) : AnyShapeStyle(.primary))
                Spacer(minLength: 0)
            }
            .padding(.horizontal, 8)
            .frame(height: 26)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(
                RoundedRectangle(cornerRadius: 6, style: .continuous)
                    .fill(isHighlighted || isHovering ? Color.primary.opacity(0.09) : Color.clear)
            )
            .contentShape(RoundedRectangle(cornerRadius: 6, style: .continuous))
        }
        .buttonStyle(.plain)
        .disabled(!isEnabled)
        .onHover { isHovering = $0 }
    }
}

// MARK: - Empty / error block

/// Loading, empty and error copy shown where a list would be. Never rendered as
/// a menu row, and never a disabled hint.
struct SwitcherStatusBlock: View {
    let message: String
    var isLoading = false

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 8) {
                if isLoading {
                    ProgressView().controlSize(.small)
                }
                Text(verbatim: message)
                    .font(.callout)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
            .padding(.horizontal, 10)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.vertical, 10)
    }
}
