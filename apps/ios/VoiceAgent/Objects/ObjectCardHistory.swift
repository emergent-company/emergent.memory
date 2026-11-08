import SwiftUI

// MARK: - History

/// The version timeline, newest first, fetched when the section first
/// appears. Each entry is a disclosure: version, status and age on the line,
/// its change list when opened.
struct ObjectCardHistoryRows: View {
    let store: ObjectCardStore

    var body: some View {
        Group {
            switch store.historyState {
            case .idle, .loading:
                HStack(spacing: 2 * .grid) {
                    ProgressView()
                    Text("objects.card.history.loading")
                        .foregroundStyle(.fg3)
                }
                .font(MemoryFont.bodySmall)
            case .failed:
                HStack {
                    Label("objects.card.history.unavailable", systemImage: "clock.badge.exclamationmark")
                        .font(MemoryFont.bodySmall)
                        .foregroundStyle(.fg3)
                    Spacer()
                    Button("objects.retry") {
                        Task { await store.loadHistoryIfNeeded() }
                    }
                    .font(MemoryFont.labelEmphasis)
                }
            case .loaded:
                if store.history.isEmpty {
                    Label("objects.card.history.empty", systemImage: "clock.arrow.circlepath")
                        .font(MemoryFont.bodySmall)
                        .foregroundStyle(.fg3)
                } else {
                    ForEach(store.history) { version in
                        ObjectCardVersionRow(version: version)
                    }
                }
            }
        }
        .task { await store.loadHistoryIfNeeded() }
    }
}

struct ObjectCardVersionRow: View {
    let version: ObjectCardVersion

    var body: some View {
        DisclosureGroup {
            VStack(alignment: .leading, spacing: 1 * .grid) {
                ForEach(Array(version.changes.enumerated()), id: \.offset) { _, change in
                    changeLine(change)
                }
            }
            .padding(.vertical, 1 * .grid)
        } label: {
            VStack(alignment: .leading, spacing: 1 * .grid) {
                HStack(spacing: 2 * .grid) {
                    Text(verbatim: version.label)
                        .font(.system(.footnote, design: .monospaced, weight: .semibold))
                        .foregroundStyle(.fg0)
                    if let status = version.status {
                        ObjectStatusBadge(status: status)
                    }
                    Spacer(minLength: 0)
                    if let date = DateFormatting.objectDate(version.createdAt) {
                        Text(date, format: .relative(presentation: .named))
                            .font(MemoryFont.meta)
                            .foregroundStyle(.fg3)
                    }
                }
                if !version.key.isEmpty {
                    Text(verbatim: version.key)
                        .font(MemoryFont.meta)
                        .foregroundStyle(.fg3)
                        .lineLimit(2)
                }
            }
        }
        .tint(.fg3)
    }

    @ViewBuilder
    private func changeLine(_ change: ObjectCardChange) -> some View {
        let mono = Font.system(.caption, design: .monospaced)
        switch change.kind {
        case .add:
            Text(verbatim: "+ \(change.label)")
                .font(mono)
                .foregroundStyle(.fgSuccess)
        case .remove:
            Text(verbatim: "− \(change.label)")
                .font(mono)
                .foregroundStyle(.fgSerious)
        case .update:
            Text(verbatim: "\(change.label): \(change.before) → \(change.after)")
                .font(mono)
                .foregroundStyle(.fg1)
        case .changed:
            Text("objects.card.history.changed \(change.label)")
                .font(mono)
                .foregroundStyle(.fg1)
        case .note:
            Text(verbatim: change.label)
                .font(MemoryFont.meta)
                .foregroundStyle(.fg3)
        }
    }
}
