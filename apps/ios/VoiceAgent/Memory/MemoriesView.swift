import SwiftUI

/// Browser for the selected agent's memories, presented in a sheet from the
/// start screen. Fetches the full list on appear and filters it client-side
/// from the search field.
struct MemoriesView: View {
    @EnvironmentObject private var store: MemoryStore
    @EnvironmentObject private var agentStore: AgentStore
    @Environment(\.dismiss) private var dismiss

    @State private var searchText = ""

    var body: some View {
        NavigationStack {
            content
                .searchable(text: $searchText, prompt: "memories.search")
                .toolbar {
                    ToolbarItem(placement: .topBarTrailing) {
                        Button {
                            dismiss()
                        } label: {
                            Image(systemName: "xmark")
                                .font(.system(size: 13, weight: .medium))
                                .foregroundStyle(.fg3)
                                .padding(2 * .grid)
                                .contentShape(Rectangle())
                        }
                        .buttonStyle(.plain)
                        .accessibilityLabel("memories.close")
                    }
                }
        }
        .background(.bg1)
        .onAppear {
            store.loadMemories(agent: agentStore.selectedAgentName)
        }
    }

    @ViewBuilder
    private var content: some View {
        switch store.loadState {
        case .idle, .loading:
            ProgressView("memories.loading")
                .font(.system(size: 13))
                .foregroundStyle(.fg3)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        case let .failed(message):
            failureView(message)
        case .loaded:
            if store.memories.isEmpty {
                ContentUnavailableView {
                    Label("memories.empty", systemImage: "text.book.closed")
                } description: {
                    Text("memories.empty.description")
                }
            } else if filteredMemories.isEmpty {
                ContentUnavailableView.search(text: searchText)
            } else {
                list
            }
        }
    }

    private var list: some View {
        List(filteredMemories) { memory in
            NavigationLink(value: memory) {
                MemoryRow(memory: memory)
            }
        }
        .listStyle(.plain)
        .scrollContentBackground(.hidden)
        .background(.bg1)
        .navigationDestination(for: Memory.self) { memory in
            MemoryDetailView(memory: memory)
        }
    }

    /// The fetched list filtered by the search text (client-side).
    private var filteredMemories: [Memory] {
        let text = searchText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { return store.memories }
        return store.memories.filter { memory in
            memory.content.localizedCaseInsensitiveContains(text)
                || memory.category.localizedCaseInsensitiveContains(text)
        }
    }

    private func failureView(_ message: String) -> some View {
        ContentUnavailableView {
            Label("memories.error", systemImage: "wifi.exclamationmark")
        } description: {
            VStack(spacing: 1 * .grid) {
                Text("memories.error.description")
                Text(message)
                    .font(.system(size: 12))
                    .foregroundStyle(.fg3)
            }
        } actions: {
            Button {
                store.loadMemories(agent: agentStore.selectedAgentName)
            } label: {
                Text("memories.retry")
                    .font(.system(size: 14, weight: .semibold))
                    .foregroundStyle(.fgAccent)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .padding(.top, 2 * .grid)
        }
    }
}

/// One row in the memories list: truncated content plus a category tag.
private struct MemoryRow: View {
    let memory: Memory

    var body: some View {
        VStack(alignment: .leading, spacing: 1 * .grid) {
            Text(memory.content)
                .font(.system(size: 15))
                .foregroundStyle(.fg1)
                .lineLimit(3)
                .multilineTextAlignment(.leading)
            Text(LocalizedStringKey(memory.categoryDisplayNameKey))
                .font(.system(size: 11))
                .foregroundStyle(.fg3)
        }
        .padding(.vertical, 1 * .grid)
    }
}

#Preview {
    MemoriesView()
        .environmentObject(MemoryStore(previewMemories: [
            Memory(
                id: "00000000-0000-0000-0000-000000000001",
                content: "The user prefers lights to be warm white in the evening.",
                category: "preference",
                confidence: 0.95
            ),
            Memory(
                id: "00000000-0000-0000-0000-000000000002",
                content: "Remembers to check the garage door before bed.",
                category: "pattern",
                confidence: 0.82
            ),
        ]))
        .environmentObject(AgentStore())
}
