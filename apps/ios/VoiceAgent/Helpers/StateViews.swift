import SwiftUI

/// Shared centered spinner + caption loading state. Replaces the four
/// hand-rolled `Spinner`/`Text` loading views across the app.
struct LoadingStateView: View {
    let title: LocalizedStringKey
    /// Caption font. The picker and session detail use `.footnote`; the object
    /// and session browsers use a fixed 13pt caption.
    var font: Font = .footnote
    /// When true the view expands to fill the available height (full-content
    /// loading states); when false it is a minimum-height block with the
    /// spinner vertically centered (the picker's compact loading state).
    var fillsHeight: Bool = true

    var body: some View {
        let maxHeight: CGFloat? = fillsHeight ? CGFloat.infinity : nil
        let minHeight: CGFloat? = fillsHeight ? nil : 32 * .grid
        return VStack(spacing: 3 * .grid) {
            if !fillsHeight {
                Spacer()
            }
            Spinner()
            Text(title)
                .font(font)
                .foregroundStyle(.fg3)
            if !fillsHeight {
                Spacer()
            }
        }
        .frame(maxWidth: .infinity)
        .frame(maxHeight: maxHeight)
        .frame(minHeight: minHeight)
    }
}

/// Shared error state: a `ContentUnavailableView` with an optional leading
/// action (the picker's scan-QR) followed by a retry button.
struct ErrorStateView: View {
    let title: LocalizedStringKey
    let description: LocalizedStringKey
    let message: String
    let retryLabel: LocalizedStringKey
    let retry: () -> Void
    var systemImage: String = "wifi.exclamationmark"
    var leadingActionLabel: LocalizedStringKey? = nil
    var leadingActionImage: String? = nil
    var leadingAction: (() -> Void)? = nil

    var body: some View {
        ContentUnavailableView {
            Label(title, systemImage: systemImage)
        } description: {
            VStack(spacing: 1 * .grid) {
                Text(description)
                Text(message)
                    .font(MemoryFont.meta)
                    .foregroundStyle(.fg3)
            }
        } actions: {
            if let leadingAction, let leadingActionLabel {
                Button(action: leadingAction) {
                    HStack(spacing: 1 * .grid) {
                        if let leadingActionImage {
                            Image(systemName: leadingActionImage)
                        }
                        Text(leadingActionLabel)
                    }
                    .font(MemoryFont.labelEmphasis)
                    .foregroundStyle(.fgAccent)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .padding(.top, 2 * .grid)
            }
            Button(action: retry) {
                Text(retryLabel)
                    .font(MemoryFont.labelEmphasis)
                    .foregroundStyle(.fgAccent)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .padding(.top, leadingAction == nil ? 2 * .grid : 1 * .grid)
        }
    }
}

/// Shared read-only empty state.
struct EmptyStateView: View {
    let title: LocalizedStringKey
    let description: LocalizedStringKey
    let systemImage: String
    /// Optional secondary message (the session detail's soft-failure text).
    var message: String? = nil
    /// Optional extra top padding (the picker's empty state).
    var topPadding: CGFloat = 0

    var body: some View {
        ContentUnavailableView {
            Label(title, systemImage: systemImage)
        } description: {
            VStack(spacing: 1 * .grid) {
                Text(description)
                if let message {
                    Text(message)
                        .font(MemoryFont.meta)
                        .foregroundStyle(.fg3)
                }
            }
        }
        .padding(.top, topPadding)
    }
}
