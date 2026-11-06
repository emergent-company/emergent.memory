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

/// What a run's summary line reports: how many calls it holds, the names a
/// reader would recognise, and how many of them failed — the same line the web
/// console draws above the folded steps.
struct ToolActivityRun {
    let tools: [ToolCallInfo]

    var steps: Int {
        tools.count
    }

    /// How many of the run's steps failed. A collapsed run must not hide that:
    /// the rows that carried the error are folded away with it.
    var failedCount: Int {
        tools.filter(\.isError).count
    }

    /// Whether any step failed.
    var hasError: Bool {
        failedCount > 0
    }

    /// How many distinct names the breakdown names before it summarizes the
    /// tail, matching the web console.
    static let maxBreakdownNames = 3

    /// The names a reader would recognise, in order of first use: up to three
    /// distinct tools with a ×N multiplier, then "+N more" for the tail.
    ///
    /// The web console builds its line from the same raw names (its
    /// `breakdownText`), so a run of steps reads identically on both surfaces.
    var breakdown: String? {
        var counts: [String: Int] = [:]
        var order: [String] = []
        for tool in tools {
            let name = tool.name
            guard !name.isEmpty else { continue }
            if counts[name] == nil {
                order.append(name)
            }
            counts[name, default: 0] += 1
        }
        guard !order.isEmpty else { return nil }
        var parts: [String] = []
        for name in order.prefix(Self.maxBreakdownNames) {
            let label = Self.humanized(name)
            let count = counts[name] ?? 1
            parts.append(count > 1 ? "\(label) ×\(count)" : label)
        }
        if order.count > Self.maxBreakdownNames {
            parts.append("+\(order.count - Self.maxBreakdownNames) more")
        }
        return parts.joined(separator: ", ")
    }

    /// A wire name made readable the way the web console renders it:
    /// `entity-search` → "Entity search".
    static func humanized(_ name: String) -> String {
        let trimmed = name.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return name }
        // The same three mutations the web console makes, in the same order, so
        // the two surfaces name a run's tools identically — including the
        // punctuation-only name that collapses to nothing.
        let collapsed = trimmed
            .replacingOccurrences(of: #"[._\-]+"#, with: " ", options: .regularExpression)
            .replacingOccurrences(of: #"\s+"#, with: " ", options: .regularExpression)
            .trimmingCharacters(in: .whitespacesAndNewlines)
            .lowercased()
        guard !collapsed.isEmpty else { return "" }
        return collapsed.prefix(1).uppercased() + collapsed.dropFirst()
    }

    /// The one element the summary line exposes to assistive technology: the
    /// label says what the run contains, the value says whether its rows are
    /// showing.
    ///
    /// SwiftUI does not vend its accessibility tree to a `UIHostingController`
    /// in the unit-test target, so the view projects this value one-to-one —
    /// one element labelled `label` with `value` — exactly as it projects
    /// `linePieces`. Everything else on the line (the wrench, the failure
    /// count's glyph, the chevron) is decorative and `accessibilityHidden`, so
    /// it has no place in this type. A real VoiceOver pass on a device remains
    /// open (#2192).
    struct SummaryAccessibility: Equatable {
        let label: String
        let value: String
    }

    /// What VoiceOver hears for a summary line in the given state and locale.
    func summaryAccessibility(expanded: Bool, locale: Locale = .current) -> SummaryAccessibility {
        let stateKey: String.LocalizationValue = expanded
            ? "chat.tool.group.a11y.expanded"
            : "chat.tool.group.a11y.collapsed"
        return SummaryAccessibility(
            label: spokenLine(locale: locale),
            value: String(localized: stateKey, locale: locale)
        )
    }

    /// The line as VoiceOver reads it: the call count, the same names the line
    /// prints, and how many steps failed — because a closed run's rows, and
    /// their error text, are not in the transcript any more.
    ///
    /// Every clause resolves in `locale`, not the process locale: the parameter
    /// is what a caller uses to speak a run in the language the rest of the
    /// screen is reading (#2125).
    func spokenLine(locale: Locale = .current) -> String {
        var line = String(localized: "chat.tool.group.calls \(steps)", locale: locale)
        if let breakdown {
            line += ", \(breakdown)"
        }
        if failedCount > 0 {
            line += ", \(String(localized: "chat.tool.group.failed \(failedCount)", locale: locale))"
        }
        return line
    }
}

// MARK: - Line contents

extension ToolActivityRun {
    /// One thing the summary line draws, in order.
    enum LinePiece: Hashable {
        /// How many tool calls the run holds.
        case calls
        /// The names of the tools involved, when there are any.
        case breakdown
        /// How many steps failed, when any did.
        case failed
    }

