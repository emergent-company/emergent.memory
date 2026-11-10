import SwiftUI

/// A finished turn's footer: model, wall-clock duration, end time, and a
/// "Copy turn" action — drawn only when the turn's run carries an end boundary.
///
/// `ChatTimelineView` places it on the last reply of each ended run, the same
/// one-footer-per-turn rule as the web's `attachTurnFooters`; the run-boundary
/// rows themselves are never drawn. A run with no end boundary (still going, or
/// never recorded one) has no footer.
struct TurnFooterView: View {
    /// The turn's run, once its record has both boundaries.
    let run: TurnRunInfo?
    /// What "Copy turn" writes: that reply's plain text.
    let text: () -> String
    var timeZone: TimeZone = .current
    var locale: Locale = .current

    var body: some View {
        if let run,
           let line = TurnFooterModel(run: run, timeZone: timeZone, locale: locale).line
        {
            HStack(alignment: .firstTextBaseline, spacing: 2 * .grid) {
                Text(verbatim: line)
                    .font(MemoryFont.meta)
                    .foregroundStyle(.fg3)
                    .textSelection(.enabled)
                Spacer(minLength: 2 * .grid)
                CopyButton(intent: { .turn(text()) }, label: "chat.turn.copy")
            }
            .padding(.leading, 1 * .grid)
        }
    }
}

#Preview {
    VStack(alignment: .leading, spacing: 4 * .grid) {
        // Renders: a complete run.
        TurnFooterView(
            run: TurnRunInfo(
                model: "deepseek-v4-flash",
                startedAt: Date(timeIntervalSince1970: 1_800_000_000),
                endedAt: Date(timeIntervalSince1970: 1_800_000_123)
            ),
            text: { "The reply's text." }
        )
        // Renders nothing: this turn has no run window.
        TurnFooterView(run: nil, text: { "" })
    }
    .padding()
    .background(.bg1)
}
