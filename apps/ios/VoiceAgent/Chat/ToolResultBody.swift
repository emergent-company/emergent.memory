import SwiftUI

/// What an opened step's result reads as before the JSON: a list of records,
/// one record's fields, or a body of text — then a disclosure for the payload
/// itself.
///
/// The readable body carries no card or fill: it is the quiet version of the
/// result, and the raw block stays one tap away for anyone who needs the exact
/// wire shape.
struct ToolResultBody: View {
    let presentation: ToolResultPresentation

    /// The payload behind the disclosure, closed by default: the readable body
    /// is the answer, the JSON is the evidence.
    @State private var showRaw = false

    var body: some View {
        VStack(alignment: .leading, spacing: 1 * .grid) {
            Text("sessions.detail.result")
                .font(.caption2.weight(.semibold))
                .foregroundStyle(.fg3)

            if let body = presentation.body {
                readable(body)
                disclosure
            } else {
                // Nothing readable to show first: the payload is the body, and
                // it is already capped.
                rawBlock
            }
        }
    }

    // MARK: - Bodies

    @ViewBuilder
    private func readable(_ body: ToolResultPresentation.Body) -> some View {
        switch body {
        case let .items(items, more):
            VStack(alignment: .leading, spacing: 2 * .grid) {
                ForEach(items) { item in
                    itemRow(item)
                }
                if more > 0 {
                    moreLine("sessions.detail.result.more \(more)")
                }
            }
        case let .fields(fields, more):
            VStack(alignment: .leading, spacing: 1 * .grid) {
                ForEach(fields) { field in
                    fieldRow(field)
                }
                if more > 0 {
                    moreLine("sessions.detail.result.more \(more)")
                }
            }
        case let .text(text, moreLines, moreCharacters):
            VStack(alignment: .leading, spacing: 0) {
                Text(text)
                    .font(MemoryFont.bodySmall)
                    .foregroundStyle(.fg1)
                    .textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading)
                if moreLines > 0 {
                    moreLine("sessions.detail.result.truncated \(moreLines)")
                }
                if moreCharacters > 0 {
                    moreLine("sessions.detail.result.characters \(moreCharacters)")
                }
            }
        }
    }

    private func itemRow(_ item: ToolResultPresentation.Item) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            Text(item.title)
                .font(MemoryFont.bodySmall)
                .foregroundStyle(.fg1)
                .lineLimit(1)
            if let detail = item.detail {
                Text(detail)
                    .font(MemoryFont.meta)
                    .foregroundStyle(.fg2)
                    .lineLimit(1)
            }
            if let url = item.url, let host = URL(string: url)?.host {
                // The host, not the full URL: the page itself is one tap away in
                // the transcript's own reply.
                Text(host)
                    .font(MemoryFont.metaMono)
                    .foregroundStyle(.fg3)
                    .lineLimit(1)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
    }

    private func fieldRow(_ field: ToolResultPresentation.Field) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 2 * .grid) {
            Text(field.label)
                .font(MemoryFont.metaMono)
                .foregroundStyle(.fg3)
                .frame(minWidth: 72, alignment: .leading)
            Text(field.value)
                .font(MemoryFont.bodySmall)
                .foregroundStyle(.fg1)
                .lineLimit(2)
            Spacer(minLength: 0)
        }
        .accessibilityElement(children: .combine)
    }

    private func moreLine(_ key: LocalizedStringKey) -> some View {
        Text(key)
            .font(MemoryFont.meta)
            .foregroundStyle(.fg3)
    }

    // MARK: - Raw

    @ViewBuilder
    private var disclosure: some View {
        Button {
            showRaw.toggle()
        } label: {
            HStack(spacing: 1 * .grid) {
                Image(systemName: "chevron.right")
                    .font(.caption2.weight(.semibold))
                    .rotationEffect(.degrees(showRaw ? 90 : 0))
                    .accessibilityHidden(true)
                Text(showRaw ? "sessions.detail.result.raw.hide" : "sessions.detail.result.raw.show")
                    .font(MemoryFont.meta)
            }
            .foregroundStyle(.fg3)
            .frame(minHeight: 44)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)

        if showRaw {
            rawBlock
        }
    }

    private var rawBlock: some View {
        VStack(alignment: .leading, spacing: 1 * .grid) {
            Text(presentation.raw)
                .font(MemoryFont.metaMono)
                .foregroundStyle(.fg1)
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(2 * .grid)
                .background(.bg1, in: RoundedRectangle(cornerRadius: .cornerRadiusSmall))
            if presentation.rawMoreLines > 0 {
                moreLine("sessions.detail.result.truncated \(presentation.rawMoreLines)")
            }
            if presentation.rawMoreCharacters > 0 {
                moreLine("sessions.detail.result.characters \(presentation.rawMoreCharacters)")
            }
        }
    }
}
