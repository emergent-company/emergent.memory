import SwiftUI

// MARK: - Thinking block

/// A collapsible thinking/reasoning block. Live deltas accumulate into the
/// single `text` (store-side); the block renders above the reply.
struct ThinkingBlockView: View {
    let text: String
    var isLive: Bool = false

    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var expanded = true

    var body: some View {
        VStack(alignment: .leading, spacing: 1 * .grid) {
            Button {
                withAnimation(reduceMotion ? nil : .snappy) { expanded.toggle() }
            } label: {
                HStack(spacing: 2 * .grid) {
                    Image(systemName: "chevron.right")
                        .font(.caption2.weight(.semibold))
                        .rotationEffect(.degrees(reduceMotion ? 0 : (expanded ? 90 : 0)))
                        .foregroundStyle(.fg3)
                    if isLive {
                        ProgressView()
                            .controlSize(.mini)
                    } else {
                        Image(systemName: "brain")
                            .font(.caption)
                            .foregroundStyle(.fg3)
                    }
                    Text("chat.thinking")
                        .font(MemoryFont.metaEmphasis)
                        .foregroundStyle(.fg2)
                    Spacer()
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)

            if expanded {
                Text(text)
                    .font(MemoryFont.metaMono)
                    .foregroundStyle(.fg2)
                    .textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(2 * .grid)
                    .background(.bg1, in: RoundedRectangle(cornerRadius: .cornerRadiusSmall))
                    .transition(reduceMotion ? .opacity : .opacity.combined(with: .move(edge: .top)))
            }
        }
        .padding(2 * .grid)
        .bubbleBackground(.assistant, cornerRadius: .cornerRadiusSmall)
    }
}

// MARK: - Approval card

/// Interactive approval request: tool name + argument summary and
/// Approve / Reject (reject optionally with a reason). Renders as a
/// non-interactive outcome summary once answered.
struct ApprovalCardView: View {
    let request: ApprovalRequest
    let onDecide: (ApprovalDecision) -> Void

    @State private var showingReason = false
    @State private var reason = ""
    /// Bumped on approve / reject submit to drive the decision haptics.
    @State private var approveFeedback = 0
    @State private var rejectFeedback = 0

    var body: some View {
        VStack(alignment: .leading, spacing: 2 * .grid) {
            header

            if let decision = request.decision {
                outcome(decision)
            } else {
                controls
            }
        }
        .padding(3 * .grid)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(.bg2, in: RoundedRectangle(cornerRadius: .cornerRadiusSmall))
        #if os(iOS)
            .sensoryFeedback(.success, trigger: approveFeedback)
            .sensoryFeedback(.warning, trigger: rejectFeedback)
        #endif
    }

    private var header: some View {
        HStack(spacing: 2 * .grid) {
            Image(systemName: "hand.raised.fill")
                .font(.caption)
                .foregroundStyle(.fgAccent)
            VStack(alignment: .leading, spacing: 1) {
                Text(request.tool)
                    .font(MemoryFont.cardHeader)
                    .foregroundStyle(.fg1)
                    .lineLimit(1)
                Text("chat.approval.title")
                    .font(MemoryFont.meta)
                    .foregroundStyle(.fg3)
            }
            Spacer()
        }
    }

    @ViewBuilder
    private var controls: some View {
        if !request.arguments.isEmpty {
            Text(request.arguments)
                .font(MemoryFont.metaMono)
                .foregroundStyle(.fg2)
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(2 * .grid)
                .background(.bg1, in: RoundedRectangle(cornerRadius: .cornerRadiusSmall))
        }

        HStack(spacing: 2 * .grid) {
            Button {
                approveFeedback += 1
                onDecide(.approve)
            } label: {
                Text("chat.approval.approve")
            }
            .buttonStyle(DecisionButtonStyle(tint: .fgSuccess))

            Button {
                withAnimation(.snappy) { showingReason.toggle() }
            } label: {
                Text("chat.approval.reject")
            }
            .buttonStyle(DecisionButtonStyle(tint: .fgSerious))
        }

        if showingReason {
            HStack(spacing: 2 * .grid) {
                TextField("chat.approval.reason", text: $reason, axis: .vertical)
                    .font(MemoryFont.label)
                    .textFieldStyle(.plain)
                    .padding(.horizontal, 2 * .grid)
                    .padding(.vertical, 1 * .grid)
                    .background(.bg1, in: RoundedRectangle(cornerRadius: .cornerRadiusSmall))
                Button {
                    let trimmed = reason.trimmingCharacters(in: .whitespacesAndNewlines)
                    rejectFeedback += 1
                    onDecide(.reject(reason: trimmed.isEmpty ? nil : trimmed))
                } label: {
                    Image(systemName: "paperplane.fill")
                        .font(.footnote)
                        .frame(minWidth: 44, minHeight: 44)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityLabel("chat.approval.sendReason")
            }
        }
    }

    private func outcome(_ decision: ApprovalDecision) -> some View {
        HStack(spacing: 1 * .grid) {
            switch decision {
            case .approve:
                Image(systemName: "checkmark.circle.fill")
                    .foregroundStyle(.fgSuccess)
                Text("chat.approval.approved")
                    .foregroundStyle(.fgSuccess)
            case let .reject(reason):
                Image(systemName: "xmark.circle.fill")
                    .foregroundStyle(.fgSerious)
                VStack(alignment: .leading, spacing: 1) {
                    Text("chat.approval.rejected")
                        .foregroundStyle(.fgSerious)
                    if let reason, !reason.isEmpty {
                        Text(reason)
                            .font(MemoryFont.meta)
                            .foregroundStyle(.fg2)
                    }
                }
            }
        }
        .font(MemoryFont.labelEmphasis)
    }
}

// MARK: - Question card

/// Interactive `ask_user` card: buttons / multi-select / free text per the
/// worker-normalized `interactionType`. Non-interactive once answered.
struct QuestionCardView: View {
    let request: QuestionRequest
    let onAnswer: (String) -> Void

