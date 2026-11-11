import SwiftUI

/// Route to one recorded session's read-only detail (pushed from Chats for
/// scheduled and board sessions).
struct SessionRoute: Hashable {
    let room: String
}

// MARK: - Row

/// One conversation row: where it came from, its title (or first message),
/// started time, turn/tool-call counts, and the archived/live state.
struct ChatRow: View {
    let summary: SessionSummary

    var body: some View {
        VStack(alignment: .leading, spacing: 2 * .grid) {
            HStack(spacing: 2 * .grid) {
                originBadge
                title
                if summary.isArchived {
                    archivedBadge
                }
                Spacer(minLength: 0)
                if summary.endedAt == nil {
                    liveBadge
                }
                SessionRunBadge(summary: summary)
            }

            if showsSeparatePreview, let preview = summary.preview, !preview.isEmpty {
                Text(preview)
                    .font(MemoryFont.bodySmall)
                    .foregroundStyle(.fg2)
                    .lineLimit(2)
                    .multilineTextAlignment(.leading)
            }

            HStack(spacing: 2 * .grid) {
                if let startedAt = summary.startedAt {
                    Text("sessions.started \(DateFormatting.formatSessionDate(startedAt) ?? startedAt)")
                }
                Text("sessions.turns.count \(summary.turns)")
                Text("sessions.toolCalls.count \(summary.toolCalls)")
            }
            .font(MemoryFont.meta)
            .foregroundStyle(.fg3)
        }
        .padding(.vertical, 1 * .grid)
    }

    /// The title line: the conversation title when there is one, otherwise the
    /// first message, otherwise a neutral placeholder.
    @ViewBuilder
    private var title: some View {
        if let title = summary.title, !title.isEmpty {
            Text(title)
                .font(MemoryFont.cardHeader)
                .foregroundStyle(.fg0)
                .lineLimit(1)
        } else if let preview = summary.preview, !preview.isEmpty {
            Text(preview)
                .font(MemoryFont.cardHeader)
                .foregroundStyle(.fg0)
                .lineLimit(1)
        } else {
            Text("chats.preview.placeholder")
                .font(MemoryFont.cardHeader)
                .foregroundStyle(.fg3)
                .lineLimit(1)
        }
    }

    /// The preview becomes a second line only when the title is not already the
    /// first message.
    private var showsSeparatePreview: Bool {
        guard let title = summary.title, !title.isEmpty else { return false }
        return title != summary.preview
    }

    private var originBadge: some View {
        Text(originKey)
            .font(.caption2.weight(.semibold))
            .foregroundStyle(.fg2)
            .padding(.horizontal, 2 * .grid)
            .padding(.vertical, 1)
            .overlay(Capsule().stroke(.separator1, lineWidth: 1))
    }

    private var originKey: LocalizedStringKey {
        switch summary.origin {
        case "scheduled":
            "chats.origin.scheduled"
        case "board":
            "chats.origin.board"
        default:
            "chats.origin.chat"
        }
    }

    private var archivedBadge: some View {
        Label("chats.archived", systemImage: "archivebox")
            .font(MemoryFont.meta)
            .foregroundStyle(.fg3)
            .labelStyle(.titleAndIcon)
    }

    private var liveBadge: some View {
        HStack(spacing: 1 * .grid) {
            Circle()
                .fill(.fgSuccess)
                .frame(width: 6, height: 6)
            Text("sessions.live")
        }
        .font(MemoryFont.meta)
        .foregroundStyle(.fg3)
    }
}

/// The row's run-state badge. It mirrors the web rail's three states from the
/// gateway `bucket` field: Working, Waiting on you (with the combined pending
/// decision count), and Failed. A done/absent bucket renders nothing.
struct SessionRunBadge: View {
    let summary: SessionSummary

    private var pendingCount: Int {
        summary.pendingApprovals + summary.pendingQuestions
    }

    var body: some View {
        if let label {
            Text(label)
                .font(.caption2.weight(.semibold))
                .foregroundStyle(tint)
                .padding(.horizontal, 2 * .grid)
                .padding(.vertical, 1)
                .background(tint.opacity(0.12), in: Capsule())
        }
    }

    /// `nil` for done or an unknown/absent bucket, so no empty badge is drawn.
    private var label: String? {
        switch summary.bucket {
        case "needs_input":
            pendingCount > 0
                ? String(localized: "chats.badge.waiting.count \(pendingCount)")
                : String(localized: "chats.badge.waiting")
        case "running":
            String(localized: "chats.badge.working")
        case "failed":
            String(localized: "chats.badge.failed")
        default:
            nil
        }
    }

    private var tint: Color {
        switch summary.bucket {
        case "failed": .fgSerious
        case "needs_input": .fgModerate
        case "running": .fgAccent
        default: .fg3
        }
    }
}
