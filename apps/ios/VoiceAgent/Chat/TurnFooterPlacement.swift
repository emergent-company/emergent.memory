import Foundation

/// Where a turn footer goes: on the last assistant row of every run whose
/// record says it ended, carrying that run's model, duration and end time.
///
/// Boundary rows feed this and are never drawn themselves — the web's
/// `attachTurnFooters` hides them the same way. A reply with no run, or a run
/// with no `run_end`/`completed_at`, gets nothing: the footer must not claim a
/// turn is over while it is still streaming, and it cannot date one that has no
/// end.
enum TurnFooterPlacement {
    /// Assistant row id → its footer facts. Only the **last** assistant row of
    /// a run carries the footer, as on the web (one footer per turn, even when
    /// tools split the reply into several text rows).
    static func byAssistant(from items: [TimelineItem]) -> [String: TurnRunInfo] {
        var windows: [String: RunWindow] = [:]
        var lastAssistant: [String: String] = [:]

        for item in items {
            switch item.kind {
            case let .runBoundary(window):
                // The pair arrives as two rows: `run_start` names the model,
                // `run_end` dates it. Merge them into one window per run.
                windows[window.id] = merged(windows[window.id], with: window)
            case .assistantText:
                if let runID = item.runID {
                    lastAssistant[runID] = item.id
                }
            default:
                continue
            }
        }

        return lastAssistant.reduce(into: [String: TurnRunInfo]()) { footers, entry in
            let (runID, assistantID) = entry
            guard let window = windows[runID],
                  let endedAt = window.completedAt
            else { return }
            footers[assistantID] = TurnRunInfo(
                model: window.model,
                startedAt: window.startedAt,
                endedAt: endedAt
            )
        }
    }

    /// One run's two boundary rows as one window: the later row's fields win,
    /// and a field only one row carries is kept.
    private static func merged(_ existing: RunWindow?, with next: RunWindow) -> RunWindow {
        guard var window = existing else { return next }
        if let model = next.model {
            window.model = model
        }
        if let status = next.status {
            window.status = status
        }
        if let startedAt = next.startedAt {
            window.startedAt = startedAt
        }
        if let completedAt = next.completedAt {
            window.completedAt = completedAt
        }
        if let error = next.error {
            window.error = error
        }
        return window
    }
}
