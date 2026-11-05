import SwiftUI

// MARK: - Shared tool-call model

/// One tool call in either the recorded session timeline or the live chat:
/// name, arguments, result, error flag, running state, and its timing. `id` is
/// the live worker-assigned correlation id (`t1`, ...) or a synthetic index for
/// recorded timeline rows.
struct ToolCallInfo: Identifiable, Equatable {
    let id: String
    let name: String
    let arguments: String?
    let result: String?
    let isError: Bool
    let isRunning: Bool
    /// When the call was announced. Live calls only: recorded rows carry no
    /// timing, so a session never shows an invented duration.
    var startedAt: Date?
    /// How long the call ran, once its result settled. Nil while running.
    var duration: TimeInterval?
    /// The size of the call's output payload on the wire, measured where it
    /// arrives: the raw result field, never a display fallback. Nil when the
    /// call produced no output (or has not settled).
    var responseSize: Int?
    /// Whether the expanded step names the call's id. Recorded timeline rows
    /// carry a synthetic index for `id`, which is not an identity the step had,
    /// so they hide it.
    var showsID: Bool
    /// The gateway's category for this call, when it sent one. It outranks the
    /// app's own name table so a call is described the way the web console
    /// describes it, verb included.
    let serverCategory: ChatToolCategory?
    /// What the gateway says the call was asked to do, in one line, from the
    /// arguments it saw. Outranks the app's own reading of the arguments.
    let serverSubject: String?
    /// The gateway's readable presentation of the result, when it sent one: the
    /// same structure the web console draws. Nil for a running call or an
    /// older gateway, which leaves `parsedPresentation` to answer.
    let serverPresentation: ToolResultPresentation?
    /// The app's own reading, computed only when the server sent no view, so a
    /// result is never parsed twice.
    private let parsedPresentation: ToolResultPresentation?

    init(
        id: String,
        name: String,
        arguments: String?,
        result: String?,
        isError: Bool,
        isRunning: Bool = false,
        startedAt: Date? = nil,
        duration: TimeInterval? = nil,
        responseSize: Int? = nil,
        showsID: Bool = true,
        category: ChatToolCategory? = nil,
        subject: String? = nil,
        presentation: ToolResultPresentation? = nil
    ) {
        self.id = id
        self.name = name
        self.arguments = arguments
        self.result = result
        self.isError = isError
        self.isRunning = isRunning
        self.startedAt = startedAt
        self.duration = duration
        self.responseSize = responseSize
        self.showsID = showsID
        serverCategory = category
        serverSubject = subject
        serverPresentation = presentation
        parsedPresentation = presentation == nil ? ToolResultPresentation.parse(result) : nil
    }

    /// The category that decides this row's icon: the gateway's family when it
    /// sent one, the app's name table otherwise.
    var category: MemoryToolCategory {
        serverCategory?.category ?? MemoryToolCategory(toolName: name)
    }

    /// What the step was about, one line: the gateway's subject when it sent
    /// one, else the app's reading of the arguments.
    var subject: String? {
        if let serverSubject {
            let trimmed = serverSubject.trimmingCharacters(in: .whitespacesAndNewlines)
            if !trimmed.isEmpty {
                return trimmed
            }
        }
        return ToolActivitySubject.line(from: arguments)
    }

    /// What the opened step reads: the gateway's presentation when it sent one,
    /// else the app's own reading. Nil for a running call, an empty result, or
    /// a payload with no shape worth reading.
    var presentation: ToolResultPresentation? {
        serverPresentation ?? parsedPresentation
    }

    /// The step's verb in the words the web console shows: the gateway's when it
    /// sent a category, the app's catalogue entry for the family otherwise (an
    /// older gateway). Nil is impossible — the fallback table always answers.
    var verbText: String {
        isRunning ? runningVerbText : doneVerbText
    }

