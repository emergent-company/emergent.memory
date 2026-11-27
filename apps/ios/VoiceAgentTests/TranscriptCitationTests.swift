import Foundation
@testable import Memory
import Testing

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
