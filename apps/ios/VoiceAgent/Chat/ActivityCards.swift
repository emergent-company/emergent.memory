import SwiftUI

// MARK: - Thinking block

/// A collapsible thinking/reasoning block. Live deltas accumulate into the
/// single `text` (store-side); the block renders above the reply.
struct ThinkingBlockView: View {
    let text: String
    var isLive: Bool = false

    @State private var expanded = true

    var body: some View {
        VStack(alignment: .leading, spacing: 1 * .grid) {
            Button {
                withAnimation(.snappy) { expanded.toggle() }
            } label: {
                HStack(spacing: 2 * .grid) {
                    Image(systemName: "chevron.right")
                        .font(.system(size: 10, weight: .semibold))
                        .rotationEffect(.degrees(expanded ? 90 : 0))
                        .foregroundStyle(.fg3)
                    if isLive {
                        ProgressView()
                            .controlSize(.mini)
                    } else {
                        Image(systemName: "brain")
                            .font(.system(size: 11))
                            .foregroundStyle(.fg3)
                    }
                    Text("chat.thinking")
                        .font(.system(size: 12, weight: .semibold))
                        .foregroundStyle(.fg2)
                    Spacer()
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)

            if expanded {
                Text(text)
                    .font(.system(size: 12, design: .monospaced))
                    .foregroundStyle(.fg2)
                    .textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(2 * .grid)
                    .background(.bg1, in: RoundedRectangle(cornerRadius: .cornerRadiusSmall))
                    .transition(.opacity.combined(with: .move(edge: .top)))
            }
        }
        .padding(2 * .grid)
        .background(.bg2, in: RoundedRectangle(cornerRadius: .cornerRadiusSmall))
    }
}

// MARK: - Typing indicator

/// Agent-position "agent is typing" dots, shown while awaiting the first
/// reply token. Purely decorative (dismissal is driven by store state).
struct TypingIndicatorView: View {
    @State private var animating = false

    var body: some View {
        HStack(spacing: 2 * .grid) {
            ForEach(0 ..< 3, id: \.self) { index in
                Circle()
                    .fill(.fg3)
                    .frame(width: 5, height: 5)
                    .opacity(animating ? 0.35 : 1)
                    .animation(
                        .easeInOut(duration: 0.6)
                            .repeatForever(autoreverses: true)
                            .delay(Double(index) * 0.2),
                        value: animating
                    )
            }
        }
        .padding(.horizontal, 3 * .grid)
        .padding(.vertical, 2 * .grid)
        .background(
            RoundedRectangle(cornerRadius: .cornerRadiusLarge)
                .fill(.bg1)
                .overlay(
                    RoundedRectangle(cornerRadius: .cornerRadiusLarge)
                        .stroke(.separator1.opacity(0.5), lineWidth: 1)
                )
        )
        .onAppear { animating = true }
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
    }

    private var header: some View {
        HStack(spacing: 2 * .grid) {
            Image(systemName: "hand.raised.fill")
                .font(.system(size: 12))
                .foregroundStyle(.fgAccent)
            VStack(alignment: .leading, spacing: 1) {
                Text(request.tool)
                    .font(.system(size: 13, weight: .semibold))
                    .foregroundStyle(.fg1)
                    .lineLimit(1)
                Text("chat.approval.title")
                    .font(.system(size: 11))
                    .foregroundStyle(.fg3)
            }
            Spacer()
        }
    }

    @ViewBuilder
    private var controls: some View {
        if !request.arguments.isEmpty {
            Text(request.arguments)
                .font(.system(size: 11, design: .monospaced))
                .foregroundStyle(.fg2)
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(2 * .grid)
                .background(.bg1, in: RoundedRectangle(cornerRadius: .cornerRadiusSmall))
        }

        HStack(spacing: 2 * .grid) {
            Button {
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
                    .font(.system(size: 13))
                    .textFieldStyle(.plain)
                    .padding(.horizontal, 2 * .grid)
                    .padding(.vertical, 1 * .grid)
                    .background(.bg1, in: RoundedRectangle(cornerRadius: .cornerRadiusSmall))
                Button {
                    let trimmed = reason.trimmingCharacters(in: .whitespacesAndNewlines)
                    onDecide(.reject(reason: trimmed.isEmpty ? nil : trimmed))
                } label: {
                    Image(systemName: "paperplane.fill")
                        .font(.system(size: 13))
                }
                .buttonStyle(.plain)
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
                            .font(.system(size: 12))
                            .foregroundStyle(.fg2)
                    }
                }
            }
        }
        .font(.system(size: 13, weight: .semibold))
    }
}

/// Compact tinted button used on the approval card.
struct DecisionButtonStyle: ButtonStyle {
    let tint: Color

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.system(size: 13, weight: .semibold))
            .foregroundStyle(.white)
            .padding(.horizontal, 3 * .grid)
            .padding(.vertical, 1 * .grid)
            .background(
                Capsule().fill(tint.opacity(configuration.isPressed ? 0.75 : 1))
            )
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

    var body: some View {
        VStack(alignment: .leading, spacing: 2 * .grid) {
            HStack(spacing: 2 * .grid) {
                Image(systemName: "questionmark.circle.fill")
                    .font(.system(size: 12))
                    .foregroundStyle(.fgAccent)
                Text(request.question)
                    .font(.system(size: 13, weight: .semibold))
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
    }

    @ViewBuilder
    private var inputControls: some View {
        switch request.interactionType {
        case .buttons:
            VStack(spacing: 1 * .grid) {
                ForEach(request.options, id: \.value) { option in
                    Button {
                        onAnswer(option.value)
                    } label: {
                        VStack(alignment: .leading, spacing: 1) {
                            Text(option.label)
                                .font(.system(size: 13, weight: .medium))
                            if let description = option.description, !description.isEmpty {
                                Text(description)
                                    .font(.system(size: 11))
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
                                    .font(.system(size: 13, weight: .medium))
                                if let description = option.description, !description.isEmpty {
                                    Text(description)
                                        .font(.system(size: 11))
                                        .foregroundStyle(.fg3)
                                }
                            }
                            Spacer()
                        }
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
                    .font(.system(size: 13))
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
                .font(.system(size: 13))
                .foregroundStyle(.fgSuccess)
            VStack(alignment: .leading, spacing: 1) {
                Text("chat.question.answered")
                    .font(.system(size: 11, weight: .semibold))
                    .foregroundStyle(.fgSuccess)
                Text(answer)
                    .font(.system(size: 13))
                    .foregroundStyle(.fg1)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
    }
}

/// Full-width selectable option row.
struct OptionRowButtonStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.system(size: 13, weight: .medium))
            .foregroundStyle(configuration.isPressed ? .fg0 : .fg1)
            .padding(2 * .grid)
            .background(.bg1, in: RoundedRectangle(cornerRadius: .cornerRadiusSmall))
    }
}
