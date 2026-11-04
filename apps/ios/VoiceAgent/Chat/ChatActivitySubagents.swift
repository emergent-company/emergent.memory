import Foundation

// MARK: - Subagent upserts

/// Live subagent track storage lives on `ChatActivityStore`; this extension
/// owns the wire-event merge so the main store file stays under the lint size
/// limits.
extension ChatActivityStore {
    /// Upserts one subagent event by id (or run id). A terminal status replaces
    /// the working state; omitted fields keep what the store already knows.
    func upsertSubagent(_ event: ChatSubagentEvent) {
        let eventID = normalized(event.id) ?? normalized(event.runId)
        let parentRunID = normalized(event.parentRunId)
        let runID = normalized(event.runId)
        resetSubagentsIfParentChanged(parentRunID)

        if let index = subagents.firstIndex(where: {
            matchesSubagent($0, eventID: eventID, runID: runID)
        }) {
            mergeSubagent(event, into: &subagents[index], parentRunID: parentRunID, runID: runID)
            return
        }

        mutateSubagents {
            $0.append(SubagentInfo(
                id: eventID ?? UUID().uuidString,
                parentRunId: parentRunID,
                runId: runID,
                agentName: normalized(event.agentName),
                task: normalized(event.task),
                status: subagentStatus(for: event),
                error: normalized(event.error)
            ))
        }
    }

    /// A new parent run replaces the whole track; rows from the previous parent
    /// run are stale for this composer.
    private func resetSubagentsIfParentChanged(_ parentRunID: String?) {
        guard let parentRunID,
              let currentParent = subagents.first?.parentRunId,
              currentParent != parentRunID
        else { return }
        mutateSubagents { $0 = [] }
    }

    private func matchesSubagent(
        _ candidate: SubagentInfo,
        eventID: String?,
        runID: String?
    ) -> Bool {
        if let eventID, candidate.id == eventID {
            return true
        }
        if let runID, candidate.runId == runID {
            return true
        }
        return false
    }

    private func mergeSubagent(
        _ event: ChatSubagentEvent,
        into info: inout SubagentInfo,
        parentRunID: String?,
        runID: String?
    ) {
        if let parentRunID {
            info.parentRunId = parentRunID
        }
        if let runID {
            info.runId = runID
        }
        if let name = normalized(event.agentName) {
            info.agentName = name
        }
        if let task = normalized(event.task) {
            info.task = task
        }
        if event.status != nil || event.error != nil {
            info.status = subagentStatus(for: event)
        }
        if let error = normalized(event.error) {
            info.error = error
        }
    }

    private func subagentStatus(for event: ChatSubagentEvent) -> SubagentStatus {
        if event.status == nil, event.error != nil {
            return .failed
        }
        return SubagentStatus(wireValue: event.status)
    }

    private func normalized(_ value: String?) -> String? {
        guard let value else { return nil }
        let trimmed = value.trimmingCharacters(in: .whitespacesAndNewlines)
        return trimmed.isEmpty ? nil : trimmed
    }
}
