import Foundation
import SwiftUI

/// A reply's grounded references as a collapsed footer under its markdown:
/// "N sources", opening onto one row per cited object. Tapping a row opens
/// that object's card.
///
/// The native counterpart of the web chat's Sources footer
/// (`sourcesFooter`, `chat-components.js`): collapsed by default, inside the
/// reply rather than a row of its own, rows named by label, then key, then id,
/// and nothing drawn when the reply cited nothing nameable.
///
/// A row opens the card through `openURL` with the citation's `/objects/…`
/// link, so it takes the same path as an inline link in the reply
/// (``View/presentsObjectReferences()``).
struct SourcesFooter: View {
    let citations: [ChatCitation]
    /// The reply this footer belongs to: the key its open state is kept under.
    let messageID: String

    @Environment(\.toolActivityDisclosure) private var disclosure
    /// The label's plural rule is chosen in the reader's language, including a
    /// locale a parent view overrides.
    @Environment(\.locale) private var locale
    /// Used only outside a chat list, where no disclosure store is provided.
    @State private var localExpanded = false

    var body: some View {
        let rows = ChatCitation.deduplicated(citations)
        if !rows.isEmpty {
            DisclosureGroup(isExpanded: expandedBinding) {
                VStack(alignment: .leading, spacing: 1.5 * .grid) {
                    ForEach(rows, id: \.self) { citation in
                        SourceRow(citation: citation)
                    }
                }
                .padding(.top, 2 * .grid)
            } label: {
                Label {
                    Text(verbatim: SourcesCountLabel.text(for: rows.count, locale: locale))
                } icon: {
                    Image(systemName: "link")
                }
                .font(MemoryFont.metaEmphasis)
                .foregroundStyle(.fg2)
            }
            .tint(.fg3)
            .padding(.horizontal, 3 * .grid)
            .padding(.vertical, 2 * .grid)
            .background(
                RoundedRectangle(cornerRadius: .cornerRadiusSmall)
                    .fill(Color.bg2.opacity(0.5))
                    .overlay(
                        RoundedRectangle(cornerRadius: .cornerRadiusSmall)
                            .strokeBorder(Color.separator1.opacity(0.5), lineWidth: 1)
                    )
            )
        }
    }

    /// The open state, read here in `body` so observation tracks it for this
    /// view; the setter writes back to the chat's disclosure store.
    private var expandedBinding: Binding<Bool> {
        let isExpanded = disclosure?.isSourcesExpanded(messageID) ?? localExpanded
        return Binding(
            get: { isExpanded },
            set: { newValue in
                if let disclosure {
                    disclosure.setSourcesExpanded(newValue, for: messageID)
                } else {
                    localExpanded = newValue
                }
            }
        )
    }
}

/// The footer's label: "1 source", "3 sources", from the string catalog's
/// `one`/`other` plural variations (the same rule as the web's
/// `sourceLabel`). Split out of the view so a test can pin the wording
/// without rendering.
enum SourcesCountLabel {
    static func text(for count: Int, locale: Locale = .current) -> String {
        String(localized: "chat.sources.count \(count)", locale: locale)
    }
}

/// One cited object: its name and type. A row that resolves to an object is a
/// button opening its card; one that does not is shown, inert.
private struct SourceRow: View {
    let citation: ChatCitation

    @Environment(\.openURL) private var openURL

    var body: some View {
        if let url = citation.objectReference?.url {
            Button {
                openURL(url)
            } label: {
                rowContent(isLink: true)
            }
            .buttonStyle(.plain)
            .accessibilityHint(Text("chat.sources.open.hint"))
        } else {
            rowContent(isLink: false)
        }
    }

    private func rowContent(isLink: Bool) -> some View {
        HStack(spacing: 2 * .grid) {
            Text(verbatim: citation.displayName)
                .font(MemoryFont.bodySmall)
                .foregroundStyle(isLink ? .fgAccent : .fg1)
                .lineLimit(1)
                .truncationMode(.middle)
            Spacer(minLength: 2 * .grid)
            if !typeLabel.isEmpty {
                ObjectBadge(text: typeLabel, tint: .fg3)
            }
            if isLink {
                Image(systemName: "chevron.right")
                    .font(.caption2.weight(.semibold))
                    .foregroundStyle(.fg4)
                    .accessibilityHidden(true)
            }
        }
        .padding(.horizontal, 2.5 * .grid)
        .padding(.vertical, 2 * .grid)
        .frame(minHeight: 44)
        .background(Color.bg1.opacity(0.6), in: RoundedRectangle(cornerRadius: .cornerRadiusSmall))
        .contentShape(Rectangle())
        .accessibilityElement(children: .combine)
    }

    /// The type made readable ("work_item" → "Work item"). The card itself
    /// shows the schema's display label once it loads.
    private var typeLabel: String {
        ToolActivityRun.humanized(citation.type)
    }
}

#Preview {
    SourcesFooter(
        citations: [
            ChatCitation(kind: "object", id: "00000000-0000-0000-0000-000000000001", type: "person", label: "Sam", url: "/objects/00000000-0000-0000-0000-000000000001"),
            ChatCitation(kind: "object", id: "00000000-0000-0000-0000-000000000002", type: "work_item", label: "Ship the card"),
        ],
        messageID: "preview"
    )
    .padding()
    .background(.bg1)
}
