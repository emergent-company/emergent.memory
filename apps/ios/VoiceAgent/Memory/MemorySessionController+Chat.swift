import Foundation
import LiveKit

private struct AgentEvent: Decodable {
    let type: String
    let newState: String?

    enum CodingKeys: String, CodingKey {
        case type
        case newState = "new_state"
    }
}

extension MemorySessionController {
    func handleAgentEvent(_ body: String) {
        guard
            let data = body.data(using: .utf8),
            let event = try? JSONDecoder().decode(AgentEvent.self, from: data)
        else { return }

        if event.type == "user_state_changed", event.newState == "away" {
            scheduleSessionEnd()
        }
    }

    /// Splits the stream body into per-message JSON lines and routes each
    /// decodable `ChatEvent` into the activity store. Unknown `type` values
    /// decode to `nil` and are ignored.
    func handleChatEvents(_ body: String) {
        let lines = body
            .split(whereSeparator: \.isNewline)
            .map(String.init)
            .filter { !$0.trimmingCharacters(in: .whitespaces).isEmpty }
        for line in lines {
            guard let event = decodeChatEvent(line) else { continue }
            handleChatEvent(event)
        }
    }

    /// Routes one decoded worker event. Conversation-id events are recorded on
    /// the controller (and forwarded to the UI store); everything else feeds
    /// the live activity store.
    func handleChatEvent(_ event: ChatEvent) {
        if case let .conversation(conversationID) = event {
            guard liveConversationID != conversationID else { return }
            liveConversationID = conversationID
            onConversationLearned?(conversationID)
            return
        }
        chatActivity.apply(event)
    }

    // MARK: - Rich-chat decisions (iOS chat parity)

    /// Sends a card decision to the worker over `lk.chat.decision`.
    func sendDecision(_ decision: ChatDecision) async {
        let payload: String
        do {
            payload = try encodeChatDecision(decision)
        } catch {
            Log.session.error("chat decision encode failed: \(error.localizedDescription)")
            return
        }
        TraceLog.log("chat_decision_sent", ["decision": payload], room: session.room.name ?? "")
        _ = try? await session.room.localParticipant.sendText(payload, for: Self.chatDecisionTopic)
    }

    /// Interrupts the current agent generation by sending empty text on
    /// `lk.chat.interrupt` (the LiveKit `Session` API has no `interrupt()`;
    /// this never calls `session.end()`).
    func interrupt() async {
        TraceLog.log("chat_interrupt", room: session.room.name ?? "")
        _ = try? await session.room.localParticipant.sendText("", for: Self.chatInterruptTopic)
    }
}
