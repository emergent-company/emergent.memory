import SwiftUI

// MARK: - Grouping

/// When a turn's tool calls read as one run instead of as separate rows.
///
/// One call needs no summary line — the row already says what happened. From
/// two on, the transcript says how many steps and how long once, and keeps the
/// individual rows a tap away, so a five-step turn stops pushing the reply off
/// the screen.
enum ToolActivityGrouping {
    /// The fewest steps worth a summary line.
    static let minimumSteps = 2

    static func isGrouped(_ tools: [ToolCallInfo]) -> Bool {
        tools.count >= minimumSteps
    }
}

// MARK: - Run summary

/// What a run's summary line reports: how many steps, how long the run
/// occupied, the categories involved, and whether any step failed.
struct ToolActivityRun {
    /// One glyph on the line, carrying the strongest state its category is in.
    struct Icon: Identifiable, Equatable {
        let category: MemoryToolCategory
        let state: ToolActivityState

        var id: String {
            category.rawValue
        }

        /// Colour is state, never category (see `ToolActivityStyle`).
        var tint: Color {
            state.tint
        }
    }

    /// Beyond this many categories the line just counts; the rows still show
    /// every step.
    static let maxIcons = 4

    let tools: [ToolCallInfo]

    var steps: Int {
        tools.count
    }

    /// Also what the line has to say aloud: a collapsed run must not hide that
    /// one of its steps failed.
    var hasError: Bool {
        tools.contains(where: \.isError)
    }

    /// The run's span, not the sum of its steps: parallel calls overlap.
    var span: TimeInterval? {
        ToolActivityTiming.span(of: tools)
    }

    /// One glyph per distinct category, in order of first use.
    var icons: [Icon] {
        var seen = Set<MemoryToolCategory>()
        var result: [Icon] = []
        for tool in tools where seen.insert(tool.category).inserted {
            result.append(Icon(category: tool.category, state: state(for: tool.category)))
            if result.count == Self.maxIcons {
                break
            }
        }
        return result
    }

    /// The line as VoiceOver reads it: the count, the spoken time when the run
    /// has one, and a failure — because a closed run's rows, and their error
    /// text, are not in the transcript any more.
    ///
    /// Every clause resolves in `locale`, not the process locale: the parameter
    /// is what a caller uses to speak a run in the language the rest of the
    /// screen is reading, and a signature that only forwarded it to the time
    /// would silently mix languages (#2125).
    func spokenLine(locale: Locale = .current) -> String {
        var line = String(localized: "chat.tool.group.steps \(steps)", locale: locale)
        if let spoken = ToolActivityTiming.spoken(span, locale: locale) {
            line += ", \(spoken)"
        }
        if hasError {
            line += ", \(String(localized: "chat.tool.group.a11y.failed", locale: locale))"
        }
        return line
    }

    private func state(for category: MemoryToolCategory) -> ToolActivityState {
        let matching = tools.filter { $0.category == category }
        if matching.contains(where: \.isError) {
            return .failed
        }
        if matching.contains(where: \.isRunning) {
            return .running
        }
        return .idle
    }
}

// MARK: - Line contents

extension ToolActivityRun {
    /// One thing the summary line draws, in order.
    enum LinePiece: Hashable {
        /// The category glyph strip — decorative, so it is the first to go.
        case glyphStrip
        /// How many steps the run has.
        case steps
        /// How long the run occupied, when that is worth showing.
        case span
        /// That at least one step failed.
        case failure
    }

    /// What the summary line draws at `size`.
    ///
    /// The count is always there: it is the only summary a closed run has left.
    /// The strip is dropped at accessibility text sizes, where its scaled glyphs
    /// would take the room the count needs; the span and the failure marker
    /// follow the run. Kept here, on the model, so the rule is testable and the
    /// view is a one-to-one projection of it (#2125).
    func linePieces(at size: DynamicTypeSize) -> [LinePiece] {
        var pieces: [LinePiece] = []
        if !size.isAccessibilitySize {
            pieces.append(.glyphStrip)
        }
        pieces.append(.steps)
        if ToolActivityTiming.text(span) != nil {
            pieces.append(.span)
        }
        if hasError {
            pieces.append(.failure)
        }
        return pieces
    }
}

/// How a category's steps are doing, in the order that outranks: a failed
/// step colours the run even while a sibling still runs.
enum ToolActivityState: Equatable {
    case idle
    case running
    case failed

    /// The line's glyph colour: quiet when settled, accents while working,
    /// serious on failure — the same precedence as `ToolActivityStyle.tint`,
    /// but a shade quieter when idle (`.fg3`): the line's glyphs have no disc
    /// behind them, so they take the caption colour rather than a row's `.fg2`.
    var tint: Color {
        switch self {
        case .idle: .fg3
        case .running: .fgAccent
        case .failed: .fgSerious
        }
    }
}

// MARK: - Disclosure

/// Which runs the reader has opened or closed, remembered outside the rows.
///
/// A step's `ToolCallInfo` is replaced wholesale when its result arrives, so a
/// row's own `@State` is lost at exactly the moment it changes from running to
/// done. A group that kept its state in the view would therefore fold shut
/// under the reader as the last step settled. The choice is remembered by turn
/// instead, so settling a step can never change it.
@MainActor
@Observable
final class ToolActivityDisclosure {
    private var states: [UUID: Bool] = [:]

