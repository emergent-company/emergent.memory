import Foundation
@testable import Memory
import Testing

// MARK: - Thinking contiguity (web `createThinkingHost` parity)

/// The web console's live thinking row keeps two meanings apart: `done` stops
/// the shimmer, while an activity boundary ends the row. A later delta after a
/// `done` frame still belongs to the same row and turns the shimmer back on.
///
/// `isLive` is the spinner; `isOpen` is "a later delta may still append here".
struct ThinkingContiguityTests {
    /// The web rule: `done` stops the shimmer but does not end the row. A later
    /// delta before an activity boundary appends to the same block and turns
    /// the shimmer back on.
    @MainActor
    @Test func thinkingDoneStopsTheShimmerButKeepsTheBlockOpen() {
        let store = ChatActivityStore()
        store.userTurnStarted(anchorMessageID: "m1")
        store.apply(.thinking(id: "r1", delta: "Let me"))
        store.apply(.thinkingDone(id: "r1"))

        guard let closed = store.liveTurn?.thinkingBlocks.last else {
            Issue.record("expected a thinking block")
            return
        }
        #expect(closed.text == "Let me")
        #expect(closed.isLive == false)
        #expect(closed.isOpen == true)

        store.apply(.thinking(id: "r1", delta: " check"))

        guard let turn = store.liveTurn else {
            Issue.record("expected a live turn")
            return
        }
        #expect(turn.thinkingBlocks.count == 1)
        #expect(turn.thinkingBlocks[0].text == "Let me check")
        #expect(turn.thinkingBlocks[0].isLive == true)
        #expect(turn.thinkingBlocks[0].isOpen == true)
    }

    /// `thinkingDone` is not an activity boundary: it must not close the tool
    /// run, so the next tool call still joins the same group.
    @MainActor
    @Test func thinkingDoneIsNotAnActivityBoundary() {
        let store = ChatActivityStore()
        store.userTurnStarted(anchorMessageID: "m1")
        store.apply(.toolCall(ChatToolCallEvent(id: "t1", tool: "web_search", arguments: "{}")))
        store.apply(.thinkingDone(id: "r1"))
        store.apply(.toolCall(ChatToolCallEvent(id: "t2", tool: "read_file", arguments: "{}")))

        guard let turn = store.liveTurn else {
            Issue.record("expected a live turn")
            return
        }
        #expect(turn.toolGroups.count == 1)
        #expect(turn.toolGroups[0].tools.map(\.id) == ["t1", "t2"])
    }

    /// The trap: a block whose `done` frame already stopped the shimmer is
    /// still open, so the close must be driven by `isOpen`. If it keyed off
    /// `isLive`, the boundary below would leave the block mergeable and the
    /// next reasoning phase would concatenate into it.
    @MainActor
    @Test func anActivityBoundaryAfterADoneFrameStillClosesTheMergeWindow() {
        let store = ChatActivityStore()
        store.userTurnStarted(anchorMessageID: "m1")
        store.apply(.thinking(id: "r1", delta: "first"))
        store.apply(.thinkingDone(id: "r1"))
        store.apply(.toolCall(ChatToolCallEvent(id: "t1", tool: "web_search", arguments: "{}")))
        store.apply(.thinking(id: "r2", delta: "second"))

        guard let turn = store.liveTurn else {
            Issue.record("expected a live turn")
            return
        }
        #expect(turn.thinkingBlocks.count == 2)
        #expect(turn.thinkingBlocks[0].text == "first")
        #expect(turn.thinkingBlocks[0].isOpen == false)
        #expect(turn.thinkingBlocks[0].isLive == false)
        #expect(turn.thinkingBlocks[1].text == "second")
        #expect(turn.thinkingBlocks[1].isOpen == true)
    }

    /// `sidepanel-thinking.spec.ts`: a stream that ends without a `done` frame
    /// leaves no live badge. Here the turn ending (quiet finish) closes it.
    @MainActor
    @Test func aTurnEndingWithoutADoneFrameLeavesNoLiveShimmer() {
        let store = ChatActivityStore()
        store.userTurnStarted(anchorMessageID: "m1")
        store.apply(.thinking(id: "r1", delta: "still going"))
        store.agentReplyStarted()
        store.markReplyFinished()

        guard let block = store.liveTurn?.thinkingBlocks.last else {
            Issue.record("expected a thinking block")
            return
        }
        #expect(block.text == "still going")
        #expect(block.isLive == false)
        #expect(block.isOpen == false)
    }

    /// A closed block also stays closed once the reply ends: the turn boundary
    /// is not the only thing that removes the shimmer.
    @MainActor
    @Test func aDoneFrameSurvivesTheReplyStartBoundary() {
        let store = ChatActivityStore()
        store.userTurnStarted(anchorMessageID: "m1")
        store.apply(.thinking(id: "r1", delta: "plan"))
        store.apply(.thinkingDone(id: "r1"))
        // The answer starts: an activity boundary, so the block is closed for
        // good even though the shimmer was already off.
        store.agentReplyStarted()

        guard let block = store.liveTurn?.thinkingBlocks.last else {
            Issue.record("expected a thinking block")
            return
        }
        #expect(block.text == "plan")
        #expect(block.isLive == false)
        #expect(block.isOpen == false)
    }
}
