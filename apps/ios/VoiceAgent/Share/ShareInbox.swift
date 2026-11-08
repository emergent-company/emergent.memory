import Foundation

/// Uploads the captures the share extension could not deliver itself.
///
/// The extension can only do the text/link fast path: a file upload cannot
/// finish inside its ~30 s budget, and any capture made while the network is
/// down has to outlive the extension's process. Those land in the shared queue,
/// and this is what drains it — on app launch, on returning to the foreground,
/// and when the user retries from the inbox.
///
/// It is an `ObservableObject` rather than a store with its own view hierarchy
/// so the picker can show a badge for the same state the inbox sheet lists.
@MainActor
final class ShareInbox: ObservableObject {
    /// Everything waiting to be delivered, oldest first.
    @Published private(set) var captures: [ShareCapture] = []
    /// Whether a drain is running, so the UI can show progress instead of
    /// offering Retry.
    @Published private(set) var isDraining = false

    /// The shared queue, or nil when the App Group is unavailable (an
    /// unprovisioned build has no shared container at all).
    private let queue: ShareQueue?
    /// How a drain gets the connection, and the session its requests use. Both
    /// are seams for tests: production reads the group defaults and uses
    /// `URLSession.shared`.
    private let loadConnection: () -> ShareConnection?
    private let session: URLSession

    var pendingCount: Int {
        captures.count
    }

    /// Whether anything queued has already failed at least once, so the badge
    /// can distinguish "waiting" from "needs attention".
    var hasFailures: Bool {
        captures.contains { $0.state == .failed }
    }

    init(
        queue: ShareQueue? = ShareQueue(),
        session: URLSession = .shared,
        connection: @escaping () -> ShareConnection? = { ShareConnection.load() }
    ) {
        self.queue = queue
        self.session = session
        loadConnection = connection
        refresh()
    }

    /// Re-reads the queue. Cheap (a directory listing), so callers do it freely.
    func refresh() {
        captures = queue?.pending() ?? []
    }

    /// Uploads everything queued, oldest first.
    ///
    /// Transport failures end the loop: the rest would fail the same way, and a
    /// row of timeouts is not a better outcome than stopping. A rejection
    /// (Memory's own refusal of one item) marks that capture failed and moves
    /// on, because the next item is usually unrelated.
    func drain() async {
        guard !isDraining else { return }
        refresh()
        guard !captures.isEmpty else { return }
        guard let queue else { return }
        // Captures made before this device was connected stay queued, not lost.
        guard let connection = loadConnection() else { return }

        isDraining = true
        defer { isDraining = false }

        let client = ShareCaptureClient(connection: connection, session: session)
        for capture in captures {
            do {
                if capture.kind == .file {
                    guard let payload = queue.payload(for: capture.id), !payload.isEmpty else {
                        recordFailure(capture, message: "The shared file is no longer on this device.")
                        continue
                    }
                    try await client.rememberFile(capture, payload: payload)
                } else {
                    try await client.remember(capture)
                }
                queue.remove(capture.id)
                ShareLog.capture.info(
                    "delivered \(capture.kind.rawValue, privacy: .public) capture \(capture.id.uuidString, privacy: .public)"
                )
            } catch let error as CaptureClientError {
                recordFailure(capture, message: error.localizedDescription)
                if error.haltsDrain {
                    break
                }
            } catch {
                recordFailure(capture, message: error.localizedDescription)
            }
            refresh()
        }
        refresh()
    }

    /// Removes one capture from the queue without uploading it.
    func discard(_ id: UUID) {
        queue?.remove(id)
        refresh()
    }

    /// Rewrites a capture as failed. A queue write failure here is only a
    /// bookkeeping loss: the capture itself stays and is retried next launch.
    private func recordFailure(_ capture: ShareCapture, message: String) {
        guard let queue else { return }
        var failed = capture
        failed.state = .failed
        failed.attempts += 1
        failed.lastError = message
        try? queue.update(failed)
    }
}
