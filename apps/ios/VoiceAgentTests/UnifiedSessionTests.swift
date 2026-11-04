import Foundation
@testable import Memory
import Testing

// MARK: - Conversation event

struct ConversationEventTests {
    @Test func decodesConversationEvent() {
        guard case let .conversation(conversationID)? = decodeChatEvent(
            #"{"type":"conversation","conversationId":"conv-1"}"#
        ) else {
            Issue.record("Expected a conversation event")
            return
        }
        #expect(conversationID == "conv-1")
    }

    @Test func malformedConversationEventIsNil() {
        #expect(decodeChatEvent(#"{"type":"conversation"}"#) == nil)
        #expect(decodeChatEvent(#"{"type":"conversation","conversationId":1}"#) == nil)
        #expect(decodeChatEvent(#"{"type":"conversation","conversationId":""}"#) != nil)
    }
}

// MARK: - Active session store

@MainActor
struct ActiveSessionStoreTests {
    @Test func openSetsAgentAndLaunchConversation() {
        let store = ActiveSessionStore()
        store.open(agentName: "Alfred", conversationID: "conv-1")

        #expect(store.agentName == "Alfred")
        #expect(store.launchConversationID == "conv-1")
        #expect(store.liveConversationID == nil)
        #expect(store.mode == .text)
    }

    @Test func startNewClearsLaunchConversation() {
        let store = ActiveSessionStore()
        store.open(agentName: "Alfred", conversationID: "conv-1")
        store.mode = .voice

        store.startNew(agentName: "Alfred")

        #expect(store.launchConversationID == nil)
        #expect(store.liveConversationID == nil)
        #expect(store.mode == .text)
    }

    @Test func learningConversationUpdatesOnlyLiveID() {
        let store = ActiveSessionStore()
        store.open(agentName: "Alfred", conversationID: nil)
        let launchKey = store.launchKey

        store.learnConversationID("conv-new")

        #expect(store.liveConversationID == "conv-new")
        #expect(store.launchConversationID == nil)
        #expect(store.launchKey == launchKey)
    }

    @Test func learningSameConversationIsIdempotent() {
        let store = ActiveSessionStore()
        store.startNew(agentName: "Alfred")
        let launchKey = store.launchKey

        store.learnConversationID("conv-new")
        store.learnConversationID("conv-new")

        #expect(store.liveConversationID == "conv-new")
        #expect(store.launchKey == launchKey)
    }

    @Test func launchKeyChangesWhenLaunchingAnotherConversation() {
        let store = ActiveSessionStore()
        store.open(agentName: "Alfred", conversationID: "conv-1")
        let first = store.launchKey
        store.open(agentName: "Alfred", conversationID: "conv-2")

        #expect(store.launchKey != first)
    }
}

// MARK: - Controller conversation routing

@MainActor
struct MemorySessionControllerConversationTests {
    @Test func conversationEventUpdatesLiveConversationID() {
        let controller = MemorySessionController(config: MemoryConfig())
        var learned: [String] = []
        controller.onConversationLearned = { learned.append($0) }

        controller.handleChatEvent(.conversation(conversationId: "conv-1"))

        #expect(controller.liveConversationID == "conv-1")
        #expect(learned == ["conv-1"])
    }

    @Test func repeatedConversationEventDoesNotReNotify() {
        let controller = MemorySessionController(config: MemoryConfig())
        var learned: [String] = []
        controller.onConversationLearned = { learned.append($0) }

        controller.handleChatEvent(.conversation(conversationId: "conv-1"))
        controller.handleChatEvent(.conversation(conversationId: "conv-1"))

        #expect(learned == ["conv-1"])
    }
}

// MARK: - Transcript store

@MainActor
struct ChatSessionTranscriptStoreTests {
    private func detail() -> SessionDetail {
        let records = [
            SessionRecord.fixture(kind: "turn", role: "user", text: "hello"),
            SessionRecord.fixture(kind: "turn", role: "user", text: "again"),
            SessionRecord.fixture(kind: "turn", role: "assistant", text: "hi there"),
            SessionRecord.fixture(
                kind: "tools_executed",
                toolName: "search",
                arguments: #"{"query":"x"}"#,
                output: "ok"
            ),
        ]
        let approvals = [
            SessionPendingApproval(
                questionID: "q1",
                tool: "search",
                arguments: #"{"query":"x"}"#
            ),
        ]
        return SessionDetail(
            room: "conv-1",
            agentDefinitionID: "agent-a",
            title: "Greeting",
            pendingApprovals: approvals,
            records: records
        )
    }

