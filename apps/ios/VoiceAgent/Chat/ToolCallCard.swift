import SwiftUI

// MARK: - Shared tool-call model

/// One tool call in either the recorded session timeline or the live chat:
/// name, arguments, result, error flag, running state. `id` is the live
/// worker-assigned correlation id (`t1`, ...) or a synthetic index for
/// recorded timeline rows.
struct ToolCallInfo: Identifiable, Equatable {
    let id: String
    let name: String
    let arguments: String?
    let result: String?
    let isError: Bool
    let isRunning: Bool

    init(id: String, name: String, arguments: String?, result: String?, isError: Bool, isRunning: Bool = false) {
        self.id = id
        self.name = name
        self.arguments = arguments
        self.result = result
        self.isError = isError
        self.isRunning = isRunning
    }
}

// MARK: - Shared tool-call row

/// One expandable tool call: name row with a rotating chevron; expanding
/// reveals arguments and result in monospaced, selectable blocks. Shared by
/// the recorded-session timeline (`ToolCallsCard`) and the live chat chips.
struct ToolCallRow: View {
    let tool: ToolCallInfo

    @State private var expanded = false

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
                    Text(tool.name)
                        .font(.system(size: 13, weight: .medium))
                        .foregroundStyle(.fg1)
                        .lineLimit(1)
                    Spacer()
                    if tool.isRunning {
                        ProgressView()
                            .controlSize(.mini)
                    }
                    if tool.isError {
                        Image(systemName: "exclamationmark.triangle.fill")
                            .font(.system(size: 11))
                            .foregroundStyle(.fgSerious)
                    }
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .disabled(tool.isRunning)

            if expanded {
                VStack(alignment: .leading, spacing: 2 * .grid) {
                    if let arguments = tool.arguments, !arguments.isEmpty {
                        detailBlock("sessions.detail.arguments", text: arguments)
                    }
                    if let result = tool.result, !result.isEmpty {
                        detailBlock("sessions.detail.result", text: result)
                    }
                }
                .padding(.leading, 4 * .grid)
                .transition(.opacity.combined(with: .move(edge: .top)))
            }
        }
    }

    private func detailBlock(_ label: LocalizedStringKey, text: String) -> some View {
        VStack(alignment: .leading, spacing: 1 * .grid) {
            Text(label)
                .font(.system(size: 10, weight: .semibold))
                .foregroundStyle(.fg3)
            Text(text)
                .font(.system(size: 12, design: .monospaced))
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
            .font(.system(size: 12, weight: .semibold))
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
