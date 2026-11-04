import Foundation
@testable import Memory
import SwiftUI
import Testing

// MARK: - Parsing

struct ToolResultPresentationTests {
    private func body(_ payload: String) -> ToolResultPresentation.Body? {
        ToolResultPresentation.parse(payload)?.body
    }

    @Test func readsAnEnvelopeWrappedSearchAsRecords() throws {
        let body = try #require(body(Payload.search))
        guard case let .items(items, more) = body else {
            Issue.record("expected items, got \(body)")
            return
        }
        #expect(items.count == 2)
        #expect(more == 0)
        // The human name wins over the slug in `key`, and the description inside
        // `object.properties` wins over the type.
        #expect(items[0].title == "Alice")
        #expect(items[0].detail == "Engineer")
        // A hit with no name falls back to its key.
        #expect(items[1].title == "retrieval")
        #expect(items[1].detail == "How answers are found")
    }

    @Test func readsAnEntityQueryAsRecords() throws {
        let body = try #require(body(Payload.entities))
        guard case let .items(items, _) = body else {
            Issue.record("expected items, got \(body)")
            return
        }
        #expect(items.first?.title == "Alice")
        #expect(items.first?.detail == "Engineer")
    }

    @Test func readsAnUpdatedRecordAsFields() throws {
        // `entity-update` answers with `data: {entity: {id, type}}`: the record
        // inside is the thing to read, unwrapped from its envelope.
        let body = try #require(body(Payload.entityUpdate))
        guard case let .fields(fields, _) = body else {
            Issue.record("expected fields, got \(body)")
            return
        }
        #expect(fields.map(\.label) == ["id", "type"])
        #expect(fields.map(\.value) == ["e1", "Note"])
    }

    @Test func readsACreatedBatchAsRecords() throws {
        // `entity-create` answers with `data: {results: [{entity: …}], message}`,
        // and each result names its entity by id — a record with no name is still
        // a record, not a reason to show the envelope.
        let body = try #require(body(Payload.entityCreate))
        guard case let .items(items, more) = body else {
            Issue.record("expected items, got \(body)")
            return
        }
        #expect(items.map(\.title) == ["e1"])
        #expect(items.first?.detail == "Note")
        #expect(more == 0)
    }

    @Test func readsAnEmptySearchAsNoResults() throws {
        // `{"data": {"data": [], "total": 0, "has_more": false}}` means
        // "nothing matched", not "here is the envelope" (#2277).
        let presentation = try #require(ToolResultPresentation.parse(Payload.emptySearch))
        #expect(presentation.body == .empty)
        // The envelope is still the evidence behind the disclosure.
        #expect(presentation.raw.contains("has_more"))
    }

    @Test func readsAnUnwrappedEmptyListAsNoResults() {
        #expect(body(Payload.emptyList) == .empty)
    }

    @Test func readsABatchCreateWithItsCounts() throws {
        let presentation = try #require(ToolResultPresentation.parse(Payload.entityCreate))
        let tally = try #require(presentation.batch)
        #expect(tally == ToolResultPresentation.BatchTally(created: 1, failed: 0, similar: 0))
        #expect(!tally.isFailure)
        #expect(tally.text(locale: Locale(identifier: "en_US")) == "1 created, 0 failed")
    }