    /// What an in-flight step says, e.g. "Searching memory".
    var runningVerbText: String {
        serverVerb { $0.runningVerb } ?? category.runningText
    }

    /// What a settled step says, e.g. "Searched memory".
    var doneVerbText: String {
        serverVerb { $0.verb } ?? category.doneText
    }

    /// The gateway's verb, when this build decoded one that is not blank. A
    /// category missing its wording degrades to the catalogue entry instead of
    /// drawing an empty line.
    private func serverVerb(_ pick: (ChatToolCategory) -> String) -> String? {
        guard let category = serverCategory else { return nil }
        let text = pick(category).trimmingCharacters(in: .whitespacesAndNewlines)
        return text.isEmpty ? nil : text
    }

    /// The call's own id, as the expanded step names it ("#call-a"), when it has
    /// one and it identifies the call — the same identity line the web console
    /// shows. A recorded row's index is not an id and is hidden.
    var idText: String? {
        guard showsID, !id.isEmpty else { return nil }
        return "#\(id)"
    }

    /// The response's wire size ("1.2 KB"), measured on the output payload the
    /// server sent rather than on a display fallback such as an error message.
    var responseSizeText: String? {
        ToolResultSize.text(bytes: responseSize)
    }

    /// The step's duration in the compact form the meta line uses ("850ms",
    /// "1.2s"), matching the web console's formatter.
    var metaDurationText: String? {
        ToolActivityTiming.metaText(duration)
    }
}

// MARK: - Shared tool-call row

/// One expandable tool call, described by what it did rather than by its wire
/// name: a category glyph on a state-tinted disc, a verb ("Searching memory" /
/// "Searched memory"), the subject it worked on, and how long it took. The
/// tool's own name, its arguments and its result stay one tap away.
///
/// Shared by the recorded-session timeline (`ToolCallsCard`) and the live chat
/// chips, so reviewing a finished run reads the same as watching one happen.
struct ToolCallRow: View {
    let tool: ToolCallInfo

    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var expanded = false

    private var tint: Color {
        ToolActivityStyle.tint(running: tool.isRunning, isError: tool.isError)
    }

    /// The state VoiceOver is told about; the glyph carries it visually.
    private var accessibilityState: LocalizedStringKey? {
        if tool.isError {
            return "chat.tool.a11y.failed"
        }
        return tool.isRunning ? "chat.tool.a11y.running" : nil
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 1 * .grid) {
            Button {
                withAnimation(reduceMotion ? nil : .snappy) { expanded.toggle() }
            } label: {
                HStack(spacing: 2 * .grid) {
                    ToolActivityIcon(
                        symbol: tool.category.symbol,
                        tint: tint,
                        discOpacity: ToolActivityStyle.discOpacity(
                            running: tool.isRunning,
                            isError: tool.isError
                        )
                    )

                    // One line, as the web chip draws it: the category's verb,
                    // then the subject the call was asked to work on. The
                    // duration and the response size belong to the opened step,
                    // where the web console puts them.
                    label
                        .font(MemoryFont.bodySmall)
                        .foregroundStyle(tool.isRunning ? Color.fgAccent : Color.fg1)
                        .lineLimit(1)
                        .truncationMode(.tail)
                        .frame(maxWidth: .infinity, alignment: .leading)

                    if tool.isRunning {
                        ProgressView()
                            .controlSize(.mini)
                    } else if tool.isError {
                        Image(systemName: "exclamationmark.triangle.fill")
                            .font(MemoryFont.meta)
                            .foregroundStyle(.fgSerious)
                            .accessibilityHidden(true)
                    }

                    Image(systemName: "chevron.down")
                        .font(.caption2.weight(.semibold))
                        .rotationEffect(.degrees(reduceMotion ? 0 : (expanded ? 180 : 0)))
                        .foregroundStyle(.fg3)
                        .accessibilityHidden(true)
                }
                // The row is a tap target, not just a label: keep it at least
                // 44pt tall without changing how it looks.
                .frame(minHeight: 44)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .disabled(tool.isRunning)
            .accessibilityElement(children: .combine)
            .accessibilityValueWhenPresent(accessibilityState)

            if expanded {
                VStack(alignment: .leading, spacing: 2 * .grid) {
                    // The web console leads an opened step with its identity,
                    // execution time and response size, then the readable
                    // result, then the call's input.
                    metaLine
                    if let presentation = tool.presentation {
                        // The arguments travel into the body so they sit before
                        // the raw payload, as the web console orders them.
                        ToolResultBody(
                            presentation: presentation,
                            toolName: tool.name,
                            arguments: tool.arguments
                        )
                    } else if let arguments = tool.arguments, !arguments.isEmpty {
                        ToolDetailBlock(label: "sessions.detail.arguments", text: arguments)
                    }
                }
                .padding(.leading, 4 * .grid)
                .transition(reduceMotion ? .opacity : .opacity.combined(with: .move(edge: .top)))
            }
        }
    }

