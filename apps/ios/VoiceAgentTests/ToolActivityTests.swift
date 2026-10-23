import Foundation
@testable import Memory
import Testing

// MARK: - Category vocabulary

struct MemoryToolCategoryTests {
    @Test func mapsKnownFamiliesByPrefix() {
        #expect(MemoryToolCategory(toolName: "entity-search") == .memory)
        #expect(MemoryToolCategory(toolName: "graph-traverse") == .memory)
        #expect(MemoryToolCategory(toolName: "journal-list") == .memory)
        #expect(MemoryToolCategory(toolName: "search-hybrid") == .search)
        #expect(MemoryToolCategory(toolName: "schema-migrate-preview") == .schemas)
        #expect(MemoryToolCategory(toolName: "document-get") == .documents)
        #expect(MemoryToolCategory(toolName: "web-search-brave") == .web)
        #expect(MemoryToolCategory(toolName: "agent-question-list") == .agents)
        #expect(MemoryToolCategory(toolName: "skill-list") == .skills)
        #expect(MemoryToolCategory(toolName: "blueprint-apply") == .blueprints)
        #expect(MemoryToolCategory(toolName: "project-list") == .projects)
        #expect(MemoryToolCategory(toolName: "session-todo-list") == .session)
        #expect(MemoryToolCategory(toolName: "mcp-brave-search") == .connector)
        #expect(MemoryToolCategory(toolName: "token-create") == .connector)
    }

    @Test func mapsUnprefixedToolsByName() {
        #expect(MemoryToolCategory(toolName: "remember") == .memory)
        #expect(MemoryToolCategory(toolName: "forget") == .memory)
        #expect(MemoryToolCategory(toolName: "lookup") == .search)
        #expect(MemoryToolCategory(toolName: "call_llm") == .agents)
        #expect(MemoryToolCategory(toolName: "list-installed-schemas") == .schemas)
        #expect(MemoryToolCategory(toolName: "classify-document") == .schemas)
        #expect(MemoryToolCategory(toolName: "finalize-discovery") == .schemas)
    }

    @Test func specificFamilyWinsOverTheFamilyItSitsInside() {
        // `project-record-` is a memory tool, `project-` a project tool.
        #expect(MemoryToolCategory(toolName: "project-record-get") == .memory)
        #expect(MemoryToolCategory(toolName: "project-get") == .projects)
    }

    @Test func matchesCaseInsensitivelyAndTrimsWhitespace() {
        #expect(MemoryToolCategory(toolName: "  Entity-Search \n") == .memory)
        #expect(MemoryToolCategory(toolName: "WEB-SEARCH-BRAVE") == .web)
    }

    @Test func unknownToolFallsBackToTheGenericCategory() {
        #expect(MemoryToolCategory(toolName: "some-future-tool") == .tool)
        #expect(MemoryToolCategory(toolName: "") == .tool)
        // A recorded row with no call name is labelled "Tool 1" by the session
        // mapper; it must still render.
        #expect(MemoryToolCategory(toolName: "Tool 1") == .tool)
    }

    @Test func everyCategoryHasASymbolAndDistinctCopyKeys() {
        var runningKeys = Set<String>()
        var doneKeys = Set<String>()
        for category in MemoryToolCategory.allCases {
            #expect(!category.symbol.isEmpty)
            #expect(category.runningKey.hasPrefix("chat.tool."))
            #expect(category.runningKey != category.doneKey)
            #expect(runningKeys.insert(category.runningKey).inserted)
            #expect(doneKeys.insert(category.doneKey).inserted)
        }
    }
}

// MARK: - Subject extraction

struct ToolActivitySubjectTests {
    @Test func readsThePreferredKey() {
        #expect(ToolActivitySubject.line(from: #"{"query":"dinner downtown"}"#) == "dinner downtown")
        #expect(ToolActivitySubject.line(from: #"{"q":"dinner"}"#) == "dinner")
        #expect(ToolActivitySubject.line(from: #"{"path":"/tmp/a.txt"}"#) == "/tmp/a.txt")
    }

    @Test func prefersASpecificIdentifierOverADisplayName() {
        let arguments = #"{"name":"Diane","entity_id":"ent-42"}"#
        #expect(ToolActivitySubject.line(from: arguments) == "ent-42")
    }

    @Test func skipsBlankValues() {
        let arguments = #"{"query":"   ","q":"real query"}"#
        #expect(ToolActivitySubject.line(from: arguments) == "real query")
    }

    @Test func fallsBackToAnyStringValue() {
        let arguments = #"{"unexpected_field":"visible"}"#
        #expect(ToolActivitySubject.line(from: arguments) == "visible")
    }

    @Test func collapsesNonJSONArgumentsToOneLine() {
        #expect(ToolActivitySubject.line(from: "first line\nsecond line") == "first line second line")
    }

    @Test func clampsLongSubjects() {
        let long = String(repeating: "x", count: 500)
        let line = ToolActivitySubject.line(from: long)
        #expect(line?.count == ToolActivitySubject.lineLimit)
    }

