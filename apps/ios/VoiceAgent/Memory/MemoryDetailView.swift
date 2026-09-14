import SwiftUI

/// Shows one memory's full content plus its category and confidence.
struct MemoryDetailView: View {
    let memory: Memory

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 4 * .grid) {
                Text(memory.content)
                    .font(.system(size: 17))
                    .foregroundStyle(.fg0)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .multilineTextAlignment(.leading)

                HStack(spacing: 2 * .grid) {
                    tag("memories.detail.category", Text(LocalizedStringKey(memory.categoryDisplayNameKey)))
                    tag("memories.detail.confidence", Text(memory.confidence.formatted(.number.precision(.fractionLength(0 ... 2)))))
                }
            }
            .padding(4 * .grid)
        }
        .background(.bg1)
        .navigationTitle("memories.detail.title")
        .navigationBarTitleDisplayMode(.inline)
    }

    private func tag(_ label: LocalizedStringKey, _ value: Text) -> some View {
        HStack(spacing: 1 * .grid) {
            Text(label)
            value
        }
        .font(.system(size: 12))
        .foregroundStyle(.fg3)
        .padding(.horizontal, 2 * .grid)
        .padding(.vertical, 1 * .grid)
        .background(.bg2, in: RoundedRectangle(cornerRadius: .cornerRadiusSmall))
    }
}

#Preview {
    NavigationStack {
        MemoryDetailView(memory: Memory(
            id: "00000000-0000-0000-0000-000000000001",
            content: "The user prefers lights to be warm white in the evening.",
            category: "preference",
            confidence: 0.95
        ))
    }
}
