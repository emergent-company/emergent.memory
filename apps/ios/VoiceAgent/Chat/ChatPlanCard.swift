import SwiftUI

/// The agent's plan for the open conversation, above the composer next to
/// ``SubagentTrackView`` (`emergent.memory#440`).
///
/// Collapsed by default and reading "Plan · done/total"; tapping expands the
/// ordered list, each row a status glyph plus the item's text. The three states
/// the chat draws a glyph for are pending, in progress and completed; anything
/// else renders as its raw text, so a status the server adds later still shows
/// up instead of disappearing. The card renders nothing while the plan is
/// empty, which is also how a chat with no ACP session looks.
///
/// The card sits in the composer stack, outside the transcript's `ScrollView`.
/// Growing or collapsing it changes the scroll view's viewport height rather
/// than its content, and ``ChatAutoScroll``/``ChatScrollPin`` treat a viewport
/// change as a geometry change: a reader who was pinned at the bottom stays
/// pinned, and a reader who had scrolled back is left where they are
/// (`emergent.memory#434`).
struct ChatPlanCard: View {
    /// The plan for the open conversation. Owned by ``ChatSessionView`` so a
    /// refresh frame and a turn's end can update the same card.
    let store: ChatPlanStore
    /// The conversation the plan belongs to; nil while a new chat has no id.
    let conversationID: String?

    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var expanded = false

    var body: some View {
        Group {
            if !store.todos.isEmpty {
                card
            }
        }
        // On open, and again when a new chat learns its id mid-turn. An empty
        // plan still leaves the Group mounted, so this runs before the card has
        // anything to show.
        .task(id: conversationID) {
            await store.load(conversationID: conversationID)
        }
    }

    private var card: some View {
        VStack(alignment: .leading, spacing: 0) {
            Button {
                withAnimation(reduceMotion ? nil : .snappy) {
                    expanded.toggle()
                }
            } label: {
                header
            }
            .buttonStyle(.plain)
            .accessibilityLabel("chat.plan.title")
            .accessibilityValue(Text(verbatim: progressText))
            .accessibilityHint("chat.plan.toggle")

            if expanded {
                VStack(alignment: .leading, spacing: 1 * .grid) {
                    ForEach(store.todos) { todo in
                        row(todo)
                    }
                }
                .padding(.top, 1 * .grid)
                .transition(reduceMotion ? .opacity : .opacity.combined(with: .move(edge: .top)))
            }
        }
        .padding(.horizontal, 3 * .grid)
        .padding(.vertical, 1 * .grid)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(.bg2, in: RoundedRectangle(cornerRadius: .cornerRadiusSmall))
        .overlay {
            RoundedRectangle(cornerRadius: .cornerRadiusSmall)
                .strokeBorder(.separator1.opacity(0.4), lineWidth: 1)
        }
        .padding(.horizontal, 4 * .grid)
        .padding(.bottom, 1 * .grid)
    }

    private var header: some View {
        HStack(spacing: 1 * .grid) {
            Image(systemName: "checklist")
                .font(.caption)
                .foregroundStyle(.fg3)
                .accessibilityHidden(true)

            Text("chat.plan.title")
                .font(MemoryFont.metaEmphasis)
                .foregroundStyle(.fg1)

            Text(verbatim: "·")
                .font(MemoryFont.meta)
                .foregroundStyle(.fg3)
                .accessibilityHidden(true)

            Text("chat.plan.progress \(store.doneCount) \(store.todos.count)")
                .font(MemoryFont.meta)
                .foregroundStyle(.fg2)

            Spacer(minLength: 1 * .grid)

            Image(systemName: "chevron.down")
                .font(.caption2.weight(.semibold))
                .rotationEffect(.degrees(reduceMotion ? 0 : (expanded ? 180 : 0)))
                .foregroundStyle(.fg3)
                .accessibilityHidden(true)
        }
        // Keep the row's visual height while giving the whole line a full tap
        // target, as the other composer pills do.
        .frame(minHeight: 32)
        .contentShape(Rectangle())
    }

    /// One plan item: the status glyph (or the raw status text) and its content,
    /// read as a single VoiceOver element.
    private func row(_ todo: SessionTodo) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 2 * .grid) {
            statusGlyph(todo)
            Text(verbatim: todo.content)
                .font(MemoryFont.bodySmall)
                .foregroundStyle(todo.status == SessionTodoStatus.completed.rawValue ? .fg3 : .fg1)
                .frame(maxWidth: .infinity, alignment: .leading)
                .fixedSize(horizontal: false, vertical: true)
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(Text(verbatim: todo.content))
        .accessibilityValue(Text(verbatim: statusText(todo)))
    }

    @ViewBuilder
    private func statusGlyph(_ todo: SessionTodo) -> some View {
        if let status = todo.knownStatus {
            switch status {
            case .pending:
                Image(systemName: "circle")
                    .font(.caption2)
                    .foregroundStyle(.fg3)
                    .accessibilityHidden(true)
            case .inProgress:
                Image(systemName: "circle.lefthalf.filled")
                    .font(.caption2)
                    .foregroundStyle(.fgAccent)
                    .accessibilityHidden(true)
            case .completed:
                Image(systemName: "checkmark.circle.fill")
                    .font(.caption2)
                    .foregroundStyle(.fgSuccess)
                    .accessibilityHidden(true)
            }
        } else {
            // A status this build does not know: show it rather than drop it.
            Text(verbatim: todo.status)
                .font(MemoryFont.meta)
                .foregroundStyle(.fg3)
                .lineLimit(1)
                .accessibilityHidden(true)
        }
    }

    private var progressText: String {
        String(localized: "chat.plan.progress \(store.doneCount) \(store.todos.count)")
    }

    private func statusText(_ todo: SessionTodo) -> String {
        if let status = todo.knownStatus {
            switch status {
            case .pending:
                return String(localized: "chat.plan.status.pending")
            case .inProgress:
                return String(localized: "chat.plan.status.inProgress")
            case .completed:
                return String(localized: "chat.plan.status.completed")
            }
        }
        return todo.status
    }
}
