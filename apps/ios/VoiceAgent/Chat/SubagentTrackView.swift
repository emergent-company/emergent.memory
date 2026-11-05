import SwiftUI

/// A composer-track pill for live subagent runs, shown above the input field.
///
/// The collapsed pill segments the runs by state — working, needs input,
/// failed — and falls back to a plain total when no run needs attention. Tap
/// opens a display-only sheet with each run's agent, task, status, error and
/// run id. It deliberately never navigates to a run transcript.
struct SubagentTrackView: View {
    @Environment(ChatActivityStore.self) private var store
    @State private var showingSheet = false

    private var working: [SubagentInfo] {
        store.subagents.filter(\.status.isWorking)
    }

    private var needsInput: [SubagentInfo] {
        store.subagents.filter(\.status.needsInput)
    }

    private var failed: [SubagentInfo] {
        store.subagents.filter(\.status.hasFailed)
    }

    private var showsFallback: Bool {
        working.isEmpty && needsInput.isEmpty && failed.isEmpty
    }

    var body: some View {
        if !store.subagents.isEmpty {
            Button {
                showingSheet = true
            } label: {
                pill
            }
            .buttonStyle(.plain)
            .padding(.horizontal, 4 * .grid)
            .padding(.bottom, 1 * .grid)
            .accessibilityLabel("chat.subagent.title")
            .accessibilityValue(Text(verbatim: accessibilitySummary))
            .sheet(isPresented: $showingSheet) {
                sheet
            }
        }
    }

    private var pill: some View {
        HStack(spacing: 3 * .grid) {
            Image(systemName: "person.2")
                .font(.caption)
                .foregroundStyle(.fg3)
                .accessibilityHidden(true)

            if showsFallback {
                segment("chat.subagent.track.count \(store.subagents.count)", tint: .fg2)
            } else {
                if !working.isEmpty {
                    segment("chat.subagent.track.working \(working.count)", tint: .fgAccent)
                }
                if !needsInput.isEmpty {
                    segment("chat.subagent.track.needsInput \(needsInput.count)", tint: .fg1)
                }
                if !failed.isEmpty {
                    segment("chat.subagent.track.failed \(failed.count)", tint: .fgSerious)
                }
            }

            Spacer(minLength: 0)
        }
        .padding(.horizontal, 3 * .grid)
        .padding(.vertical, 1 * .grid)
        .frame(maxWidth: 92 * .grid, alignment: .leading)
        .background(.bg2, in: Capsule())
        .overlay {
            Capsule()
                .strokeBorder(.separator1.opacity(0.4), lineWidth: 1)
        }
        .contentShape(Capsule())
    }

    private func segment(_ key: LocalizedStringKey, tint: Color) -> some View {
        HStack(spacing: 1 * .grid) {
            Circle()
                .fill(tint)
                .frame(width: 5, height: 5)
                .accessibilityHidden(true)
            Text(key)
                .font(MemoryFont.metaEmphasis)
                .foregroundStyle(tint)
                .lineLimit(1)
        }
    }

    private var accessibilitySummary: String {
        var parts: [String] = []
        if !working.isEmpty {
            parts.append(String(localized: "chat.subagent.track.working \(working.count)"))
        }
        if !needsInput.isEmpty {
            parts.append(String(localized: "chat.subagent.track.needsInput \(needsInput.count)"))
        }
        if !failed.isEmpty {
            parts.append(String(localized: "chat.subagent.track.failed \(failed.count)"))
        }
        if parts.isEmpty {
            parts.append(String(localized: "chat.subagent.track.count \(store.subagents.count)"))
        }
        return parts.joined(separator: ", ")
    }

    private var sheet: some View {
        NavigationStack {
            List(store.subagents) { info in
                SubagentDetailRow(info: info)
            }
            .listStyle(.plain)
            .navigationTitle("chat.subagent.title")
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("chat.subagent.done") { showingSheet = false }
                }
            }
        }
        .presentationDetents([.medium, .large])
    }
}

/// One expandable row in the subagent sheet.
private struct SubagentDetailRow: View {
    let info: SubagentInfo

    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var expanded = false

    var body: some View {
        VStack(alignment: .leading, spacing: 2 * .grid) {
            Button {
                withAnimation(reduceMotion ? nil : .snappy) { expanded.toggle() }
            } label: {
                HStack(spacing: 2 * .grid) {
                    Circle()
                        .fill(info.status.tint)
                        .frame(width: 8, height: 8)
                        .accessibilityHidden(true)

                    VStack(alignment: .leading, spacing: 0) {
                        Text(verbatim: info.agentName ?? String(localized: "chat.subagent.unknownAgent"))
                            .font(MemoryFont.bodySmall)
                            .foregroundStyle(.fg1)
                            .lineLimit(1)
                        Text(info.status.localizedKey)
                            .font(MemoryFont.meta)
                            .foregroundStyle(.fg2)
                            .lineLimit(1)
                    }

                    Spacer(minLength: 2 * .grid)

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
            .accessibilityElement(children: .combine)

            if expanded {
                VStack(alignment: .leading, spacing: 2 * .grid) {
                    if let agent = info.agentName {
                        detail("chat.subagent.details.agent", value: agent)
                    }
                    if let task = info.task {
                        detail("chat.subagent.details.task", value: task)
                    }
                    detail("chat.subagent.details.status", value: info.status.localizedText)
                    if let error = info.error {
                        detail("chat.subagent.details.error", value: error, tint: .fgSerious)
                    }
                    if let runId = info.runId {
                        detail("chat.subagent.details.runId", value: runId, monospaced: true)
                    }
                }
                .transition(reduceMotion ? .opacity : .opacity.combined(with: .move(edge: .top)))
            }
        }
        .padding(.vertical, 1 * .grid)
    }

    private func detail(
        _ label: LocalizedStringKey,
        value: String,
        tint: Color = .fg1,
        monospaced: Bool = false
    ) -> some View {
        VStack(alignment: .leading, spacing: 1 * .grid) {
            Text(label)
                .font(.caption2.weight(.semibold))
                .foregroundStyle(.fg3)
            Text(verbatim: value)
                .font(monospaced ? MemoryFont.metaMono : MemoryFont.meta)
                .foregroundStyle(tint)
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
    }
}

private extension SubagentStatus {
    var localizedKey: LocalizedStringKey {
        switch self {
        case .queued: "chat.subagent.status.queued"
        case .running: "chat.subagent.status.running"
        case .completed: "chat.subagent.status.completed"
        case .failed: "chat.subagent.status.failed"
        case .paused: "chat.subagent.status.paused"
        case .canceled: "chat.subagent.status.canceled"
        case .unknown: "chat.subagent.status.unknown"
        }
    }

    var localizedText: String {
        switch self {
        case .queued: String(localized: "chat.subagent.status.queued")
        case .running: String(localized: "chat.subagent.status.running")
        case .completed: String(localized: "chat.subagent.status.completed")
        case .failed: String(localized: "chat.subagent.status.failed")
        case .paused: String(localized: "chat.subagent.status.paused")
        case .canceled: String(localized: "chat.subagent.status.canceled")
        case .unknown: String(localized: "chat.subagent.status.unknown")
        }
    }

    var tint: Color {
        switch self {
        case .queued, .running: .fgAccent
        case .paused: .fg1
        case .failed: .fgSerious
        case .completed, .canceled, .unknown: .fg3
        }
    }
}