    @State private var selectedValues: Set<String> = []
    @State private var freeText = ""
    /// Bumped on each option select / deselect to drive the selection haptic.
    @State private var selectFeedback = 0

    var body: some View {
        VStack(alignment: .leading, spacing: 2 * .grid) {
            HStack(spacing: 2 * .grid) {
                Image(systemName: "questionmark.circle.fill")
                    .font(.caption)
                    .foregroundStyle(.fgAccent)
                Text(request.question)
                    .font(MemoryFont.cardHeader)
                    .foregroundStyle(.fg1)
                Spacer()
            }

            if let answer = request.submittedAnswer {
                answeredState(answer)
            } else {
                inputControls
            }
        }
        .padding(3 * .grid)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(.bg2, in: RoundedRectangle(cornerRadius: .cornerRadiusSmall))
        #if os(iOS)
            .sensoryFeedback(.selection, trigger: selectFeedback)
        #endif
    }

    @ViewBuilder
    private var inputControls: some View {
        switch request.interactionType {
        case .buttons:
            VStack(spacing: 1 * .grid) {
                ForEach(request.options, id: \.value) { option in
                    Button {
                        selectFeedback += 1
                        onAnswer(option.value)
                    } label: {
                        VStack(alignment: .leading, spacing: 1) {
                            Text(option.label)
                                .font(MemoryFont.label)
                            if let description = option.description, !description.isEmpty {
                                Text(description)
                                    .font(MemoryFont.meta)
                                    .foregroundStyle(.fg3)
                                    .multilineTextAlignment(.leading)
                            }
                        }
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(OptionRowButtonStyle())
                }
            }
        case .multiSelect:
            VStack(alignment: .leading, spacing: 1 * .grid) {
                ForEach(request.options, id: \.value) { option in
                    Button {
                        toggle(option.value)
                    } label: {
                        HStack(spacing: 2 * .grid) {
                            Image(systemName: selectedValues.contains(option.value) ? "checkmark.square.fill" : "square")
                                .foregroundStyle(selectedValues.contains(option.value) ? .fgAccent : .fg3)
                            VStack(alignment: .leading, spacing: 1) {
                                Text(option.label)
                                    .font(MemoryFont.label)
                                if let description = option.description, !description.isEmpty {
                                    Text(description)
                                        .font(MemoryFont.meta)
                                        .foregroundStyle(.fg3)
                                }
                            }
                            Spacer()
                        }
                        .frame(minHeight: 44)
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                }
                if !request.options.isEmpty {
                    Button {
                        submitMultiSelect()
                    } label: {
                        Text("chat.question.submit")
                    }
                    .buttonStyle(DecisionButtonStyle(tint: .fgAccent))
                    .disabled(selectedValues.isEmpty)
                }
            }
        case .freeText:
            VStack(alignment: .leading, spacing: 2 * .grid) {
                TextField(request.placeholder?.isEmpty == false ? request.placeholder! : "chat.question.answer", text: $freeText, axis: .vertical)
                    .font(MemoryFont.label)
                    .textFieldStyle(.plain)
                    .lineLimit(1 ... 4)
                    .padding(2 * .grid)
                    .background(.bg1, in: RoundedRectangle(cornerRadius: .cornerRadiusSmall))
                    .onChange(of: freeText) { _, newValue in
                        if let maxLength = request.maxLength, maxLength > 0, newValue.count > maxLength {
                            freeText = String(newValue.prefix(maxLength))
                        }
                    }
                Button {
                    let trimmed = freeText.trimmingCharacters(in: .whitespacesAndNewlines)
                    guard !trimmed.isEmpty else { return }
                    onAnswer(trimmed)
                } label: {
                    Text("chat.question.submit")
                }
                .buttonStyle(DecisionButtonStyle(tint: .fgAccent))
                .disabled(freeText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
            }
        }
    }

    private func toggle(_ value: String) {
        selectFeedback += 1
        if selectedValues.contains(value) {
            selectedValues.remove(value)
        } else {
            selectedValues.insert(value)
        }
    }

    /// Multi-select answers join the selected values in option order.
    private func submitMultiSelect() {
        let ordered = request.options.map(\.value).filter { selectedValues.contains($0) }
        guard !ordered.isEmpty else { return }
        onAnswer(ordered.joined(separator: ", "))
    }

    private func answeredState(_ answer: String) -> some View {
        HStack(alignment: .top, spacing: 2 * .grid) {
            Image(systemName: "checkmark.circle.fill")
                .font(.footnote)
                .foregroundStyle(.fgSuccess)
            VStack(alignment: .leading, spacing: 1) {
                Text("chat.question.answered")
                    .font(MemoryFont.metaEmphasis)
                    .foregroundStyle(.fgSuccess)
                Text(answer)
                    .font(MemoryFont.label)
                    .foregroundStyle(.fg1)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
    }
}
