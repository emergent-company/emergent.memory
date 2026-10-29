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
    /// What the result says, in readable form, parsed once when the call is
    /// built — never while the chat scrolls. Nil for a running call, an empty
    /// result, or a payload with no shape worth reading.
    let presentation: ToolResultPresentation?

    init(
        id: String,
        name: String,
        arguments: String?,
        result: String?,
        isError: Bool,
        isRunning: Bool = false,
        startedAt: Date? = nil,
        duration: TimeInterval? = nil
    ) {
        self.id = id
        self.name = name
        self.arguments = arguments
        self.result = result
        self.isError = isError
        self.isRunning = isRunning
        self.startedAt = startedAt
        self.duration = duration
        presentation = ToolResultPresentation.parse(result)
    }

    /// The category that decides this row's verb and icon.
    var category: MemoryToolCategory {
        MemoryToolCategory(toolName: name)
    }

    /// What the step was about, one line, from its arguments.
    var subject: String? {
        ToolActivitySubject.line(from: arguments)
    }

    /// The step's finished duration in short form ("2s"), when known.
    var durationText: String? {
        ToolActivityTiming.text(duration)
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

                    VStack(alignment: .leading, spacing: 0) {
                        HStack(spacing: 1 * .grid) {
                            Text(tool.isRunning ? tool.category.runningPhrase : tool.category.donePhrase)
                                .font(MemoryFont.bodySmall)
                                .foregroundStyle(tool.isRunning ? Color.fgAccent : Color.fg1)
                                .lineLimit(1)

                            if let duration = tool.durationText {
                                Text(duration)
                                    .font(MemoryFont.meta)
                                    .monospacedDigit()
                                    .foregroundStyle(.fg3)
                                    .accessibilityLabel(
                                        Text(ToolActivityTiming.spoken(tool.duration) ?? duration)
                                    )
                            }
                        }

                        // The subject is what the expanded body shows as its
                        // real input, so the collapsed row alone carries it.
                        if !expanded, let subject = tool.subject {
                            Text(subject)
                                .font(MemoryFont.meta)
                                .foregroundStyle(.fg2)
                                .lineLimit(1)
                                .truncationMode(.tail)
                        }
                    }

                    Spacer(minLength: 2 * .grid)

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
                    if !tool.name.isEmpty {
                        Text(tool.name)
                            .font(MemoryFont.metaMono)
                            .foregroundStyle(.fg3)
                    }
                    if let arguments = tool.arguments, !arguments.isEmpty {
                        detailBlock("sessions.detail.arguments", text: arguments)
                    }
                    if let presentation = tool.presentation {
                        ToolResultBody(presentation: presentation)
                    }
                }
                .padding(.leading, 4 * .grid)
                .transition(reduceMotion ? .opacity : .opacity.combined(with: .move(edge: .top)))
            }
        }
    }

    private func detailBlock(_ label: LocalizedStringKey, text: String) -> some View {
        VStack(alignment: .leading, spacing: 1 * .grid) {
            Text(label)
                .font(.caption2.weight(.semibold))
                .foregroundStyle(.fg3)
            Text(text)
                .font(MemoryFont.metaMono)
                .foregroundStyle(.fg1)
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(2 * .grid)
                .background(.bg1, in: RoundedRectangle(cornerRadius: .cornerRadiusSmall))
        }
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
