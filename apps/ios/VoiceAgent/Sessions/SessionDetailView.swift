import SwiftUI

/// Session detail: the chronological conversation timeline for one recorded
/// session (`GET /api/session?room=...`), read-only.
///
/// User turns render as right-aligned bubbles, assistant turns left-aligned,
/// and `tools_executed` records as expandable tool-call cards (name,
/// arguments, result, error flag). A session the API cannot find renders as
/// an empty timeline rather than an error, and there is no create, edit, or
/// delete surface anywhere.
struct SessionDetailView: View {
    let room: String

    @Environment(\.dismiss) private var dismiss

    private enum LoadState: Equatable {
        case loading
        case loaded
        /// Empty timeline; carries the failure message when the fetch itself
        /// failed (unknown session, unreachable service) so the screen never
        /// shows a hard error for a missing session.
        case empty(String?)
    }

    @State private var records: [SessionRecord] = []
    @State private var state: LoadState = .loading

    var body: some View {
        VStack(spacing: 0) {
            header()
            content
        }
        .background(.bg1)
        .task(id: "session-detail-load") { await load() }
    }

    // MARK: - Loading

    private func load() async {
        state = .loading
        Log.net.info("session timeline room=\(room)")
        let client = SessionLogClient(config: MemoryConfig())
        do {
            let fetched = try await client.timeline(room: room)
            records = fetched
            Log.net.info("session timeline ok room=\(room) records=\(records.count)")
            state = fetched.isEmpty ? .empty(nil) : .loaded
        } catch {
            // Unknown session → empty timeline, not an error page.
            records = []
            Log.net.error("session timeline failed room=\(room): \(error.localizedDescription)")
            state = .empty(error.localizedDescription)
        }
    }

    // MARK: - Chrome

    private func header() -> some View {
        HStack(spacing: 2 * .grid) {
            Button {
                dismiss()
            } label: {
                Image(systemName: "chevron.left")
                    .font(.system(size: 16, weight: .medium))
                    .foregroundStyle(.fg3)
                    .padding(2 * .grid)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel("agents.level.back")

            VStack(alignment: .leading, spacing: 1 * .grid) {
                Text("sessions.detail.title")
                    .font(.system(size: 18, weight: .semibold))
                    .foregroundStyle(.fg0)
                Text(room)
                    .font(.system(size: 11, design: .monospaced))
                    .foregroundStyle(.fg3)
                    .lineLimit(1)
            }
            Spacer()
        }
        .padding(.horizontal, 4 * .grid)
        .padding(.top, 1 * .grid)
        .padding(.bottom, 2 * .grid)
    }

    // MARK: - Content

    @ViewBuilder
    private var content: some View {
        ZStack {
            timeline
            if case .loading = state {
                VStack(spacing: 3 * .grid) {
                    Spinner()
                    Text("sessions.loading")
                        .font(.system(size: 13))
                        .foregroundStyle(.fg3)
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .background(.bg1)
            }
            if case let .empty(message) = state {
                emptyView(message)
            }
        }
    }

    private func emptyView(_ message: String?) -> some View {
        ContentUnavailableView {
            Label("sessions.detail.empty", systemImage: "clock.arrow.circlepath")
        } description: {
            VStack(spacing: 1 * .grid) {
                Text("sessions.detail.empty.description")
                if let message {
                    Text(message)
                        .font(.system(size: 12))
                        .foregroundStyle(.fg3)
                }
            }
        }
    }

    // MARK: - Timeline

    /// Chronological items: user/assistant turn groups and tool-call records.
    private var items: [TimelineItem] {
        var result: [TimelineItem] = []
        for record in records {
            switch record.kind {
            case "turn":
                guard let role = record.role, let text = record.text, !text.isEmpty else { continue }
                if case let .turn(previousRole, previousText) = result.last, previousRole == role {
                    result[result.count - 1] = .turn(role: role, text: previousText + "\n" + text)
                } else {
                    result.append(.turn(role: role, text: text))
                }
            case "tools_executed":
                if record.calls?.isEmpty == false || record.outputs?.isEmpty == false {
                    result.append(.tools(record))
                }
            default:
                continue
            }
        }
        return result
    }

    private var timeline: some View {
        ScrollView {
            LazyVStack(spacing: 3 * .grid) {
                ForEach(Array(items.enumerated()), id: \.offset) { _, item in
                    switch item {
                    case let .turn(role, text):
                        TurnBubble(role: role, text: text)
                    case let .tools(record):
                        ToolCallsCard(tools: record.toolCallDisplay, isError: record.isError == true)
                    }
                }
            }
            .padding(4 * .grid)
        }
        .refreshable { await load() }
    }

    // MARK: - Timeline items

    private enum TimelineItem {
        case turn(role: String, text: String)
        case tools(SessionRecord)
    }
}

/// A `tools_executed` record flattened to the shared tool-call display model
/// (`ToolCallInfo`), mirroring the previous private `ToolsCard` mapping so the
/// timeline rendering is unchanged.
extension SessionRecord {
    var toolCallDisplay: [ToolCallInfo] {
        let calls = self.calls ?? []
        let outputs = self.outputs ?? []
        if calls.isEmpty {
            return outputs.enumerated().map { index, output in
                ToolCallInfo(
                    id: String(index),
                    name: String(format: String(localized: "sessions.detail.tool"), index + 1),
                    arguments: nil,
                    result: output,
                    isError: self.isError ?? false
                )
            }
        }
        return calls.enumerated().map { index, call in
            ToolCallInfo(
                id: String(index),
                name: call.name,
                arguments: call.arguments,
                result: call.result ?? (index < outputs.count ? outputs[index] : nil),
                isError: call.isError ?? (self.isError ?? false)
            )
        }
    }
}

/// One chat bubble: user turns right-aligned on an accent tint, assistant
/// turns left-aligned on the card background.
private struct TurnBubble: View {
    let role: String
    let text: String

    private var isUser: Bool { role == "user" }

    var body: some View {
        VStack(alignment: isUser ? .trailing : .leading, spacing: 1 * .grid) {
            Text(isUser ? "sessions.detail.user" : "sessions.detail.assistant")
                .font(.system(size: 11))
                .foregroundStyle(.fg3)
            Text(text)
                .font(.system(size: 15))
                .foregroundStyle(.fg0)
                .textSelection(.enabled)
                .padding(3 * .grid)
                .background(
                    RoundedRectangle(cornerRadius: .cornerRadiusSmall)
                        .fill(isUser ? AnyShapeStyle(.fgAccent.opacity(0.18)) : AnyShapeStyle(.bg2))
                )
        }
        .frame(maxWidth: .infinity, alignment: isUser ? .trailing : .leading)
    }
}
