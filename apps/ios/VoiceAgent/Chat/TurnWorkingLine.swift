import SwiftUI

// MARK: - State

/// What the live turn shows while it has no live row of its own.
///
/// The old "typing" dots claimed the agent was writing when it might be running
/// a tool, and they sat above the very rows that already said so. The line takes
/// their place and shows only in the gap they exist for: the turn is waiting on
/// the agent and nothing on screen is moving.
enum TurnWorkingState: Equatable {
    /// Something else in the turn is live, so the line would repeat it.
    case hidden
    /// Nothing is moving yet: the agent is thinking.
    case thinking

    static func of(awaitingFirstReply: Bool, turn: ChatActivityStore.Turn?) -> TurnWorkingState {
        guard awaitingFirstReply, let turn else { return .hidden }
        // A running step's row names what it is doing, with its spinner. (If
        // the reader closed the run, that run's own line still reports it.)
        guard !turn.tools.contains(where: \.isRunning) else { return .hidden }
        // The thinking block's live header says "Thinking" while it streams.
        guard turn.thinkingText.isEmpty else { return .hidden }
        return .thinking
    }
}

// MARK: - Line

/// The live turn's working line: a small spinner, what the turn is doing, and
/// how long it has been running — one quiet line where the dots used to blink.
///
/// The elapsed time is drawn by `TimelineView`, so only that text re-renders
/// each second and the turn's rows are left alone.
struct WorkingLine: View {
    let state: TurnWorkingState
    /// When the turn began. Nil hides the timer (a turn assembled without one).
    var since: Date?

    var body: some View {
        if state == .thinking {
            HStack(spacing: 2 * .grid) {
                ProgressView()
                    .controlSize(.mini)
                    .accessibilityHidden(true)

                Text("chat.thinking")
                    .font(MemoryFont.bodySmall)
                    .foregroundStyle(.fg2)

                if let since {
                    elapsed(since)
                }

                Spacer(minLength: 4 * .grid)
            }
            .padding(.top, 1 * .grid)
            // One element: the state, then the time VoiceOver reads when it
            // focuses the line.
            .accessibilityElement(children: .combine)
        }
    }

    /// The visible short form ("12s") and the words VoiceOver reads
    /// ("12 seconds"), or nil under a second so a fast turn never flashes a
    /// time. Pairing them here keeps the two from drifting apart — the rows do
    /// the same thing with `ToolActivityTiming.spoken`.
    static func durationText(
        _ seconds: TimeInterval,
        locale: Locale = .current
    ) -> (text: String, spoken: String)? {
        guard let text = ToolActivityTiming.text(seconds, locale: locale) else { return nil }
        return (text, ToolActivityTiming.spoken(seconds, locale: locale) ?? text)
    }

    private func elapsed(_ since: Date) -> some View {
        TimelineView(.periodic(from: since, by: 1)) { context in
            if let duration = Self.durationText(context.date.timeIntervalSince(since)) {
                Text(duration.text)
                    .font(MemoryFont.meta)
                    .monospacedDigit()
                    .foregroundStyle(.fg3)
                    // The eye reads "12s"; VoiceOver reads it out in words.
                    .accessibilityLabel(Text(duration.spoken))
            }
        }
    }
}
