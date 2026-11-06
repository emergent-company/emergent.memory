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
        // This is the success exit of a fetch that `beginHistoryLoad()` opened,
        // but it is also called directly by the recovery and resumed-approval
        // reloads, which never set the flag. Settle the loading state on every
        // exit, including a detail with no records: a loaded-but-empty
        // conversation otherwise keeps `isLoadingHistory` set, so the view shows
        // "Loading earlier messages" forever and its empty state — which requires
        // `!isLoadingHistory` — can never appear.
        defer {
            isLoadingHistory = false
            historyError = nil
        }

        history = Self.entries(from: detail.records)
        // A turn streamed over HTTP is persisted server-side, so a reload must
        // not leave its live copy on top of the recorded one.
        liveMessages.removeAll { historyCovers(role: $0.role, text: $0.text) }
        lastRecordCount = detail.records.count
        pendingApprovals = detail.pendingApprovals.map {
            ApprovalRequest(questionId: $0.questionID, tool: $0.tool, arguments: $0.arguments)
        }
    }

    /// The rendered entries for one detail's records: consecutive same-role
    /// turns merged with a newline, `tools_executed` kept between runs.
    ///
    /// This is the *rendering* shape. ``recordedReply(extending:in:since:)``
    /// deliberately judges the flattened reply instead: a tool call between two
    /// assistant segments splits the rendered timeline but not the reply.
    private static func entries(from records: [SessionRecord]) -> [HistoryEntry] {
        var entries: [HistoryEntry] = []
        for record in records {
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
        return entries
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

    /// Whether a fetched detail holds the finished version of a reply this client
    /// saw cut off — the signal that a dropped run has been recorded.
    ///
    /// Three conditions, each earning its place:
    /// - the record grew past `baseline`, so an older reply cannot pass for this
    ///   one just because it starts the same way;
    /// - some suffix of the reply's assistant turns opens with what the client
    ///   already saw;
    /// - it carries more than that, so a record still holding only the truncated
    ///   copy keeps the wait going.
    ///
    /// Suffixes, not one concatenation, because the client's partial can start
    /// part-way through the recorded reply:
    /// - rendering splits entries at a `tools_executed` record, but a reply that
    ///   called a tool is still one reply: `"hi the"`, a tool call, `"re"` is
    ///   `"hi there"`;
    /// - the gateway records a function-call-only step as an assistant turn of
    ///   its own, `"(calls: …)"` (`apps/web-ui/gateway/client_api.go`,
    ///   `messageRecord`), so a reply with no preamble to its first tool step is
    ///   stored as `"(calls: …)"` and then the answer — the text the client saw
    ///   starts after that placeholder.
    ///
    /// Judging only the last rendered entry was what #213 found broken; judging
    /// one concatenation only moves the break to the placeholder shape.
    static func recordedReply(extending partial: String, in detail: SessionDetail, since baseline: Int) -> Bool {
        let opening = partial.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !opening.isEmpty, detail.records.count > baseline else { return false }
        return replySuffixes(from: detail.records).contains {
            $0.count > opening.count && $0.hasPrefix(opening)
        }
    }

    /// The reply to the turn the client is waiting on — every assistant turn
    /// after the last user turn — as the text from each turn onward: the client's
    /// partial may start at any of them, and each continuation extends into the
    /// ones after it.
    private static func replySuffixes(from records: [SessionRecord]) -> [String] {
        var turns: [String] = []
        for record in records {
            switch record.kind {
            case "turn":
                guard let role = record.role else { continue }
                if role == "user" {
                    // A new user turn starts a new reply; anything recorded for
                    // an earlier one is not the continuation being waited on.
                    turns.removeAll()
                    continue
                }
                guard let text = record.text else { continue }
                turns.append(text)
            default:
                // `tools_executed` between assistant segments does not split the
                // reply; other record kinds carry no reply text.
                continue
            }
        }
        return turns.indices.map { turns[$0...].joined() }
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

    /// Records that a history fetch was abandoned — the view went away, or a
    /// newer load replaced it — without turning it into a failure the user
    /// would be asked to retry.
    func cancelHistoryLoad() {
        isLoadingHistory = false
        historyError = nil
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