    @Test func loadHistoryMergesTurnsAndKeepsTools() {
        let store = ChatSessionTranscriptStore()
        store.loadHistory(from: detail())

        #expect(store.history.count == 3)
        guard case let .message(user) = store.history[0] else {
            Issue.record("Expected a user message")
            return
        }
        #expect(user.role == .user)
        #expect(user.text == "hello\nagain")
        guard case let .message(assistant) = store.history[1] else {
            Issue.record("Expected an assistant message")
            return
        }
        #expect(assistant.role == .assistant)
        #expect(assistant.text == "hi there")
        guard case let .tools(tools) = store.history[2] else {
            Issue.record("Expected a tools entry")
            return
        }
        #expect(tools.count == 1)
        #expect(tools[0].name == "search")
    }

    @Test func loadHistoryMapsPendingApprovals() {
        let store = ChatSessionTranscriptStore()
        store.loadHistory(from: detail())

        #expect(store.pendingApprovals.count == 1)
        #expect(store.pendingApprovals[0].questionId == "q1")
        #expect(store.pendingApprovals[0].tool == "search")
        #expect(store.pendingApprovals[0].arguments.contains("\"query\":\"x\""))
        #expect(store.historyError == nil)
    }

    @Test func markApprovalAnsweredRemovesIt() {
        let store = ChatSessionTranscriptStore()
        store.loadHistory(from: detail())

        store.markApprovalAnswered(questionId: "q1")

        #expect(store.pendingApprovals.isEmpty)
    }

    @Test func syncLiveMapsRolesAndReplacesPartialsByID() {
        let store = ChatSessionTranscriptStore()
        store.syncLive([
            LiveTranscriptMessage(id: "m1", role: .user, text: "hel"),
            LiveTranscriptMessage(id: "m1", role: .user, text: "hello"),
            LiveTranscriptMessage(id: "m2", role: .assistant, text: "hi"),
        ])

        #expect(store.liveMessages.count == 2)
        #expect(store.liveMessages[0] == ChatMessage(id: "m1", role: .user, text: "hello"))
        #expect(store.liveMessages[1] == ChatMessage(id: "m2", role: .assistant, text: "hi"))
    }

    @Test func syncLiveDropsMessagesAlreadyInHistory() {
        let store = ChatSessionTranscriptStore()
        store.loadHistory(from: detail())

        store.syncLive([
            LiveTranscriptMessage(id: "m1", role: .user, text: "hello\nagain"),
            LiveTranscriptMessage(id: "m2", role: .assistant, text: "hi there"),
            LiveTranscriptMessage(id: "m3", role: .user, text: "new question"),
        ])

        #expect(store.liveMessages.count == 1)
        #expect(store.liveMessages[0].text == "new question")
        #expect(store.liveMessages[0].id == "m3")
    }

    @Test func syncLiveSkipsEmptyTranscripts() {
        let store = ChatSessionTranscriptStore()
        store.syncLive([
            LiveTranscriptMessage(id: "m1", role: .assistant, text: ""),
        ])

        #expect(store.liveMessages.isEmpty)
    }

    @Test func resetClearsEverything() {
        let store = ChatSessionTranscriptStore()
        store.loadHistory(from: detail())
        store.syncLive([LiveTranscriptMessage(id: "m1", role: .user, text: "live")])

        store.reset()

        #expect(store.history.isEmpty)
        #expect(store.liveMessages.isEmpty)
        #expect(store.pendingApprovals.isEmpty)
    }
}

// MARK: - Session date formatting

struct SessionDateFormattingTests {
    @Test func formatsRFC3339SessionTimestamp() throws {
        let formatted = try #require(DateFormatting.formatSessionDate("2026-09-01T10:00:00Z"))
        #expect(formatted != "2026-09-01T10:00:00Z")
        #expect(formatted.contains("2026"))
    }

    @Test func formatsFractionalRFC3339SessionTimestamp() throws {
        let formatted = try #require(DateFormatting.formatSessionDate("2026-09-01T10:00:00.123Z"))
        #expect(formatted != "2026-09-01T10:00:00.123Z")
        #expect(formatted.contains("2026"))
    }

    @Test func stillFormatsLegacyClockString() throws {
        let formatted = try #require(DateFormatting.formatSessionDate("10:00:00.000"))
        #expect(formatted != "10:00:00.000")
    }

    @Test func unparseableValueIsReturnedVerbatim() {
        #expect(DateFormatting.formatSessionDate("not a date") == "not a date")
        #expect(DateFormatting.formatSessionDate(nil) == nil)
    }
}

// MARK: - Fixtures

private extension SessionRecord {
    static func fixture(
        kind: String,
        role: String? = nil,
        text: String? = nil,
        toolName: String? = nil,
        arguments: String? = nil,
        output: String? = nil
    ) -> SessionRecord {
        if let toolName {
            let call = ToolCall(name: toolName, arguments: arguments ?? "{}")
            return SessionRecord(
                kind: kind,
                calls: [call],
                outputs: [output ?? ""],
                isError: false
            )
        }
        return SessionRecord(kind: kind, role: role, text: text)
    }
}