    @Test func readsABatchWithAFailedItemAsAFailure() throws {
        // The real envelope for a half-failed batch is `ok: false` plus the
        // error summary, so the body is that message; the tally is what keeps
        // the created/failed counts visible (#2277).
        let presentation = try #require(ToolResultPresentation.parse(Payload.entityCreatePartial))
        let tally = try #require(presentation.batch)
        #expect(tally.created == 1)
        #expect(tally.failed == 1)
        #expect(tally.isFailure)
        #expect(tally.text(locale: Locale(identifier: "en_US")) == "1 created, 1 failed")

        guard case let .text(text, _, _) = try #require(presentation.body) else {
            Issue.record("expected the envelope's error text")
            return
        }
        #expect(text == "1 of 2 entities failed to create")
    }

    @Test func readsNearDuplicateCountsFromMeta() throws {
        // `similar_count` is not derivable from the records, so this pins that
        // the tally comes from `meta`, not from counting result items (#2277).
        let presentation = try #require(ToolResultPresentation.parse(Payload.entityCreateSimilar))
        let tally = try #require(presentation.batch)
        #expect(tally.created == 2)
        #expect(tally.failed == 0)
        #expect(tally.similar == 3)
        #expect(!tally.isFailure)
        #expect(
            tally.text(locale: Locale(identifier: "en_US"))
                == "2 created, 0 failed, 3 near-duplicates"
        )
        #expect(
            ToolResultPresentation.BatchTally(created: 1, failed: 0, similar: 1)
                .text(locale: Locale(identifier: "en_US"))
                == "1 created, 0 failed, 1 near-duplicate"
        )
    }

    @Test func countsABatchFromItsItemsWhenMetaIsMissing() throws {
        let payload = #"{"ok":true,"data":{"results":[{"ok":true,"index":0,"entity":{"id":"e1","type":"Note"}},{"ok":false,"index":1}]}}"#
        let presentation = try #require(ToolResultPresentation.parse(payload))
        let tally = try #require(presentation.batch)
        #expect(tally.created == 1)
        #expect(tally.failed == 1)
    }

    @Test func aSearchIsNotABatch() {
        // A `results` array alone is not a batch: web search has one too.
        #expect(ToolResultPresentation.parse(Payload.search)?.batch == nil)
        #expect(ToolResultPresentation.parse(Payload.web)?.batch == nil)
    }

    @Test func readsAFailureThatAlsoCarriesAPayloadAsItsMessage() throws {
        // The error is the answer even when a partial payload sits beside it;
        // checking the envelope only after unwrapping lost the message.
        let payload = #"{"ok":false,"error":"quota exceeded","data":{"results":[],"message":"0 created"}}"#
        let body = try #require(body(payload))
        guard case let .text(text, _, _) = body else {
            Issue.record("expected text, got \(body)")
            return
        }
        #expect(text == "quota exceeded")
    }

    @Test func readsADocumentAsText() throws {
        let body = try #require(body(Payload.document))
        guard case let .text(text, moreLines, moreCharacters) = body else {
            Issue.record("expected text, got \(body)")
            return
        }
        #expect(text.hasPrefix("A line of the retrieved document."))
        #expect(text.components(separatedBy: "\n").count == ToolResultPresentation.textLineLimit)
        // The other twelve lines are summarized, not dropped silently.
        #expect(moreLines == 20 - ToolResultPresentation.textLineLimit)
        #expect(moreCharacters == 0)
    }

    @Test func readsADocumentListByFilename() throws {
        let body = try #require(body(Payload.documentList))
        guard case let .items(items, _) = body else {
            Issue.record("expected items, got \(body)")
            return
        }
        #expect(items.map(\.title) == ["notes.md", "spec.pdf"])
        #expect(items.first?.detail == "text/markdown")
    }

    @Test func readsWebResultsWithTheirLinks() throws {
        let body = try #require(body(Payload.web))
        guard case let .items(items, _) = body else {
            Issue.record("expected items, got \(body)")
            return
        }
        #expect(items.first?.title == "SwiftUI")
        #expect(items.first?.url == "https://developer.apple.com/documentation/swiftui")
        // The link is shown once, as the link line — not repeated as the detail.
        #expect(items.first?.detail == "Declarative UI")
    }

    @Test func readsARecordOfScalarsAsFields() throws {
        let body = try #require(body(Payload.record))
        guard case let .fields(fields, _) = body else {
            Issue.record("expected fields, got \(body)")
            return
        }
        // JSON has no order: fields are sorted so the list never reshuffles.
        #expect(fields.map(\.label) == ["count", "error", "ok", "paused", "status"])
        // A null is a value the record has, not a reason to show raw JSON.
        #expect(fields.map(\.value) == ["3", "—", "true", "false", "ok"])
    }

    @Test func readsAFailedEnvelopeAsItsMessage() throws {
        let body = try #require(body(Payload.failure))
        guard case let .text(text, _, _) = body else {
            Issue.record("expected text, got \(body)")
            return
        }
        #expect(text == "entity not found: e9")
    }

    @Test func readsAListOfStringsAsRecords() throws {
        let body = try #require(body(Payload.stringList))
        guard case let .items(items, _) = body else {
            Issue.record("expected items, got \(body)")
            return
        }
        #expect(items.map(\.title) == ["alpha", "beta"])
    }

    @Test func anUntitledRecordIsCountedNotShown() throws {
        let body = try #require(body(Payload.untitled))
        guard case let .items(items, more) = body else {
            Issue.record("expected items, got \(body)")
            return
        }
        #expect(items.map(\.title) == ["Only one"])
        #expect(more == 1)
    }

    @Test func aLongListIsCappedAndCounted() throws {
        let body = try #require(body(Payload.hits(7)))
        guard case let .items(items, more) = body else {
            Issue.record("expected items, got \(body)")
            return
        }
        #expect(items.count == ToolResultPresentation.itemLimit)
        #expect(more == 7 - ToolResultPresentation.itemLimit)
    }

    @Test func aRecordOfNestedObjectsIsRawOnly() {
        #expect(body(Payload.nested) == nil)
    }

    @Test func plainTextIsRawOnly() throws {
        let presentation = try #require(ToolResultPresentation.parse(Payload.plainText))
        #expect(presentation.body == nil)
        #expect(presentation.raw == Payload.plainText)
        #expect(presentation.rawMoreLines == 0)
    }

    @Test func oneEndlessLineIsShortenedAndCounted() throws {
        let presentation = try #require(ToolResultPresentation.parse(Payload.longLine))
        guard case let .text(text, moreLines, moreCharacters) = try #require(presentation.body) else {
            Issue.record("expected text")
            return
        }
        // One line, so nothing was dropped by the line cap — but most of the
        // line was, and the body says so instead of pretending it is complete.
        #expect(moreLines == 0)
        #expect(moreCharacters == 50000 - ToolResultPresentation.lineWidth)
        #expect(text.hasSuffix("…"))
        // The raw block is bounded the same way, and says the same thing.
        #expect(presentation.rawMoreCharacters > 0)
        let longest = presentation.raw.components(separatedBy: "\n").map(\.count).max() ?? 0
        #expect(longest <= ToolResultPresentation.lineWidth + 1)
    }

    @Test func aLargePayloadIsCappedNotRefused() throws {
        // A 400-record list pretty-prints to far more than the raw cap: the body
        // still parses, the reveal is bounded and its remainder counted.
        let presentation = try #require(ToolResultPresentation.parse(Payload.hits(400)))
        #expect(presentation.raw.components(separatedBy: "\n").count == ToolResultPresentation.rawLineLimit)
        #expect(presentation.rawMoreLines > 0)
    }

    @Test func anOversizedPayloadIsBoundedAndCounted() throws {
        // A result past the parse limit is a wall no presentation fixes — but it
        // is still bounded, and still says what it left out.
        let huge = String(repeating: "x", count: ToolResultPresentation.parseLimit + 1)
        let presentation = try #require(ToolResultPresentation.parse(huge))
        #expect(presentation.body == nil)
        #expect(presentation.raw.count == ToolResultPresentation.lineWidth + 1)
        #expect(presentation.rawMoreCharacters == huge.count - ToolResultPresentation.lineWidth)
    }

    @Test func nothingToShowForAnEmptyResult() {
        #expect(ToolResultPresentation.parse(nil) == nil)
        #expect(ToolResultPresentation.parse("") == nil)
        #expect(ToolResultPresentation.parse("  \n ") == nil)
    }
}
