import Foundation
import LiveKit
import LiveKitComponents
import Observation

/// Speaker of one transcript message.
enum ChatRole: String, Equatable, Sendable {
    case user
    case assistant
}

/// One rendered transcript line (history or live).
struct ChatMessage: Identifiable, Equatable, Sendable {
    let id: String
    let role: ChatRole
    let text: String
}

/// One entry of the unified transcript: a chat message or the tool calls that
/// ran between messages.
enum HistoryEntry: Equatable {
    case message(ChatMessage)
    case tools([ToolCallInfo])
}

/// SDK-independent live transcript input, so the merge/dedupe rules are
/// testable without constructing LiveKit messages.
struct LiveTranscriptMessage: Equatable, Sendable {
    let id: String
    let role: ChatRole
    let text: String
}

/// Backs the unified chat: recorded history from `GET /api/session`, live
/// transcripts from the LiveKit room, and pending approvals from the session
/// detail, all in one ordered transcript.
///
/// History and live messages are kept separate so a reconnect can reload
/// history without losing the live stream, and the view can stitch them. Live
/// messages already present in history are dropped (`syncLive`) so a restored
/// transcript never duplicates.
@MainActor
@Observable
final class ChatSessionTranscriptStore {
    /// Recorded conversation timeline (messages + tool runs).
    private(set) var history: [HistoryEntry] = []
    /// Live transcripts from the room, excluding anything already in history.
    private(set) var liveMessages: [ChatMessage] = []
    /// Approvals waiting on the user.
    private(set) var pendingApprovals: [ApprovalRequest] = []
    private(set) var isLoadingHistory = false
    private(set) var historyError: String?
    /// Raw record count of the loaded detail. Polling for a resumed run must
    /// compare like with like: `history` merges/skips records, so its count is
    /// not the server's record count.
    private(set) var lastRecordCount = 0

    init() {}

    /// Replaces history + pending approvals from a fetched session detail.
    ///
    /// Consecutive `turn` records with the same role are merged with a newline,
    /// matching the session-detail timeline; a `tools_executed` record between
    /// turns breaks the run.
    func loadHistory(from detail: SessionDetail) {
        var entries: [HistoryEntry] = []
        for record in detail.records {
            switch record.kind {
            case "turn":
                guard let roleText = record.role, let text = record.text, !text.isEmpty else { continue }
                let role: ChatRole = roleText == "user" ? .user : .assistant
                if case let .message(previous)? = entries.last, previous.role == role {
                    entries[entries.count - 1] = .message(ChatMessage(
                        id: previous.id,
                        role: role,
                        text: previous.text + "\n" + text
                    ))
                } else {
                    entries.append(.message(ChatMessage(
                        id: "history-\(entries.count)",
                        role: role,
                        text: text
                    )))
                }
            case "tools_executed":
                let tools = record.toolCallDisplay
                if !tools.isEmpty {
                    entries.append(.tools(tools))
                }
            default:
                continue
            }
        }

        history = entries
        // A turn streamed over HTTP is persisted server-side, so a reload must
        // not leave its live copy on top of the recorded one.
        liveMessages.removeAll { historyCovers(role: $0.role, text: $0.text) }
        lastRecordCount = detail.records.count
        pendingApprovals = detail.pendingApprovals.map {
            ApprovalRequest(questionId: $0.questionID, tool: $0.tool, arguments: $0.arguments)
        }
        historyError = nil
    }

    /// Replaces the live transcript from the room's message list.
    ///
    /// Messages are keyed by their LiveKit id (a streaming update replaces the
    /// previous partial), and anything already present in history is dropped so
    /// a restored conversation is not duplicated.
    func syncLive(messages: [ReceivedMessage]) {
        syncLive(messages.map { message in
            let role: ChatRole
            let text: String
            switch message.content {
            case let .userInput(content):
                role = .user
                text = content
            case let .userTranscript(content):
                role = .user
                text = content
            case let .agentTranscript(content):
                role = .assistant
                text = content
            }
            return LiveTranscriptMessage(id: message.id, role: role, text: text)
        })
    }

    /// Applies already-mapped live transcripts (see ``syncLive(messages:)``).
    func syncLive(_ messages: [LiveTranscriptMessage]) {
        var live: [ChatMessage] = []
        for message in messages {
            guard !message.text.isEmpty else { continue }
            let chat = ChatMessage(id: message.id, role: message.role, text: message.text)
            if let index = live.firstIndex(where: { $0.id == chat.id }) {
                live[index] = chat
            } else {
                live.append(chat)
            }
        }

        liveMessages = live.filter { !historyCovers(role: $0.role, text: $0.text) }
    }

    /// Whether the recorded transcript already contains this live message.
    ///
    /// Exact text first, then line-bounded containment: `loadHistory` merges
    /// consecutive turns of the same role with a newline, so the live halves of a
    /// resumed run only match that merged record line by line. Line-bounded (not
    /// any substring) so a short reply cannot be hidden by a longer older one.
    private func historyCovers(role: ChatRole, text: String) -> Bool {
        let recorded = history.compactMap { entry -> String? in
            guard case let .message(message) = entry, message.role == role else { return nil }
            return message.text
        }
        if recorded.contains(text) {
            return true
        }
        let needle = text.split(separator: "\n", omittingEmptySubsequences: false).map(String.init)
        guard !needle.isEmpty else { return true }
        return recorded.contains { candidate in
            let lines = candidate.split(separator: "\n", omittingEmptySubsequences: false).map(String.init)
            guard lines.count >= needle.count else { return false }
            for start in 0 ... (lines.count - needle.count) where Array(lines[start ..< start + needle.count]) == needle {
                return true
            }
            return false
        }
    }

    /// Marks the start of a history fetch (the previous result stays visible
    /// while the request runs).
    func beginHistoryLoad() {
        isLoadingHistory = true
        historyError = nil
    }

    /// Records a failed history fetch without discarding the live transcript or
    /// blocking new turns; the view shows it as a non-fatal banner.
    func historyLoadFailed(_ message: String) {
        isLoadingHistory = false
        historyError = message
    }

    /// Removes `questionId` from the pending list (the user answered it).
    func markApprovalAnswered(questionId: String) {
        pendingApprovals.removeAll { $0.questionId == questionId }
    }

    /// Clears all state for a new session.
    func reset() {
        history = []
        liveMessages = []
        pendingApprovals = []
        historyError = nil
        isLoadingHistory = false
        lastRecordCount = 0
    }
}
