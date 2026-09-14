import SwiftUI

// MARK: - Memory brand colours

extension Color {
    /// Memory web (daisyUI dark theme) `primary` — sRGB approximation of
    /// `oklch(0.74 0.135 78)` → #D99F36. Used for the account avatar.
    static let memoryPrimary = Color(red: 0.8513, green: 0.6231, blue: 0.2125)
    /// daisyUI `primary-content` — sRGB approximation of `oklch(0.2 0.04 78)`
    /// → #201301. Text/glyph colour drawn on `memoryPrimary`.
    static let memoryPrimaryContent = Color(red: 0.1248, green: 0.0758, blue: 0.0030)
}

/// Shared card container for the information pages (Dashboard, Project &
/// Account, About). Keeps padding, corner radius, and border consistent.
struct ConnectorCard<Content: View>: View {
    private let title: String?
    private let systemImage: String?
    private let content: Content

    init(title: String? = nil, systemImage: String? = nil,
         @ViewBuilder content: () -> Content) {
        self.title = title
        self.systemImage = systemImage
        self.content = content()
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            if let title {
                HStack(spacing: 8) {
                    if let systemImage {
                        Image(systemName: systemImage)
                            .foregroundStyle(.secondary)
                    }
                    Text(title)
                        .font(.headline)
                }
            }
            content
        }
        .padding(18)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(
            RoundedRectangle(cornerRadius: 12, style: .continuous)
                .fill(.quaternary.opacity(0.5))
        )
        .overlay(
            RoundedRectangle(cornerRadius: 12, style: .continuous)
                .strokeBorder(.quaternary.opacity(0.7))
        )
    }
}

/// A label/value row used inside cards. Values are selectable so a user can
/// copy a project or instance id out.
struct ConnectorInfoRow: View {
    let label: String
    let value: String
    var monospaced: Bool = false
    var valueColor: Color = .primary

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 12) {
            Text(label)
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .frame(width: 130, alignment: .leading)
            Text(value)
                .font(monospaced ? .subheadline.monospaced() : .subheadline)
                .foregroundStyle(valueColor)
                .textSelection(.enabled)
            Spacer(minLength: 0)
        }
    }
}

/// A label + copy-only identifier row (id hidden until hover, copyable with
/// the shared control). Used for the engine instance id on the Dashboard page.
struct IdentifierInfoRow: View {
    let label: String
    let identifier: String

    var body: some View {
        HStack(alignment: .center, spacing: 12) {
            Text(label)
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .frame(width: 130, alignment: .leading)
            if identifier.isEmpty {
                Text("—")
                    .font(.subheadline)
                    .foregroundStyle(.tertiary)
            } else {
                IdentifierReveal(identifier: identifier)
            }
            Spacer(minLength: 0)
        }
    }
}

/// A small centered spinner + caption used while a page loads identity data.
struct ConnectorLoadingRow: View {
    var caption: String = "Loading…"

    var body: some View {
        HStack(spacing: 10) {
            ProgressView()
                .controlSize(.small)
            Text(caption)
                .font(.callout)
                .foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.vertical, 6)
    }
}
