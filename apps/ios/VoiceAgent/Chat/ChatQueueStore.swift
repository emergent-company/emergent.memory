import Foundation
import Observation

// MARK: - Model

/// One message parked in the composer queue, before it is sent as a turn.
struct QueuedMessage: Codable, Identifiable, Equatable {
    let id: String
    var text: String
}

/// A conversation's staged queue.
///
/// `armed` records *why* the rows are waiting: they were parked behind a turn
/// that was running, so the turn's end releases them in order. A queue staged
/// with the composer idle is not armed — it waits for an explicit action
/// (Send next, or the next message sent while a turn runs).
struct ChatQueue: Codable, Equatable {
    var items: [QueuedMessage] = []
    var armed = false
}

// MARK: - Storage key

/// Where one conversation's queue lives in `UserDefaults`.
///
/// Scoped exactly like the web chat's (`memory.chat.queue.v1.<project>.<id>`): a
/// row belongs to the project it was typed in, so switching projects can never
/// surface another tenant's draft. A chat with no conversation id yet is scoped
/// to its agent under `__new__` until the `meta` frame reports the id the server
/// assigned.
struct ChatQueueKey: Equatable {
    static let prefix = "memory.chat.queue.v1."
    static let newChatMarker = "__new__"

    let storageKey: String

    init(projectID: String, conversationID: String) {
        storageKey = Self.prefix + "\(projectID).\(conversationID)"
    }

    init(projectID: String, agentName: String) {
        storageKey = Self.prefix + "\(projectID).\(Self.newChatMarker).\(agentName)"
    }
}

// MARK: - Store

/// The composer queue for the open conversation: its rows, whether they are
/// armed for release, and the four release rules ported from the web chat
/// (`enqueue`, `sendNow`, `pumpQueue`, `maybeReleaseOnReopen`).
///
/// Sending is not this type's job: a pump and a Send next take a `dispatch`
/// closure that starts one turn and reports whether it started, so the item can
/// be put back when the transport refused it.
@MainActor
@Observable
final class ChatQueueStore {
    /// Rows waiting to be sent, oldest first.
    private(set) var items: [QueuedMessage] = []
    /// True when the rows are parked behind a running turn and should be
    /// released when it ends.
    private(set) var armed = false

    @ObservationIgnored private let defaults: UserDefaults
    @ObservationIgnored private var projectID = ""
    @ObservationIgnored private var agentName = ""
    @ObservationIgnored private var conversationID: String?

    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
    }

    // MARK: Scope

    /// Points the store at one conversation and loads its queue.
    ///
    /// Re-scoping to the same key keeps the in-memory rows: a turn end re-reads
    /// the view's state, and reloading would drop rows added since.
    func scope(projectID: String, agentName: String, conversationID: String?) {
        let unchanged = projectID == self.projectID
            && agentName == self.agentName
            && conversationID == self.conversationID
        self.projectID = projectID
        self.agentName = agentName
        self.conversationID = conversationID
        guard !unchanged else { return }
        let loaded = load()
        items = loaded.items
        armed = loaded.armed
    }

    /// The key this conversation's queue is stored under.
    var key: ChatQueueKey {
        if let conversationID, !conversationID.isEmpty {
            return ChatQueueKey(projectID: projectID, conversationID: conversationID)
        }
        return ChatQueueKey(projectID: projectID, agentName: agentName)
    }

    /// The `meta` frame taught us the conversation id a new chat was given: the
    /// `__new__` queue moves with it, so a staged follow-up is never stranded or
    /// delivered to another conversation.
    func learnConversationID(_ id: String) {
        guard !id.isEmpty, conversationID == nil else { return }
        let previous = key
        conversationID = id
        if !items.isEmpty || armed {
            persist()
        }
        // Drop the old slot even when it was empty: a stale `__new__` queue must
        // not surface in a later new chat for the same agent.
        defaults.removeObject(forKey: previous.storageKey)
    }

    // MARK: Sending a new row

    /// Stages `text`. Only a row parked *while a turn runs* arms the queue; an
    /// idle ⌘↩ waits for an explicit action.
    func enqueue(_ text: String, whileRunning running: Bool) {
        let message = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !message.isEmpty else { return }
        items.append(QueuedMessage(id: Self.newID(), text: message))
        if running {
            armed = true
        }
        persist()
    }

    /// Releases an armed queue in order, one turn at a time: each finished turn
    /// calls back here for the next row.
    ///
    /// Refuses while a turn is streaming (or a run is parked on a decision) and
    /// refuses an unarmed queue, so a ⌘↩ parked while idle is not silently
    /// released when a later unrelated turn ends. The last row clearing disarms
    /// the queue; a send that fails to start puts the row back at the head.
    @discardableResult
    func pump(whileRunning running: Bool, dispatch: (String) -> Bool) -> QueuedMessage? {
        guard !running, armed, let first = items.first else { return nil }
        items.removeFirst()
        if items.isEmpty {
            armed = false
        }
        persist()
        guard dispatch(first.text) else {
            items.insert(first, at: 0)
            persist()
            return nil
        }
        return first
    }

    /// Send next. When the composer is idle this sends exactly that row and
    /// disarms the rest (#1301): the turn it starts must not cascade-release
    /// them. While a turn runs the row is promoted to the head and released the
    /// moment the turn ends, ahead of every other row.
    func sendNext(_ id: String, whileRunning running: Bool, dispatch: (String) -> Bool) {
        guard let index = items.firstIndex(where: { $0.id == id }) else { return }
        let item = items.remove(at: index)
        guard !running else {
            items.insert(item, at: 0)
            armed = true
            persist()
            return
        }
        armed = false
        persist()
        guard dispatch(item.text) else {
            items.insert(item, at: 0)
            persist()
            return
        }
    }

    /// Reopening a chat: releases an armed queue when the conversation has an id
    /// and its last run has ended, so a turn that finished off-screen still
    /// delivers the follow-ups parked behind it.
    func releaseOnReopen(
        whileRunning running: Bool,
        hasConversation: Bool,
        lastRunEnded: Bool,
        dispatch: (String) -> Bool
    ) {
        guard !running, hasConversation, lastRunEnded else { return }
        pump(whileRunning: running, dispatch: dispatch)
    }

    // MARK: Rows

    func updateText(_ text: String, for id: String) {
        guard let index = items.firstIndex(where: { $0.id == id }), items[index].text != text else { return }
        items[index].text = text
        persist()
    }

    func remove(_ id: String) {
        let before = items.count
        items.removeAll { $0.id == id }
        guard items.count != before else { return }
        persist()
    }

    // MARK: Persistence

    private func load() -> ChatQueue {
        guard
            let data = defaults.data(forKey: key.storageKey),
            let queue = try? JSONDecoder().decode(ChatQueue.self, from: data)
        else { return ChatQueue() }
        // A row with nothing to send is not a message: drop it on read, as the
        // web does, so an emptied row can never be delivered.
        return ChatQueue(
            items: queue.items.filter { !$0.text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty },
            armed: queue.armed
        )
    }

    private func persist() {
        guard let data = try? JSONEncoder().encode(ChatQueue(items: items, armed: armed)) else { return }
        defaults.set(data, forKey: key.storageKey)
    }

    private static func newID() -> String {
        "q" + UUID().uuidString.replacingOccurrences(of: "-", with: "").lowercased()
    }
}
