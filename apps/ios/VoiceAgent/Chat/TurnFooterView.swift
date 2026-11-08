import SwiftUI

/// A finished turn's footer: model, wall-clock duration, end time, and a
/// "Copy turn" action — drawn only when the turn's run carries all three.
///
/// Nothing mounts this yet. The run data is blocked on the history timeline
/// (`emergent.memory#431` for the route, `emergent.memory#435` for the decode),
/// and a live stream carries no model or duration either. Until then the view
/// is invisible by construction: `run` is nil, and even a partial run renders
/// nothing rather than a footer that cannot be interpreted and would vanish on
/// reload.
struct TurnFooterView: View {
    /// The turn's run, when the transport can supply one.
    let run: TurnRunInfo?
    /// What "Copy turn" writes: the reply's plain text.
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
                CopyButton(text: text, label: "chat.turn.copy")
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
        // Renders nothing: the data plumbing is not here yet.
        TurnFooterView(run: nil, text: { "" })
    }
    .padding()
    .background(.bg1)
}
