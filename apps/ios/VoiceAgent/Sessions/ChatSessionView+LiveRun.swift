import Foundation
import SwiftUI

/// Reattaching to a run this client did not start (`emergent.memory#437`).
///
/// The app used to see a run only through the `POST /api/chat` stream it opened
/// itself; opening or foregrounding a chat mid-run showed the last recorded
/// state with no working bubble and no stop control. The conversation events
/// stream (`GET /api/conversations/:id/events`) is now the source of truth for
/// the run's status:
///
/// - a `refresh` frame with an active run shows the working line, keeps the
///   working activity rows the server replays, and gates the stop control;
/// - a frame with no run, a parked run, or a failure releases all of it;
/// - every populated frame reloads the recorded transcript (debounced to one
///   request in flight), so the final reply arrives without the old recorded
///   reply poll.
///
/// `InterruptedTurnRecovery` stays for a stream that dropped while the events
/// stream itself was unavailable: it is the last-resort path, not the reattach
/// path.
extension ChatSessionView {
    /// Subscribes for the open text chat. A new chat has no id yet, so this is
    /// called again from `onConversationLearned` once the `meta` frame names it.
    func startConversationEventsIfNeeded() {
        guard activeSession.mode == .text, let conversationID = openConversationID else { return }
        attachConversationEventsCallbacks()
        liveRun.attach(conversationID: conversationID)
    }

    /// Cancels the subscription and drops the attached-run UI. Called when the
    /// chat disappears, moves to the background, or switches to voice.
    func stopConversationEvents() {
        liveRun.onRefresh = nil
        liveRun.onReplay = nil
        liveRun.detach()
        endAttachedRun()
        transport.clearAttachedRun()
    }

    func attachConversationEventsCallbacks() {
        liveRun.onRefresh = { state in
            applyRunRefresh(state)
            flushPendingReplayIfReady()
        }
        liveRun.onReplay = { _ in
            flushPendingReplayIfReady()
        }
    }

    /// Applies one populated `refresh` frame: run status, working bubble, stop
    /// control, composer release and a recorded-history refresh.
    func applyRunRefresh(_ state: ConversationRunState) {
        let wasWorking = attachedTurnID != nil
        let wasCancelling = transport.isCancelling
        transport.applyRunRefresh(state)

        if state.isWorking, !transport.isStreaming {
            beginAttachedRunIfNeeded()
        } else {
            endAttachedRun()
        }

        // The client's own stream already owns the live rows; a reattached run
        // is the only case that reads the record on every refresh.
        if !transport.isStreaming {
            reloadHistoryAfterRefresh()
        }
        // A run that finished releases the queue parked behind it, one row per
        // run. A parked run waits on a decision, and a run the user stopped was
        // ended on purpose, so neither releases the queue.
        if wasWorking, !state.isWorking, !state.isParked, !wasCancelling {
            pumpQueue()
        }
    }

    /// Opens a turn for a run this client did not start. It reuses the normal
    /// turn opener with a synthetic anchor: the transcript has no message under
    /// that id, so the turn's thinking/tool rows render after recorded history
    /// with the working line above them.
    func beginAttachedRunIfNeeded() {
        guard attachedTurnID == nil else { return }
        let anchorID = "attached-run-\(UUID().uuidString)"
        activity.userTurnStarted(anchorMessageID: anchorID)
        attachedTurnID = anchorID
    }

    func endAttachedRun() {
        guard let id = attachedTurnID else { return }
        activity.discardTurn(anchoredTo: id)
        attachedTurnID = nil
    }

    /// Applies the retained `live_replay` frames once the recorded history is on
    /// screen and an active run owns the live rows. The frames are only thinking
    /// and running tools, so nothing persisted is duplicated.
    func flushPendingReplayIfReady() {
        guard !transport.isStreaming, transport.isRunning, !historyReloadInFlight else { return }
        guard history.loadedConversationID == openConversationID else { return }
        guard let events = liveRun.takePendingReplay() else { return }
        beginAttachedRunIfNeeded()
        activity.applyReplay(events)
    }

    /// Reloads the recorded transcript after a refresh, with one request in
    /// flight: a newer refresh replaces a pending one instead of stacking
    /// requests (`emergent.memory#437`).
    func reloadHistoryAfterRefresh() {
        guard let conversationID = openConversationID else { return }
        if historyReloadInFlight {
            historyReloadPending = true
            return
        }
        historyReloadInFlight = true
        Task { @MainActor in
            await history.load(conversationID: conversationID, force: true, into: transcript)
            while historyReloadPending {
                historyReloadPending = false
                guard let id = openConversationID else { continue }
                await history.load(conversationID: id, force: true, into: transcript)
            }
            historyReloadInFlight = false
            flushPendingReplayIfReady()
        }
    }

    /// The header's run status. A successful stop holds "working" (the
    /// `cancelling` state) until the next refresh or turn settle.
    var headerRunStatus: ChatHeaderRunStatus? {
        if transport.isCancelling {
            return .working
        }
        return liveRun.state.headerStatus
    }

    /// The composer is busy while this client streams its own turn, while an
    /// attached run is active, and while the activity store still considers the
    /// reply live. The stop control follows this (`emergent.memory#470`).
    var isTextRunActive: Bool {
        transport.isStreaming || transport.isRunning || activity.isGeneratingReply
    }
}

/// The chat's events-subscription lifecycle: subscribe when the chat appears or
/// returns to the foreground, and drop the connection when it leaves the screen
/// or the app backgrounds (`emergent.memory#437`). A small modifier keeps the
/// three triggers together instead of growing `ChatSessionView.body`.
struct ChatRunLifecycleModifier: ViewModifier {
    let mode: ChatMode
    let onStart: () -> Void
    let onStop: () -> Void

    @Environment(\.scenePhase) private var scenePhase

    func body(content: Content) -> some View {
        content
            .onChange(of: scenePhase) { _, phase in
                switch phase {
                case .active: onStart()
                case .background: onStop()
                default: break
                }
            }
            .onChange(of: mode) { _, mode in
                switch mode {
                case .text: onStart()
                case .voice: onStop()
                }
            }
            .onDisappear { onStop() }
    }
}

extension ChatHeaderRunStatus {
    /// The header subtitle copy, matching the web console's
    /// `renderHeaderStatus` strings.
    var label: Text {
        switch self {
        case let .waiting(count):
            if count > 0 {
                return Text("chat.run.waiting \(count)")
            }
            return Text("chat.run.waiting")
        case .working:
            return Text("chat.run.working")
        case .failed:
            return Text("chat.run.failed")
        }
    }
}