    @Test func doesNotParseOversizedArguments() throws {
        let oversized = "{\"query\":\"" + String(repeating: "x", count: ToolActivitySubject.parseLimit + 1) + "\"}"
        let unwrapped = try #require(ToolActivitySubject.line(from: oversized))
        // Shown raw: the parse cap means the value never reaches a one-line
        // subject, so the fallback's leading brace is still there.
        #expect(unwrapped.hasPrefix("{\"query\""))
        #expect(unwrapped.count <= ToolActivitySubject.lineLimit)
    }

    @Test func returnsNilWhenThereIsNothingToSay() {
        #expect(ToolActivitySubject.line(from: nil) == nil)
        #expect(ToolActivitySubject.line(from: "") == nil)
        #expect(ToolActivitySubject.line(from: "   \n ") == nil)
        #expect(ToolActivitySubject.line(from: "{}") == nil)
    }
}

// MARK: - Timing

struct ToolActivityTimingTests {
    private let englishLocale = Locale(identifier: "en_US")

    @Test func formatsSecondsAndMinutes() {
        #expect(ToolActivityTiming.text(2, locale: englishLocale) == "2s")
        #expect(ToolActivityTiming.text(65, locale: englishLocale) == "1m 5s")
        #expect(ToolActivityTiming.text(3600, locale: englishLocale) == "1h")
    }

    @Test func roundsToWholeSeconds() {
        #expect(ToolActivityTiming.text(1.4, locale: englishLocale) == "1s")
        #expect(ToolActivityTiming.text(2.6, locale: englishLocale) == "3s")
    }

    @Test func showsNothingUnderASecond() {
        #expect(ToolActivityTiming.text(0.4, locale: englishLocale) == nil)
        #expect(ToolActivityTiming.text(0.999, locale: englishLocale) == nil)
        #expect(ToolActivityTiming.text(0, locale: englishLocale) == nil)
        #expect(ToolActivityTiming.text(nil, locale: englishLocale) == nil)
    }

    @Test func ignoresImpossibleValues() {
        #expect(ToolActivityTiming.text(-3, locale: englishLocale) == nil)
        #expect(ToolActivityTiming.text(.infinity, locale: englishLocale) == nil)
        #expect(ToolActivityTiming.text(.nan, locale: englishLocale) == nil)
    }

    @Test func speaksTheDurationInWords() {
        #expect(ToolActivityTiming.spoken(2, locale: englishLocale) == "2 seconds")
        #expect(ToolActivityTiming.spoken(65, locale: englishLocale) == "1 minute, 5 seconds")
        #expect(ToolActivityTiming.spoken(0.4, locale: englishLocale) == nil)
    }
}

// MARK: - Store timing

struct ToolActivityStoreTimingTests {
    @MainActor
    @Test func stampsStartAndSettlesDuration() throws {
        var clock = Date(timeIntervalSince1970: 1000)
        let store = ChatActivityStore(now: { clock })
        store.userTurnStarted(anchorMessageID: "m1")
        store.apply(.toolCall(ChatToolCallEvent(id: "t1", tool: "entity-search", arguments: #"{"query":"x"}"#)))

        let running = try #require(store.liveTurn?.tools.first)
        #expect(running.startedAt == Date(timeIntervalSince1970: 1000))
        #expect(running.duration == nil)

        clock = Date(timeIntervalSince1970: 1004.5)
        store.apply(.toolResult(ChatToolResultEvent(id: "t1", tool: "entity-search", result: "[]", isError: false)))

        let settled = try #require(store.liveTurn?.tools.first)
        #expect(settled.duration == 4.5)
        // 4.5 s formats at the row's one-second resolution.
        #expect(ToolActivityTiming.text(settled.duration, locale: Locale(identifier: "en_US")) == "5s")
        #expect(settled.isRunning == false)
    }

    @MainActor
    @Test func clampsANegativeDuration() throws {
        var clock = Date(timeIntervalSince1970: 1000)
        let store = ChatActivityStore(now: { clock })
        store.userTurnStarted(anchorMessageID: "m1")
        store.apply(.toolCall(ChatToolCallEvent(id: "t1", tool: "entity-search", arguments: "{}")))

        // A clock that steps backwards must not produce a negative duration.
        clock = Date(timeIntervalSince1970: 900)
        store.apply(.toolResult(ChatToolResultEvent(id: "t1", tool: "entity-search", result: "[]", isError: false)))

        let settled = try #require(store.liveTurn?.tools.first)
        #expect(settled.duration == 0)
    }

    @MainActor
    @Test func leavesACallWithoutAResultUntimed() throws {
        let store = ChatActivityStore()
        store.userTurnStarted(anchorMessageID: "m1")
        store.apply(.toolCall(ChatToolCallEvent(id: "t1", tool: "document-get", arguments: "{}")))

        let call = try #require(store.liveTurn?.tools.first)
        #expect(call.duration == nil)
        #expect(call.durationText == nil)
    }

    @MainActor
    @Test func categorizesTheRowFromItsToolName() throws {
        let store = ChatActivityStore()
        store.userTurnStarted(anchorMessageID: "m1")
        store.apply(.toolCall(ChatToolCallEvent(id: "t1", tool: "web-search-brave", arguments: #"{"query":"swift"}"#)))

        let call = try #require(store.liveTurn?.tools.first)
        #expect(call.category == MemoryToolCategory.web)
        #expect(call.subject == "swift")
    }
}
