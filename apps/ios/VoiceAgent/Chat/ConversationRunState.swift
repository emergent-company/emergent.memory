import Foundation

/// The run facts a conversation events stream's `refresh` frame carries.
///
/// The gateway sends `{"type":"refresh"}` with no fields on the first connect
/// (and whenever the poller has no state to broadcast), and the populated frame
/// once a run exists. Every field is optional so both shapes decode, and
/// ``isBare`` keeps the fieldless frame from clearing a turn this client is
/// already streaming (`emergent.memory#437`, `emergent.memory#470`).
struct ConversationRunState: Equatable, Sendable {
    var bucket: String?
    var runID: String?
    var runStatus: String?
    var pendingApprovals = 0
    var pendingQuestions = 0

    static let needsInputBucket = "needs_input"
    static let doneBucket = "done"

    /// The connect-time frame: no run facts to apply.
    var isBare: Bool {
        bucket == nil
            && runID == nil
            && runStatus == nil
            && pendingApprovals == 0
            && pendingQuestions == 0
    }

    var pendingDecisionCount: Int {
        pendingApprovals + pendingQuestions
    }

    /// The run is paused awaiting an approval or answer. The bucket is the
    /// authority: a parked run can still report `working` while it waits.
    var isParked: Bool {
        bucket == Self.needsInputBucket || runStatus == "input-required"
    }

    /// A status the gateway uses for a run that is still in flight.
    ///
    /// `cancelling` is a status; `running` is a bucket, kept here only because
    /// older frames and the web's `renderHeaderStatus` also accept it.
    var isWorkingStatus: Bool {
        switch runStatus {
        case "submitted", "working", "running", "cancelling": true
        default: false
        }
    }

    /// The run is in flight and not waiting on the user: the working bubble and
    /// the busy composer belong to this state.
    var isWorking: Bool {
        isWorkingStatus && !isParked
    }

    /// A run that can still be cancelled. A run already `cancelling` is not
    /// offered a second stop.
    var isActive: Bool {
        isWorking && runStatus != "cancelling"
    }

    /// Terminal failure statuses; both read as "Run failed" in the header.
    var isFailed: Bool {
        runStatus == "failed" || runStatus == "error"
    }

    /// The chat header's subtitle slot, or `nil` when there is nothing to say.
    var headerStatus: ChatHeaderRunStatus? {
        if isParked {
            return .waiting(pendingDecisionCount)
        }
        if isWorkingStatus {
            return .working
        }
        if isFailed {
            return .failed
        }
        return nil
    }
}

/// What the chat header says about the active run.
enum ChatHeaderRunStatus: Equatable, Sendable {
    /// An approval or question is waiting; the count is both kinds together.
    case waiting(Int)
    case working
    case failed
}
