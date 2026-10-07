import SwiftUI

/// Route to one recorded session's detail (pushed from the Sessions list).
struct SessionRoute: Hashable {
    let room: String
}

/// Sessions destination (second level): lists the recorded sessions for the
/// selected agent, most recently active first, via ``SessionsStore``.
///
/// Read-only — no create, edit, or delete surface. Shows an empty state when
/// the agent has no recorded sessions and an error state (with retry) when
/// the session-log API is unreachable.
struct SessionsView: View {
    /// The second-level agent this list belongs to. The session-log API has
    /// no per-agent filter, so the full list is fetched; the agent scope is
    /// provided by the second level itself.
    let agentName: String

    @StateObject private var store = SessionsStore()

    var body: some View {
        ZStack {
            if store.sessions.isEmpty {
                emptyView()
            } else {
                list
            }
            if store.loadState == .loading {
                LoadingStateView(title: "sessions.loading")
                    .background(.bg1)
            }
            if case let .failed(message) = store.loadState {
                failureView(message)
                    .background(.bg1)
            }
        }
        .background(.bg1)
        .task(id: "sessions-load") { await store.load() }
    }

    // MARK: - List

    private var list: some View {
        List(store.sessions) { summary in
            NavigationLink(value: AppRoute.session(SessionRoute(room: summary.room))) {
                SessionRow(summary: summary)
            }
            .listRowBackground(Color.clear)
        }
        .listStyle(.plain)
        .scrollContentBackground(.hidden)
        .refreshable { await store.load() }
    }

    private func emptyView() -> some View {
        EmptyStateView(
            title: "sessions.empty",
            description: "sessions.empty.description",
            systemImage: "clock"
        )
    }

    private func failureView(_ message: String) -> some View {
        ErrorStateView(
            title: "sessions.error",
            description: "sessions.error.description",
            message: message,
            retryLabel: "sessions.retry",
            retry: { Task { await store.load() } }
        )
    }
}

/// One session row: preview text, started time, turn/tool-call counts, and a
/// "Live" badge while the session has not ended.
private struct SessionRow: View {
    let summary: SessionSummary

    var body: some View {
        VStack(alignment: .leading, spacing: 2 * .grid) {
            Group {
                if let preview = summary.preview, !preview.isEmpty {
                    Text(preview)
                } else {
                    Text("sessions.preview.placeholder")
                        .foregroundStyle(.fg3)
                }
            }
            .font(MemoryFont.bodySmall)
            .foregroundStyle(.fg1)
            .lineLimit(2)
            .multilineTextAlignment(.leading)

            HStack(spacing: 2 * .grid) {
                if let startedAt = summary.startedAt {
                    Text("sessions.started \(DateFormatting.formatSessionDate(startedAt) ?? startedAt)")
                }
                Text("sessions.turns.count \(summary.turns)")
                Text("sessions.toolCalls.count \(summary.toolCalls)")
                if summary.endedAt == nil {
                    liveBadge
                }
            }
            .font(MemoryFont.meta)
            .foregroundStyle(.fg3)
        }
        .padding(.vertical, 1 * .grid)
    }

    private var liveBadge: some View {
        HStack(spacing: 1 * .grid) {
            Circle()
                .fill(.fgSuccess)
                .frame(width: 6, height: 6)
            Text("sessions.live")
        }
    }
}
