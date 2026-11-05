import Foundation

/// Recognising cooperative cancellation.
///
/// A cancelled request is how `URLSession` and Swift concurrency say "this work
/// is no longer wanted": the view that started it went away, or a `.task` keyed
/// on state that just changed was replaced. It is not a network failure, and
/// showing it as one blames the connection for something the app did.
///
/// `GatewayHTTP` converts it to ``CancellationError``, so callers can match the
/// standard type; cancellation of the surrounding `.task` is visible as
/// ``isCurrentTaskCancelled`` because it arrives at the caller rather than
/// through the transport.
enum Cancellation {
    /// Whether `error` reports cancellation rather than a failure.
    static func isCancellation(_ error: Error) -> Bool {
        if error is CancellationError {
            return true
        }
        guard let urlError = error as? URLError else { return false }
        return urlError.code == .cancelled
    }

    /// Whether the calling task has been cancelled.
    ///
    /// Read in the `catch` of the task that was cancelled: a cancelled `.task`
    /// sets the flag, and the `await` that observes it throws promptly.
    static var isCurrentTaskCancelled: Bool {
        Task.isCancelled
    }
}

extension CancellationError: @retroactive LocalizedError {
    /// Guarded callers ignore cancellation, so this text is only a backstop: it
    /// keeps a stray cancelled request from printing the standard
    /// "The operation couldn’t be completed. (Swift.CancellationError error 1.)"
    /// boilerplate if one ever reaches a user-facing message.
    public var errorDescription: String? {
        "The request was cancelled."
    }
}
