import SwiftUI

/// The composer's staged messages, shown above the input.
///
/// One row per queued message: its text (editable, capped at about five lines),
/// **Send next**, and remove. The row's text stays live in the store, so an
/// edit that empties it leaves a row the user can still remove or refill.
struct ChatQueueRowsView: View {
    let items: [QueuedMessage]
    /// Whether a turn is running, which changes what the rows promise: an armed
    /// queue goes out when the turn ends, an idle one waits for Send next.
    let isRunning: Bool
    let onEdit: (String, String) -> Void
    let onSendNext: (String) -> Void
    let onRemove: (String) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 2 * .grid) {
            header
            ForEach(items) { item in
                row(item)
            }
            Text(LocalizedStringKey(isRunning ? "chat.queue.whenTurnFinishes" : "chat.queue.idleHint"))
                .font(MemoryFont.meta)
                .foregroundStyle(.fg3)
                .fixedSize(horizontal: false, vertical: true)
        }
        .padding(.horizontal, 3 * .grid)
        .padding(.vertical, 2 * .grid)
        .frame(maxWidth: 92 * .grid, alignment: .leading)
        .background(.bg2, in: RoundedRectangle(cornerRadius: .cornerRadiusLarge))
        .overlay {
            RoundedRectangle(cornerRadius: .cornerRadiusLarge)
                .strokeBorder(.separator1.opacity(0.4), lineWidth: 1)
        }
        .padding(.horizontal, 4 * .grid)
        .padding(.bottom, 1 * .grid)
    }

    private var header: some View {
        HStack(spacing: 2 * .grid) {
            Image(systemName: "list.todo")
                .font(MemoryFont.meta)
                .foregroundStyle(.fg3)
                .accessibilityHidden(true)
            Text("chat.queue.title \(items.count)")
                .font(MemoryFont.metaEmphasis)
                .foregroundStyle(.fg2)
            Spacer(minLength: 0)
        }
    }

    private func row(_ item: QueuedMessage) -> some View {
        VStack(alignment: .leading, spacing: 1 * .grid) {
            TextField(
                "chat.queue.edit",
                text: Binding(
                    get: { item.text },
                    set: { onEdit(item.id, $0) }
                ),
                axis: .vertical
            )
            .lineLimit(1 ... 5)
            .textFieldStyle(.plain)
            .font(MemoryFont.bodySmall)
            .foregroundStyle(.fg1)
            .fixedSize(horizontal: false, vertical: true)
            .accessibilityLabel("chat.queue.edit")
            HStack(spacing: 3 * .grid) {
                Button("chat.queue.sendNext") {
                    onSendNext(item.id)
                }
                .font(MemoryFont.metaEmphasis)
                .foregroundStyle(.fgAccent)
                .buttonStyle(.plain)
                .accessibilityHint(isRunning ? "chat.queue.sendNext.runningHint" : "chat.queue.sendNext.hint")
                Spacer(minLength: 0)
                Button {
                    onRemove(item.id)
                } label: {
                    Image(systemName: "xmark")
                        .font(MemoryFont.meta)
                        .frame(minWidth: 28, minHeight: 28)
                }
                .buttonStyle(.plain)
                .foregroundStyle(.fg2)
                .accessibilityLabel("chat.queue.remove")
            }
        }
        .padding(.top, 1 * .grid)
    }
}
