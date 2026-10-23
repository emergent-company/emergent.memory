import SwiftUI

/// Session detail: the chronological conversation timeline for one recorded
/// session (`GET /api/session?room=...`), read-only, via ``SessionDetailStore``.
///
/// User turns render as right-aligned bubbles, assistant turns left-aligned,
/// and `tools_executed` records as expandable tool-call cards (name,
/// arguments, result, error flag). A session the API cannot find renders as
/// an empty timeline rather than an error, and there is no create, edit, or
/// delete surface anywhere.
struct SessionDetailView: View {
    let room: String

    @Environment(\.dismiss) private var dismiss
    @StateObject private var store = SessionDetailStore()

    var body: some View {
        VStack(spacing: 0) {
            ScreenHeader(
                title: Text("sessions.detail.title"),
                subtitle: Text(room),
                subtitleFont: MemoryFont.metaMono
            ) {
                dismiss()
            }
            content
        }
        .background(.bg1)
        // Exactly one back affordance: the ScreenHeader chevron. The system
        // navigation bar and its back button are hidden so a pushed session
        // cannot show two of them.
        #if os(iOS)
            .toolbar(.hidden, for: .navigationBar)
            .navigationBarBackButtonHidden(true)
        #endif
            .task(id: "session-detail-load") { await store.load(room: room) }
    }

    // MARK: - Content

    private var content: some View {
        ZStack {
            timeline
            if store.loadState == .loading {
                LoadingStateView(title: "sessions.loading")
                    .background(.bg1)
            }
            if store.loadState == .loaded, store.records.isEmpty {
                EmptyStateView(
                    title: "sessions.detail.empty",
                    description: "sessions.detail.empty.description",
                    systemImage: "clock.arrow.circlepath",
                    message: store.failureMessage
                )
            }
        }
    }

    // MARK: - Timeline

    /// Chronological items: user/assistant turn groups and tool-call records.
    private var items: [TimelineItem] {
        var result: [TimelineItem] = []
        for record in store.records {
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
        .refreshable { await store.load(room: room) }
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
        let calls = calls ?? []
        let outputs = outputs ?? []
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

    private var conversationRole: ConversationRole {
        role == "user" ? .user : .assistant
    }

    var body: some View {
        let role = conversationRole
        VStack(alignment: role.horizontalAlignment, spacing: 1 * .grid) {
            Text(role.labelKey)
                .font(MemoryFont.meta)
                .foregroundStyle(.fg3)
            Text(text)
                .font(MemoryFont.conversationBody)
                .foregroundStyle(.fg0)
                .textSelection(.enabled)
                .padding(3 * .grid)
                .bubbleBackground(role, cornerRadius: .cornerRadiusSmall)
        }
        .frame(maxWidth: .infinity, alignment: role.frameAlignment)
    }
}