    /// What the summary line draws, in order.
    ///
    /// The count is always there: it is the only summary a closed run has left.
    /// The names, when the run has any, and the failure count follow it. The
    /// web console draws the same three things in the same order, so one run
    /// reads the same on both surfaces.
    ///
    /// At accessibility text sizes the names give way: the count and the failure
    /// still fit one readable line, where a breakdown would leave neither room
    /// to read. The old summary dropped its decorative glyph strip there for the
    /// same reason (#2192).
    func linePieces(at size: DynamicTypeSize = .large) -> [LinePiece] {
        var pieces: [LinePiece] = [.calls]
        if breakdown != nil, !size.isAccessibilitySize {
            pieces.append(.breakdown)
        }
        if failedCount > 0 {
            pieces.append(.failed)
        }
        return pieces
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

    /// Flips the choice exactly as tapping the summary line does, so a test can
    /// follow the same activation path the button uses (#2192).
    func toggle(_ turnID: UUID, default defaultValue: Bool) {
        setExpanded(!isExpanded(turnID, default: defaultValue), for: turnID)
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

/// A turn's tool calls as one run: how many calls it holds, which tools they
/// were, and how many failed, with the step rows behind the line. The line is
/// the web console's line, drawn natively.
struct ToolActivityGroup: View {
    let tools: [ToolCallInfo]
    let turnID: UUID

    @Environment(\.toolActivityDisclosure) private var disclosure
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @Environment(\.locale) private var locale
    @ScaledMetric(relativeTo: .caption) private var iconSize: CGFloat = 14

    private var run: ToolActivityRun {
        ToolActivityRun(tools: tools)
    }

    private var isRunning: Bool {
        tools.contains(where: \.isRunning)
    }

    private var isExpanded: Bool {
        disclosure?.isExpanded(turnID, default: isRunning) ?? isRunning
    }

    private var summaryAccessibility: ToolActivityRun.SummaryAccessibility {
        run.summaryAccessibility(expanded: isExpanded, locale: locale)
    }

    var body: some View {
        if ToolActivityGrouping.isGrouped(tools) {
            VStack(alignment: .leading, spacing: 1 * .grid) {
                header
                if isExpanded {
                    stepList
                }
            }
        } else {
            ForEach(tools) { tool in
                LiveToolChip(tool: tool)
            }
        }
    }

    /// The opened rows, capped and scrolled like the web console's 24rem run:
    /// a long run stays a readable block instead of pushing the reply away.
    private var stepList: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 1 * .grid) {
                ForEach(tools) { tool in
                    LiveToolChip(tool: tool)
                }
            }
        }
        .frame(maxHeight: Self.expandedMaxHeight)
        .scrollBounceBehavior(.basedOnSize)
    }

    /// `24rem` at the web console's body size.
    private static let expandedMaxHeight: CGFloat = 384

    // MARK: Line

    private var header: some View {
        Button {
            withAnimation(reduceMotion ? nil : .snappy) {
                disclosure?.toggle(turnID, default: isRunning)
            }
        } label: {
            HStack(spacing: 2 * .grid) {
                // One wrench leads the line, as on the web console: a run of
                // mixed families has no single category glyph to draw. At
                // accessibility sizes the count and the failure need its room.
                if !dynamicTypeSize.isAccessibilitySize {
                    Image(systemName: "wrench.and.screwdriver")
                        .font(.system(size: iconSize * 0.62, weight: .semibold))
                        .foregroundStyle(.fg3)
                        .frame(width: iconSize, height: iconSize)
                        .accessibilityHidden(true)
                }

                ForEach(run.linePieces(at: dynamicTypeSize), id: \.self) { piece in
                    switch piece {
                    case .calls:
                        callsText
                    case .breakdown:
                        breakdownText
                    case .failed:
                        failedText
                    }
                }

                // A run in flight spins, as the web console's summary does, so
                // a folded run still says it is working.
                if isRunning {
                    ProgressView()
                        .controlSize(.mini)
                        .accessibilityHidden(true)
                }

                Spacer(minLength: 2 * .grid)

                chevron
            }
            // A tap target, not just a label, as on a step row.
            .frame(minHeight: 44)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        // One summary element: the wrench, the failure glyph and the chevron are
        // decorative and hidden, and the count/names/failures are spoken from
        // `spokenLine` so a closed run still announces what it contains.
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(Text(verbatim: summaryAccessibility.label))
        .accessibilityValue(Text(verbatim: summaryAccessibility.value))
    }

    /// How many calls the run holds ("2 tool calls").
    private var callsText: some View {
        Text(callsKey)
            .font(MemoryFont.bodySmall)
            .foregroundStyle(.fg1)
            .lineLimit(1)
            // The count keeps its room before the decorative spacer.
            .layoutPriority(1)
    }

    /// The tools involved (" · Entity search, Web search ×2"), the line the web
    /// console prints after the count.
    private var breakdownText: some View {
        Text(verbatim: run.breakdown.map { " · \($0)" } ?? "")
            .font(MemoryFont.meta)
            .foregroundStyle(.fg3)
            .lineLimit(1)
            .truncationMode(.tail)
            .layoutPriority(-1)
    }

    /// How many steps failed ("1 failed"), a word as well as a colour so the
    /// state never rests on colour alone.
    private var failedText: some View {
        HStack(spacing: 1 * .grid) {
            Image(systemName: "exclamationmark.triangle.fill")
                .font(MemoryFont.meta)
                .accessibilityHidden(true)
            Text("chat.tool.group.failed \(run.failedCount)")
                .font(MemoryFont.meta)
                .lineLimit(1)
                .layoutPriority(1)
        }
        .foregroundStyle(.fgSerious)
    }

    private var chevron: some View {
        Image(systemName: "chevron.down")
            .font(.caption2.weight(.semibold))
            .rotationEffect(.degrees(reduceMotion ? 0 : (isExpanded ? 180 : 0)))
            .foregroundStyle(.fg3)
            .accessibilityHidden(true)
    }

    // MARK: Copy

    private var callsKey: LocalizedStringKey {
        "chat.tool.group.calls \(run.steps)"
    }
}
