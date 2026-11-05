import SwiftUI

/// A run of two or more consecutive tool calls, collapsed by default: a wrench
/// and the call count, a secondary breakdown of the tool names involved, and
/// running/failure state. Expanding shows the individual `ToolCallRow`s in a
/// bounded scrollable area so a long run cannot push the reply off screen.
struct ToolCallGroupView: View {
    let group: ToolCallGroup

    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var expanded = false

    /// The highest this view's step list grows before it scrolls on its own.
    private static let expandedMaxHeight: CGFloat = 400

    private var tools: [ToolCallInfo] {
        group.tools
    }

    private var isRunning: Bool {
        tools.contains(where: \.isRunning)
    }

    private var hasError: Bool {
        tools.contains(where: \.isError)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 1 * .grid) {
            header
            if expanded {
                stepList
            }
        }
        .padding(2 * .grid)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(.bg2, in: RoundedRectangle(cornerRadius: .cornerRadiusSmall))
    }

    // MARK: - Line

    private var header: some View {
        Button {
            withAnimation(reduceMotion ? nil : .snappy) { expanded.toggle() }
        } label: {
            HStack(spacing: 2 * .grid) {
                Image(systemName: "wrench.and.screwdriver")
                    .font(.caption)
                    .foregroundStyle(hasError ? Color.fgSerious : Color.fg3)
                    .accessibilityHidden(true)

                VStack(alignment: .leading, spacing: 0) {
                    Text("chat.tool.group.calls \(tools.count)")
                        .font(MemoryFont.bodySmall)
                        .foregroundStyle(.fg1)
                        .lineLimit(1)

                    if !nameBreakdown.isEmpty {
                        Text(verbatim: nameBreakdown)
                            .font(MemoryFont.meta)
                            .foregroundStyle(.fg2)
                            .lineLimit(1)
                            .truncationMode(.tail)
                    }
                }

                Spacer(minLength: 2 * .grid)

                if isRunning {
                    ProgressView()
                        .controlSize(.mini)
                        .accessibilityHidden(true)
                }
                if hasError {
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
            .frame(minHeight: 44)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(Text(verbatim: accessibilityLabel))
        .accessibilityValue(disclosureKey)
    }

    // MARK: - Steps

    private var stepList: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 1 * .grid) {
                ForEach(tools) { tool in
                    ToolCallRow(tool: tool)
                }
            }
        }
        .frame(maxHeight: Self.expandedMaxHeight)
        .scrollBounceBehavior(.basedOnSize)
        .transition(reduceMotion ? .opacity : .opacity.combined(with: .move(edge: .top)))
    }

    // MARK: - Copy

    private var disclosureKey: LocalizedStringKey {
        expanded ? "chat.tool.group.a11y.expanded" : "chat.tool.group.a11y.collapsed"
    }

    /// The tool names in first-use order, with a count when one repeats.
    private var nameBreakdown: String {
        var order: [String] = []
        var counts: [String: Int] = [:]
        for tool in tools {
            let name = tool.name.trimmingCharacters(in: .whitespacesAndNewlines)
            guard !name.isEmpty else { continue }
            if counts[name] == nil {
                order.append(name)
            }
            counts[name, default: 0] += 1
        }
        return order.map { name in
            let count = counts[name, default: 0]
            return count > 1 ? "\(name) ×\(count)" : name
        }
        .joined(separator: ", ")
    }

    private var accessibilityLabel: String {
        var label = String(localized: "chat.tool.group.calls \(tools.count)")
        if !nameBreakdown.isEmpty {
            label += ", \(nameBreakdown)"
        }
        if hasError {
            label += ", \(String(localized: "chat.tool.group.a11y.failed"))"
        }
        return label
    }
}
