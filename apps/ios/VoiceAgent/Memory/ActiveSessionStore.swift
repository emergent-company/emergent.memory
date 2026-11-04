import Foundation
import Observation

/// How the user is talking to the active agent.
enum ChatMode: String, CaseIterable {
    case text
    case voice
}

/// The single source of truth for "which conversation is open right now".
///
/// A session is one Memory conversation backed by one LiveKit room. The user
/// picks an agent (and optionally an existing conversation) before the room
/// connects; the worker reports the actual conversation id once the server
/// reveals it. `launchConversationID` is what the token mint asks to continue,
/// `liveConversationID` is what the worker reported. They differ only for a
/// brand-new conversation (launch id nil until the server creates one).
@MainActor
@Observable
final class ActiveSessionStore {
    /// Display name of the agent for the open session.
    private(set) var agentName: String = ""
    /// Conversation id requested at token-mint time (nil = start a new one).
    private(set) var launchConversationID: String?
    /// Conversation id reported by the worker (nil until learned).
    private(set) var liveConversationID: String?
    /// Which interaction surface is showing.
    var mode: ChatMode = .text
    /// Whether the room should join with the microphone muted (chat-first
    /// sessions). The Conversation tab keeps the voice-first default.
    private(set) var startsMuted = false

    /// Opens an existing conversation (or a fresh one when `conversationID` is
    /// nil) and resets the transient live conversation id.
    func open(agentName: String, conversationID: String?, startsMuted: Bool = false) {
        self.agentName = agentName
        launchConversationID = conversationID
        liveConversationID = nil
        mode = .text
        self.startsMuted = startsMuted
    }

    /// Starts a brand-new conversation with `agentName`.
    func startNew(agentName: String, startsMuted: Bool = false) {
        open(agentName: agentName, conversationID: nil, startsMuted: startsMuted)
    }

    /// Records the conversation id the worker reported. Never changes the
    /// launch id: the room was minted for the original request.
    func learnConversationID(_ conversationID: String) {
        guard liveConversationID != conversationID else { return }
        liveConversationID = conversationID
    }

    /// Stable key for SwiftUI `.task(id:)` / `.onChange(of:)`: changes only when
    /// a new session is launched, not when the live id is learned.
    var launchKey: String {
        "\(agentName)|\(launchConversationID ?? "new")"
    }
}
