import Foundation
@testable import Memory
import Testing

/// Which chat links open the object card. The rules mirror the web chat's
/// `parseRef` (`object-preview.js`): an `/objects/<ref>` path, with an optional
/// `#relationship-<id>` fragment, and a single-segment ref.
struct ObjectReferenceTests {
    private let objectID = "11111111-1111-1111-1111-111111111111"

    private func parse(_ link: String) -> ObjectReference? {
        URL(string: link).flatMap(ObjectReference.init(url:))
    }

    @Test func relativeObjectLinkIsAReference() {
        // The cited form: [Name](/objects/<id>).
        #expect(parse("/objects/\(objectID)") == ObjectReference(ref: objectID))
    }

    @Test func relationshipFragmentIsCarried() {
        let reference = parse("/objects/\(objectID)#relationship-r-1")
        #expect(reference == ObjectReference(ref: objectID, relationshipID: "r-1"))
    }

    @Test func otherFragmentsAreIgnored() {
        #expect(parse("/objects/\(objectID)#history") == ObjectReference(ref: objectID))
        #expect(parse("/objects/\(objectID)#relationship-") == ObjectReference(ref: objectID))
    }

    @Test func scopedAndAbsoluteGatewayLinksAreReferences() {
        #expect(parse("/p/proj-1/objects/\(objectID)")?.ref == objectID)
        #expect(parse("https://memory.example.com/objects/\(objectID)")?.ref == objectID)
    }

    @Test func keyReferencesAreKept() {
        #expect(parse("/objects/sam")?.ref == "sam")
        #expect(parse("/objects/caf%C3%A9")?.ref == "café")
    }

    @Test func nonObjectLinksAreLeftToTheSystem() {
        #expect(parse("https://example.com/docs/page") == nil)
        #expect(parse("mailto:sam@example.com") == nil)
        #expect(parse("tel:+48123") == nil)
        #expect(parse("/objects/") == nil)
        // A key with a slash cannot be one route segment, so navigation wins,
        // as on the web.
        #expect(parse("/objects/lov/2005-06-17-90") == nil)
    }

    @Test func urlRoundTripsThroughTheParser() throws {
        let reference = ObjectReference(ref: objectID, relationshipID: "r1")
        let url = try #require(reference.url)
        #expect(url.absoluteString == "/objects/\(objectID)#relationship-r1")
        #expect(ObjectReference(url: url) == reference)
    }

    @Test func identityDistinguishesTheRelationship() {
        #expect(ObjectReference(ref: "a").id != ObjectReference(ref: "a", relationshipID: "r").id)
    }
}

/// The citation model behind the Sources footer.
struct ChatCitationTests {
    @Test func listDecodesLenientlyAndSkipsOddElements() throws {
        let json = #"""
        [{"kind":"object","id":"o1","type":"person","label":"Sam","url":"/objects/o1"},
         null, "stray", 7,
         {"kind":"relationship","id":"r1","type":"works_at","label":"Sam —works at→ Acme","url":"/objects/o1#relationship-r1"}]
        """#
        let list = try JSONDecoder().decode(LenientCitations.self, from: Data(json.utf8)).items
        let named = list.filter { !$0.displayName.isEmpty }
        #expect(named.map(\.label) == ["Sam", "Sam —works at→ Acme"])
    }

    @Test func notAnArrayIsEmpty() throws {
        let list = try JSONDecoder().decode(LenientCitations.self, from: Data(#"{"a":1}"#.utf8)).items
        #expect(list.isEmpty)
    }

    @Test func displayNameFallsBackFromLabelToKeyToID() {
        #expect(ChatCitation(id: "o1", label: "Sam", key: "sam").displayName == "Sam")
        #expect(ChatCitation(id: "o1", label: " ", key: "sam").displayName == "sam")
        #expect(ChatCitation(id: "o1").displayName == "o1")
        #expect(ChatCitation().displayName.isEmpty)
    }

    @Test func objectCitationOpensItsObject() {
        #expect(ChatCitation(kind: "object", id: "o1", url: "/objects/o1").objectReference == ObjectReference(ref: "o1"))
        // No URL: the object's own id.
        #expect(ChatCitation(kind: "object", id: "o1").objectReference == ObjectReference(ref: "o1"))
    }

    @Test func relationshipCitationOpensItsSourceObject() {
        let citation = ChatCitation(kind: "relationship", id: "r1", url: "/objects/o1#relationship-r1")
        #expect(citation.objectReference == ObjectReference(ref: "o1", relationshipID: "r1"))
        // A relationship id is never opened as if it were an object.
        #expect(ChatCitation(kind: "relationship", id: "r1").objectReference == nil)
    }

    @Test func deduplicationKeepsFirstSeenOrder() {
        let sam = ChatCitation(kind: "object", id: "o1", label: "Sam", url: "/objects/o1")
        let acme = ChatCitation(kind: "object", id: "o2", label: "Acme")
        let unnamed = ChatCitation()
        #expect(ChatCitation.deduplicated([sam, unnamed, acme, sam]) == [sam, acme])
    }

    @Test func streamFrameCarriesTheCitations() {
        let frame = #"{"type":"citations","citations":[{"kind":"object","id":"o1","type":"person","label":"Sam","url":"/objects/o1"}]}"#
        let event = ChatStreamFrameDecoder.decode(frame, fallbackToolID: { "tool" })
        #expect(event == .citations([
            ChatCitation(kind: "object", id: "o1", type: "person", label: "Sam", url: "/objects/o1"),
        ]))
    }

    @Test func oddCitationsNeverDropTheFrameCarryingThem() {
        // `meta` carries a citations key too: a malformed one must not cost the
        // conversation id.
        let meta = #"{"type":"meta","conversationId":"c1","citations":[null,{"x":1}]}"#
        let event = ChatStreamFrameDecoder.decode(meta, fallbackToolID: { "tool" })
        #expect(event == .meta(conversationID: "c1", runID: nil))
    }

    @Test func recordedTurnsCarryAndMergeTheirCitations() throws {
        let json = #"""
        {"kind":"turn","role":"diane","text":"Sam works at Acme.",
         "citations":[{"kind":"object","id":"o1","label":"Sam","url":"/objects/o1"}]}
        """#
        let record = try JSONDecoder().decode(SessionRecord.self, from: Data(json.utf8))
        #expect(record.citations?.map(\.label) == ["Sam"])

        let plain = try JSONDecoder().decode(SessionRecord.self, from: Data(#"{"kind":"turn","role":"user","text":"hi"}"#.utf8))
        #expect(plain.citations == nil)
    }
}