    /// What the row calls itself: the verb, then the subject in the same line —
    /// the label the web chip draws, so a call reads identically on both
    /// surfaces. Both come from the gateway when it described the call.
    private var label: Text {
        let verb = Text(verbatim: tool.verbText)
        guard let subject = tool.subject, !subject.isEmpty else { return verb }
        return verb + Text(verbatim: " · \(subject)")
    }

    /// The opened step's identity line: "Duration 1.2s · 3.4 KB response · #id",
    /// the parts that exist, joined the way the web console joins them.
    @ViewBuilder private var metaLine: some View {
        if let text = metaText {
            Text(verbatim: text)
                .font(MemoryFont.metaMono)
                .foregroundStyle(.fg3)
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
    }

    private var metaText: String? {
        var parts: [String] = []
        if let duration = tool.metaDurationText {
            parts.append(String(localized: "chat.tool.meta.duration \(duration)"))
        }
        if let size = tool.responseSizeText {
            parts.append(String(localized: "chat.tool.meta.response \(size)"))
        }
        if let id = tool.idText {
            parts.append(id)
        }
        return parts.isEmpty ? nil : parts.joined(separator: " · ")
    }
}

// MARK: - Shared tool-calls card

/// A titled, grouped card of expandable tool-call rows — used by the recorded
/// session timeline. The live chat renders individual chips around
/// `ToolCallRow` instead.
struct ToolCallsCard: View {
    let tools: [ToolCallInfo]
    /// Record-level error flag: renders the error banner + red border.
    var isError: Bool = false

    var body: some View {
        VStack(alignment: .leading, spacing: 2 * .grid) {
            HStack(spacing: 1 * .grid) {
                Image(systemName: "wrench.and.screwdriver")
                Text("sessions.detail.tools")
                if isError {
                    Spacer()
                    Label("sessions.detail.error", systemImage: "exclamationmark.triangle.fill")
                        .foregroundStyle(.fgSerious)
                }
            }
            .font(MemoryFont.metaEmphasis)
            .foregroundStyle(.fg1)

            ForEach(tools) { tool in
                ToolCallRow(tool: tool)
            }
        }
        .padding(3 * .grid)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(.bg2, in: RoundedRectangle(cornerRadius: .cornerRadiusSmall))
        .overlay {
            if isError {
                RoundedRectangle(cornerRadius: .cornerRadiusSmall)
                    .strokeBorder(.fgSerious.opacity(0.6), lineWidth: 1)
            }
        }
    }
}

// MARK: - Live chat chip

/// A compact, bordered chip around one live tool call (running spinner while
/// the worker executes, expandable name/args/result once done).
struct LiveToolChip: View {
    let tool: ToolCallInfo

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            ToolCallRow(tool: tool)
        }
        .padding(2 * .grid)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(.bg2, in: RoundedRectangle(cornerRadius: .cornerRadiusSmall))
    }
}
