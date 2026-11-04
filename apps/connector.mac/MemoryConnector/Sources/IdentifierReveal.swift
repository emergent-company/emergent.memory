import AppKit
import SwiftUI

/// Compact copy-only identifier affordance.
///
/// The raw identifier is hidden by default (nothing but a copy icon). Hovering
/// reveals it: an inline monospaced label AND a `.help` tooltip with the full
/// value. Clicking copies it to the general pasteboard and flips the icon to a
/// checkmark plus a "Copied" label for ~1.5s. Reads as an id without cluttering
/// the default view.
struct IdentifierReveal: View {
    let identifier: String

    @State private var isHovering = false
    @State private var copied = false
    @State private var resetTask: Task<Void, Never>?

    var body: some View {
        if identifier.isEmpty {
            EmptyView()
        } else {
            content
        }
    }

    private var content: some View {
        HStack(spacing: 6) {
            Button(action: copy) {
                Image(systemName: copied ? "checkmark" : "doc.on.doc")
                    .font(.caption)
                    .foregroundStyle(copied ? Color.green : Color.secondary)
                    .frame(width: 18, height: 18)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .help(identifier)
            .accessibilityLabel("Copy identifier")
            .accessibilityValue(identifier)

            if isHovering || copied {
                Text(copied ? "Copied" : identifier)
                    .font(.caption.monospaced())
                    .foregroundStyle(copied ? Color.green : Color.secondary)
                    .lineLimit(1)
                    .truncationMode(.middle)
                    .frame(maxWidth: 260, alignment: .leading)
                    .textSelection(.enabled)
                    .transition(.opacity)
            }
        }
        .onHover { hovering in
            withAnimation(.easeOut(duration: 0.12)) { isHovering = hovering }
        }
    }

    private func copy() {
        let pasteboard = NSPasteboard.general
        pasteboard.clearContents()
        pasteboard.setString(identifier, forType: .string)

        resetTask?.cancel()
        withAnimation(.easeOut(duration: 0.12)) { copied = true }
        resetTask = Task {
            try? await Task.sleep(for: .seconds(1.5))
            guard !Task.isCancelled else { return }
            withAnimation(.easeOut(duration: 0.12)) { copied = false }
        }
    }
}
