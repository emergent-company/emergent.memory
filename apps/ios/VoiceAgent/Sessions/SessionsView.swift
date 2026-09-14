import SwiftUI

/// Route to one recorded session's detail (pushed from the Sessions list).
struct SessionRoute: Hashable {
    let room: String
}

/// Sessions destination (second level): lists the recorded sessions for the
/// selected agent, most recently active first, via `SessionLogClient`.
///
/// Read-only — no create, edit, or delete surface. Shows an empty state when
/// the agent has no recorded sessions and an error state (with retry) when
/// the session-log API is unreachable.
struct SessionsView: View {
    /// The second-level agent this list belongs to. The session-log API has
    /// no per-agent filter, so the full list is fetched; the agent scope is
    /// provided by the second level itself.
    let agentName: String

    private enum LoadState: Equatable {
        case loading
        case loaded
        case failed(String)
    }

    @State private var sessions: [SessionSummary] = []
    @State private var state: LoadState = .loading

    var body: some View {
        ZStack {
            if sessions.isEmpty {
                emptyView()
            } else {
                list
            }
            if case .loading = state {
                loadingView()
                    .background(.bg1)
            }
            if case let .failed(message) = state {
                failureView(message)
                    .background(.bg1)
            }
        }
        .background(.bg1)
        .task(id: "sessions-load") { await load() }
    }

    // MARK: - Loading

    private func load() async {
        state = .loading
        Log.net.info("listSessions")
        let client = SessionLogClient(config: MemoryConfig())
        do {
            sessions = try await client.listSessions()
            Log.net.info("listSessions ok \(sessions.count)")
            state = .loaded
        } catch {
            Log.net.error("listSessions failed: \(error.localizedDescription)")
            state = .failed(error.localizedDescription)
        }
    }

    private func loadingView() -> some View {
        VStack(spacing: 3 * .grid) {
            Spinner()
            Text("sessions.loading")
                .font(.system(size: 13))
                .foregroundStyle(.fg3)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    // MARK: - List

    private var list: some View {
        List(sessions) { summary in
            NavigationLink(value: AppRoute.session(SessionRoute(room: summary.room))) {
                SessionRow(summary: summary)
            }
            .listRowBackground(Color.clear)
        }
        .listStyle(.plain)
        .scrollContentBackground(.hidden)
        .refreshable { await load() }
    }

    private func emptyView() -> some View {
        ContentUnavailableView {
            Label("sessions.empty", systemImage: "clock")
        } description: {
            Text("sessions.empty.description")
        }
    }

    private func failureView(_ message: String) -> some View {
        ContentUnavailableView {
            Label("sessions.error", systemImage: "wifi.exclamationmark")
        } description: {
            VStack(spacing: 1 * .grid) {
                Text("sessions.error.description")
                Text(message)
                    .font(.system(size: 12))
                    .foregroundStyle(.fg3)
            }
        } actions: {
            Button {
                Task { await load() }
            } label: {
                Text("sessions.retry")
                    .font(.system(size: 14, weight: .semibold))
                    .foregroundStyle(.fgAccent)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .padding(.top, 2 * .grid)
        }
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
            .font(.system(size: 15))
            .foregroundStyle(.fg1)
            .lineLimit(2)
            .multilineTextAlignment(.leading)

            HStack(spacing: 2 * .grid) {
                if let startedAt = summary.startedAt {
                    Text("sessions.started \(startedAt)")
                }
                Text("sessions.turns.count \(summary.turns)")
                Text("sessions.toolCalls.count \(summary.toolCalls)")
                if summary.endedAt == nil {
                    liveBadge
                }
            }
            .font(.system(size: 11))
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
