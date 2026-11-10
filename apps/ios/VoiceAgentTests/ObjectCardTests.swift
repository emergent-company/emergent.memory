import Foundation
@testable import Memory
import Testing

/// The object card's wire model, pinned to what the gateway's
/// `GET /api/objects/:id/card` and `/history` return (`object_card.go`, and its
/// `object_card_test.go` fixtures).
struct ObjectCardDecodingTests {
    static let cardJSON = #"""
    {
      "id": "11111111-1111-1111-1111-111111111111",
      "copy_id": "22222222-2222-2222-2222-222222222222",
      "title": "sam",
      "key": "sam",
      "version": 3,
      "type": {"name": "person", "label": "Person", "icon": "user", "color": "#3b82f6"},
      "status": {"value": "active", "label": "Active", "tone": "neutral"},
      "referenced_via": "works at",
      "sections": [
        {"title": "Properties", "fields": [
          {"label": "Key", "value": "sam"},
          {"label": "Version", "value": "v3"},
          {"label": "Created", "value": "", "empty": true},
          {"label": "ID", "value": "22222222-2222-2222-2222-222222222222", "mono": true}
        ]},
        {"title": "Person Properties", "fields": [
          {"label": "First name", "name": "first_name", "description": "Given name.", "value": "Sam"},
          {"label": "Nickname", "name": "nickname", "value": "", "empty": true}
        ]}
      ],
      "labels": ["colleague"]
    }
    """#

    @Test func cardDecodesEveryDrawerPiece() throws {
        let card = try JSONDecoder().decode(ObjectCard.self, from: Data(Self.cardJSON.utf8))
        #expect(card.title == "sam")
        #expect(card.copyID == "22222222-2222-2222-2222-222222222222")
        #expect(card.version == 3)
        #expect(card.type == ObjectCardType(name: "person", label: "Person", icon: "user", color: "#3b82f6"))
        #expect(card.status == ObjectCardStatus(value: "active", label: "Active", tone: "neutral"))
        #expect(card.workflow == nil)
        #expect(card.referencedVia == "works at")
        #expect(card.labels == ["colleague"])
        #expect(card.sections.map(\.title) == ["Properties", "Person Properties"])

        let builtins = card.sections[0].fields
        #expect(builtins.map(\.label) == ["Key", "Version", "Created", "ID"])
        #expect(builtins[2].isEmpty)
        #expect(builtins[3].isMonospaced)

        let typed = card.sections[1].fields
        #expect(typed[0] == ObjectCardField(label: "First name", name: "first_name", description: "Given name.", value: "Sam"))
        #expect(typed[1].isEmpty)
        // Rows are identified by property name, so two rows never collide.
        #expect(Set(typed.map(\.id)).count == typed.count)
    }

    @Test func workflowTypeCarriesItsStrip() throws {
        let json = #"""
        {"id":"o1","title":"Ship it","type":{"name":"task"},
         "workflow":{"status":{"value":"in_progress","label":"In progress","tone":"info"},"assignee":"diane"},
         "sections":[],"labels":[]}
        """#
        let card = try JSONDecoder().decode(ObjectCard.self, from: Data(json.utf8))
        #expect(card.status == nil)
        #expect(card.workflow == ObjectCardWorkflow(
            status: ObjectCardStatus(value: "in_progress", label: "In progress", tone: "info"),
            assignee: "diane"
        ))
        // A type without a declared label reads as its name.
        #expect(card.type.label == "task")
    }

    @Test func workflowPickersDecodeTheirOptions() throws {
        let json = #"""
        {"id":"o1","title":"Ship it","key":"TASK-1","type":{"name":"task"},
         "workflow":{
           "status":{"value":"review","label":"Review","tone":"warning"},
           "assignee":"diane",
           "allowed_statuses":[
             {"value":"ready","label":"Ready","tone":"neutral"},
             {"value":"review","label":"Review","tone":"warning"},
             {"value":"done","label":"Done","tone":"success"}
           ],
           "assignees":[{"value":"agent-a","label":"agent-a"},{"value":"Alice","label":"Alice"}]
         },
         "sections":[],"labels":[]}
        """#
        let card = try JSONDecoder().decode(ObjectCard.self, from: Data(json.utf8))
        guard let workflow = card.workflow else {
            Issue.record("workflow strip missing")
            return
        }
        #expect(workflow.allowedStatuses.map(\.value) == ["ready", "review", "done"])
        #expect(workflow.allowedStatuses[1].label == "Review")
        #expect(workflow.allowedStatuses[1].tone == "warning")
        #expect(workflow.assignees.map(\.value) == ["agent-a", "Alice"])
        #expect(card.canEditWorkflow)
    }

    @Test func keylessOrOptionlessWorkflowIsReadOnly() throws {
        // No key: the write routes have no work item to act on.
        let keyless = try JSONDecoder().decode(ObjectCard.self, from: Data(#"""
        {"id":"o1","title":"Note","type":{"name":"note"},
         "workflow":{"status":{"value":"ready","label":"Ready"},
                     "allowed_statuses":[{"value":"ready","label":"Ready"}]},
         "sections":[],"labels":[]}
        """#.utf8))
        #expect(!keyless.canEditWorkflow)

        // A key but no declared statuses: nothing to pick.
        let optionless = try JSONDecoder().decode(ObjectCard.self, from: Data(#"""
        {"id":"o1","title":"Task","key":"TASK-1","type":{"name":"task"},
         "workflow":{"status":{"value":"ready","label":"Ready"}},
         "sections":[],"labels":[]}
        """#.utf8))
        #expect(!optionless.canEditWorkflow)
    }

    @Test func sparseCardStillOpens() throws {
        let card = try JSONDecoder().decode(ObjectCard.self, from: Data(#"{"id":"o1"}"#.utf8))
        #expect(card.title == "o1")
        #expect(card.copyID == "o1")
        #expect(card.sections.isEmpty)
        #expect(card.labels.isEmpty)
        #expect(card.referencedVia == nil)
    }

    @Test func historyDecodesChangesAndToleratesUnknownKinds() throws {
        let json = #"""
        {"version":2,"label":"v2","status":{"value":"done","label":"Done","tone":"success"},
         "key":"sam","created_at":"2026-09-02T10:00:00Z",
         "changes":[{"kind":"update","label":"first_name","from":"S","to":"Sam"},
                    {"kind":"add","label":"nickname"},
                    {"kind":"future","label":"something new"}]}
        """#
        let version = try JSONDecoder().decode(ObjectCardVersion.self, from: Data(json.utf8))
        #expect(version.label == "v2")
        #expect(version.status?.tone == "success")
        #expect(DateFormatting.objectDate(version.createdAt) != nil)
        #expect(version.changes == [
            ObjectCardChange(kind: .update, label: "first_name", before: "S", after: "Sam"),
            ObjectCardChange(kind: .add, label: "nickname"),
            ObjectCardChange(kind: .note, label: "something new"),
        ])
    }
}

/// The card's type appearance: every icon the web lets a schema declare draws a
/// real symbol, and declared colors read in the shapes schemas use.
struct ObjectTypeAppearanceTests {
    /// The web's supported icon set (`supportedTypeIconClasses`,
    /// `apps/web-ui/gateway/type_icons.go`), without the `lucide--` prefix.
    static let webIcons = [
        "box", "file-text", "file", "git-branch", "git-commit", "git-merge", "git-pull-request",
        "shield", "shield-check", "zap", "layers", "tag", "star", "heart", "alert-triangle", "bell",
        "book", "book-open", "bot", "briefcase", "calendar", "camera", "check", "check-circle",
        "circle", "clock", "cloud", "code", "cog", "settings", "database", "edit", "eye", "folder",
        "globe", "hash", "home", "image", "info", "key", "link", "list", "lock", "mail", "map",
        "map-pin", "message-circle", "monitor", "package", "paperclip", "pen", "phone", "play",
        "plus", "puzzle", "search", "send", "server", "share", "sparkles", "terminal", "trash",
        "user", "users", "wrench", "x",
    ]

    @Test func everyWebIconHasASymbol() {
        let missing = Self.webIcons.filter { ObjectTypeAppearance.lucideSymbols[$0] == nil }
        #expect(missing.isEmpty, "unmapped web icons: \(missing)")
        #expect(ObjectTypeAppearance.symbol(forLucide: "user") == "person")
        #expect(ObjectTypeAppearance.symbol(forLucide: "not-an-icon") == ObjectTypeAppearance.fallbackSymbol)
    }

    @Test func declaredColorsParse() {
        #expect(ObjectTypeAppearance.color(css: "#3b82f6") != nil)
        #expect(ObjectTypeAppearance.color(css: "#abc") != nil)
        #expect(ObjectTypeAppearance.color(css: "#3b82f680") != nil)
        #expect(ObjectTypeAppearance.color(css: " Blue ") != nil)
        #expect(ObjectTypeAppearance.color(css: "") == nil)
        #expect(ObjectTypeAppearance.color(css: "#12") == nil)
        #expect(ObjectTypeAppearance.color(css: "#zzzzzz") == nil)
        #expect(ObjectTypeAppearance.color(css: "oklch(70% 0.1 200)") == nil)
    }
}

/// A recording card-fetching fake: serves one queued result per call.
@MainActor
final class FakeObjectCardClient: ObjectCardFetching {
    var cards: [Result<ObjectCard, Error>]
    var histories: [Result<[ObjectCardVersion], Error>]
    var statusUpdates: [Result<ObjectCard, Error>]
    var assigneeUpdates: [Result<ObjectCard, Error>]
    private(set) var cardCalls: [ObjectReference] = []
    private(set) var historyCalls = 0
    private(set) var statusCalls: [String] = []
    private(set) var assigneeCalls: [String] = []

    init(
        cards: [Result<ObjectCard, Error>],
        histories: [Result<[ObjectCardVersion], Error>] = [],
        statusUpdates: [Result<ObjectCard, Error>] = [],
        assigneeUpdates: [Result<ObjectCard, Error>] = []
    ) {
        self.cards = cards
        self.histories = histories
        self.statusUpdates = statusUpdates
        self.assigneeUpdates = assigneeUpdates
    }

    func card(for reference: ObjectReference) async throws -> ObjectCard {
        cardCalls.append(reference)
        return try (cards.count > 1 ? cards.removeFirst() : cards[0]).get()
    }

    func history(for _: ObjectReference) async throws -> [ObjectCardVersion] {
        historyCalls += 1
        guard !histories.isEmpty else { return [] }
        return try (histories.count > 1 ? histories.removeFirst() : histories[0]).get()
    }

    func setStatus(_ status: String, for _: ObjectReference) async throws -> ObjectCard {
        statusCalls.append(status)
        guard !statusUpdates.isEmpty else {
            throw GatewayError.httpStatus(500, "no queued status update")
        }
        return try (statusUpdates.count > 1 ? statusUpdates.removeFirst() : statusUpdates[0]).get()
    }

    func setAssignee(_ assignee: String, for _: ObjectReference) async throws -> ObjectCard {
        assigneeCalls.append(assignee)
        guard !assigneeUpdates.isEmpty else {
            throw GatewayError.httpStatus(500, "no queued assignee update")
        }
        return try (assigneeUpdates.count > 1 ? assigneeUpdates.removeFirst() : assigneeUpdates[0]).get()
    }
}

@MainActor
struct ObjectCardStoreTests {
    private let reference = ObjectReference(ref: "o1", relationshipID: "r1")
    private let card = ObjectCard(id: "o1", copyID: "o1", title: "sam", type: ObjectCardType(name: "person"))

    @Test func loadPublishesTheCardForTheReference() async {
        let fake = FakeObjectCardClient(cards: [.success(card)])
        let store = ObjectCardStore(reference: reference, client: fake)
        await store.load()
        #expect(store.loadState == .loaded)
        #expect(store.card == card)
        // The relationship context travels with the request.
        #expect(fake.cardCalls == [reference])
    }

    @Test func failureCanBeRetried() async {
        let fake = FakeObjectCardClient(cards: [.failure(GatewayError.httpStatus(404, "Object card returned HTTP 404")), .success(card)])
        let store = ObjectCardStore(reference: reference, client: fake)
        await store.load()
        #expect(store.loadState == .failed("Object card returned HTTP 404"))
        #expect(store.card == nil)
        await store.load()
        #expect(store.loadState == .loaded)
        #expect(store.card == card)
    }

    @Test func historyLoadsOnceAndItsFailureLeavesTheCard() async {
        let fake = FakeObjectCardClient(
            cards: [.success(card)],
            histories: [.failure(GatewayError.transport("offline")), .success([ObjectCardVersion(version: 1, label: "v1")])]
        )
        let store = ObjectCardStore(reference: reference, client: fake)
        await store.load()

        await store.loadHistoryIfNeeded()
        #expect(store.historyState == .failed("offline"))
        #expect(store.card == card)

        // A failed history may be retried; a loaded one is not refetched.
        await store.loadHistoryIfNeeded()
        #expect(store.history.map(\.version) == [1])
        await store.loadHistoryIfNeeded()
        #expect(fake.historyCalls == 2)
    }

    @Test func statusUpdateAdoptsTheRefreshedCard() async {
        let ready = workItemCard(status: ObjectCardStatus(value: "ready", label: "Ready"))
        let done = workItemCard(status: ObjectCardStatus(value: "done", label: "Done", tone: "success"))
        let fake = FakeObjectCardClient(cards: [.success(ready)], statusUpdates: [.success(done)])
        let store = ObjectCardStore(reference: reference, client: fake)
        await store.load()

        await store.setStatus("done")
        #expect(fake.statusCalls == ["done"])
        #expect(store.card == done)
        #expect(store.actionError == nil)
        #expect(store.isSaving == false)
    }

    @Test func failedStatusUpdateKeepsTheOldValueAndShowsTheError() async {
        let workItem = workItemCard(status: ObjectCardStatus(value: "ready", label: "Ready"))
        let fake = FakeObjectCardClient(
            cards: [.success(workItem)],
            statusUpdates: [.failure(GatewayError.httpStatus(400, "status is not in the type's allowed work-status set: shipped"))]
        )
        let store = ObjectCardStore(reference: reference, client: fake)
        await store.load()

        await store.setStatus("shipped")
        // The stored value is untouched: the drawer never guesses, so there is
        // nothing to revert.
        #expect(store.card == workItem)
        #expect(store.actionError == "status is not in the type's allowed work-status set: shipped")
        #expect(store.isSaving == false)
    }

    @Test func assigneeUpdateSendsTheExactValue() async {
        let workItem = workItemCard(status: ObjectCardStatus(value: "ready", label: "Ready"))
        let fake = FakeObjectCardClient(
            cards: [.success(workItem)],
            assigneeUpdates: [.success(workItem), .success(workItem)]
        )
        let store = ObjectCardStore(reference: reference, client: fake)
        await store.load()

        await store.setAssignee("agent-a")
        await store.setAssignee("")
        #expect(fake.assigneeCalls == ["agent-a", ""])
        #expect(store.actionError == nil)
    }

    /// A work-item card with the statuses and candidates the pickers need.
    private func workItemCard(status: ObjectCardStatus) -> ObjectCard {
        ObjectCard(
            id: "o1",
            copyID: "o1",
            title: "sam",
            key: "TASK-1",
            type: ObjectCardType(name: "task"),
            workflow: ObjectCardWorkflow(
                status: status,
                allowedStatuses: [
                    ObjectCardStatus(value: "ready", label: "Ready", tone: "neutral"),
                    ObjectCardStatus(value: "done", label: "Done", tone: "success"),
                ],
                assignees: [ObjectCardAssignee(value: "agent-a")]
            )
        )
    }
}

/// Recorded turns carry their citations into the transcript, and a merged
/// reply cites what each of its turns cited, once.
@MainActor
struct TranscriptCitationTests {
    @Test func historyRepliesCarryMergedCitations() {
        let sam = ChatCitation(kind: "object", id: "o1", label: "Sam", url: "/objects/o1")
        let acme = ChatCitation(kind: "object", id: "o2", label: "Acme", url: "/objects/o2")
        let transcript = ChatSessionTranscriptStore()
        transcript.loadHistory(from: SessionDetail(room: "c1", records: [
            SessionRecord(kind: "turn", role: "user", text: "Who is Sam?", citations: [sam]),
            SessionRecord(kind: "turn", role: "diane", text: "Sam is a colleague.", citations: [sam]),
            SessionRecord(kind: "turn", role: "diane", text: "Sam works at Acme.", citations: [sam, acme]),
        ]))

        let messages = transcript.history.compactMap { entry -> ChatMessage? in
            if case let .message(message) = entry {
                return message
            }
            return nil
        }
        #expect(messages.count == 2)
        // A user turn never shows a Sources footer.
        #expect(messages[0].citations.isEmpty)
        #expect(messages[1].citations == [sam, acme])
    }
}
