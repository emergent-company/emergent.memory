import Foundation
import Observation

/// Owns the open conversation's events subscription and the run facts its
/// `refresh` frames report.
///
/// The view starts and stops the subscription with the chat's lifecycle (and on
/// foreground/background); this type only turns frames into state and hands the
/// retained `live_replay` frames to the caller. It never touches the transport:
/// the view decides what an active run means for the composer, the header and
/// the working bubble, which keeps the run-state rules testable without SwiftUI.
@MainActor
@Observable
final class ConversationEventsModel {
    /// The latest populated `refresh` frame. Empty while nothing is known.
    private(set) var state = ConversationRunState()
    /// Whether a connection is currently open. A backgrounded chat closes it and
    /// reconnects when it comes back.
    private(set) var isConnected = false
    /// Retained in-flight frames from the last `live_replay`, waiting for the
    /// recorded history to land so they render after it (`emergent.memory#437`).
    private(set) var pendingReplay: [ChatStreamEvent] = []

    /// Called for every populated refresh after the state is stored.
    var onRefresh: ((ConversationRunState) -> Void)?
    /// Called when a `live_replay` arrives; the caller decides when history is
    /// ready and calls ``takePendingReplay()``.
    var onReplay: (([ChatStreamEvent]) -> Void)?

    private let stream: any ConversationEventStreaming
    private var task: Task<Void, Never>?
    private var conversationID: String?

    /// The live client is the default; tests inject a stream and drive
    /// ``apply(_:)`` directly.
    init(stream: (any ConversationEventStreaming)? = nil) {
        self.stream = stream ?? ConversationEventsClient(config: MemoryConfig())
    }

    /// Subscribes to `conversationID`, replacing any other subscription. Idempotent
    /// for the same id, so foregrounding an already-watched chat does not stack
    /// connections.
    func attach(conversationID: String) {
        if self.conversationID == conversationID, task != nil {
            return
        }
        detach()
        self.conversationID = conversationID
        let eventStream = stream
        task = Task { [weak self] in
            do {
                for try await event in eventStream.events(conversationID: conversationID) {
                    guard !Task.isCancelled, let self else { return }
                    isConnected = true
                    apply(event)
                }
            } catch {
                // The client only ends the stream for a configuration that can
                // never work; a dropped connection reconnects inside the client.
            }
            guard !Task.isCancelled, let self else { return }
            isConnected = false
        }
    }

    /// Stops watching and forgets the run facts: a backgrounded or closed chat
    /// must not keep a stale "working" header.
    func detach() {
        task?.cancel()
        task = nil
        conversationID = nil
        isConnected = false
        state = ConversationRunState()
        pendingReplay = []
    }

    /// Routes one decoded frame. The fieldless connect frame is dropped here as
    /// well as in the decoder: it says nothing, and treating it as "no run"
    /// would clear a turn this client is already streaming.
    func apply(_ event: ChatStreamEvent) {
        switch event {
        case let .refresh(next):
            guard !next.isBare else { return }
            state = next
            onRefresh?(next)
        case let .replay(events):
            pendingReplay = events
            onReplay?(events)
        default:
            break
        }
    }

    /// Hands over the retained replay frames once, for the caller to apply after
    /// the recorded history is on screen. Returns nil when there is nothing new.
    func takePendingReplay() -> [ChatStreamEvent]? {
        guard !pendingReplay.isEmpty else { return nil }
        let events = pendingReplay
        pendingReplay = []
        return events
    }
}