    /// The reader's choice, or the caller's default: open while a step runs,
    /// closed once the run has settled.
    func isExpanded(_ turnID: UUID, default defaultValue: Bool) -> Bool {
        states[turnID] ?? defaultValue
    }

    func setExpanded(_ expanded: Bool, for turnID: UUID) {
        states[turnID] = expanded
    }

    /// Drops every choice, for a new conversation.
    func clear() {
        states.removeAll()
    }
}

extension EnvironmentValues {
    /// Nil outside the chat list, where a run simply follows its default.
    @Entry var toolActivityDisclosure: ToolActivityDisclosure?
}

// MARK: - Group

/// A turn's tool calls as one run: the categories involved, how many steps and
/// how long they took, with the step rows behind the line.
struct ToolActivityGroup: View {
    let tools: [ToolCallInfo]
    let turnID: UUID

    @Environment(\.toolActivityDisclosure) private var disclosure
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @Environment(\.locale) private var locale
    @ScaledMetric(relativeTo: .caption) private var iconSize: CGFloat = 14

    /// Accessibility text sizes are where the summary line has to give something
    /// up: `linePieces` drops the decorative glyph strip (the opened rows still
    /// show every category) and the count is allowed to wrap, because the count
    /// is the one thing this line exists to say — it is the only summary a closed
    /// run has left (#2125).
    private var isAccessibilitySize: Bool {
        dynamicTypeSize.isAccessibilitySize
    }

    private var run: ToolActivityRun {
        ToolActivityRun(tools: tools)
    }

    private var isRunning: Bool {
        tools.contains(where: \.isRunning)
    }

    private var isExpanded: Bool {
        disclosure?.isExpanded(turnID, default: isRunning) ?? isRunning
    }

    var body: some View {
        if ToolActivityGrouping.isGrouped(tools) {
            VStack(alignment: .leading, spacing: 1 * .grid) {
                header
                if isExpanded {
                    ForEach(tools) { tool in
                        LiveToolChip(tool: tool)
                    }
                }
            }
        } else {
            ForEach(tools) { tool in
                LiveToolChip(tool: tool)
            }
        }
    }

    // MARK: Line

    private var header: some View {
        Button {
            withAnimation(reduceMotion ? nil : .snappy) {
                disclosure?.setExpanded(!isExpanded, for: turnID)
            }
        } label: {
            HStack(spacing: 2 * .grid) {
                ForEach(run.linePieces(at: dynamicTypeSize), id: \.self) { piece in
                    switch piece {
                    case .glyphStrip:
                        iconStrip
                    case .steps:
                        stepsText
                    case .span:
                        spanText
                    case .failure:
                        failureGlyph
                    }
                }

                Spacer(minLength: 2 * .grid)

                chevron
            }
            // A tap target, not just a label, as on a step row.
            .frame(minHeight: 44)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        // One summary element: the glyphs and the chevron are decorative and
        // hidden, and the count/time/failure are spoken from `spokenLine` so a
        // closed run still announces what it contains.
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(Text(verbatim: run.spokenLine(locale: locale)))
        .accessibilityValue(
            isExpanded ? Text("chat.tool.group.a11y.expanded") : Text("chat.tool.group.a11y.collapsed")
        )
    }

    /// The count, the one thing the line always says. It may wrap instead of
    /// ellipsising at accessibility sizes, where a phone cannot show it beside
    /// the chevron on a single line (#2125).
    private var stepsText: some View {
        Text(stepsKey)
            .font(MemoryFont.bodySmall)
            .foregroundStyle(.fg1)
            .lineLimit(isAccessibilitySize ? nil : 1)
    }

    /// The run's span at its narrowest ("12s"), when it has one.
    @ViewBuilder private var spanText: some View {
        if let time = ToolActivityTiming.text(run.span, locale: locale) {
            Text(verbatim: time)
                .font(MemoryFont.meta)
                .monospacedDigit()
                .foregroundStyle(.fg3)
        }
    }

    private var failureGlyph: some View {
        Image(systemName: "exclamationmark.triangle.fill")
            .font(MemoryFont.meta)
            .foregroundStyle(.fgSerious)
            .accessibilityHidden(true)
    }

    private var chevron: some View {
        Image(systemName: "chevron.down")
            .font(.caption2.weight(.semibold))
            .rotationEffect(.degrees(reduceMotion ? 0 : (isExpanded ? 180 : 0)))
            .foregroundStyle(.fg3)
            .accessibilityHidden(true)
    }

    private var iconStrip: some View {
        HStack(spacing: 1) {
            ForEach(run.icons) { icon in
                Image(systemName: icon.category.symbol)
                    .font(.system(size: iconSize * 0.62, weight: .semibold))
                    .foregroundStyle(icon.tint)
                    .frame(width: iconSize, height: iconSize)
            }
        }
        .accessibilityHidden(true)
    }

    // MARK: Copy

    private var stepsKey: LocalizedStringKey {
        "chat.tool.group.steps \(run.steps)"
    }
}
