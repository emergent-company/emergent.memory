import SwiftUI

/// Route to an agent's object browser (pushed from the start screen).
struct ObjectRoute: Hashable {
    let agentID: String
    let agentName: String
}

/// The object browser: the objects an agent created or updated, fetched from
/// the gateway's device endpoint (`GET /api/agents/:id/objects`).
///
/// Read-only — no create, edit, or delete surface. A provenance control selects
/// Any / Created by / Updated by, search text filters the loaded list
/// client-side, and a footer loads further pages. Shows loading, empty, and
/// error (with retry) states, plus pull-to-refresh.
struct ObjectBrowserView: View {
    let agentID: String
    let agentName: String

    @Environment(\.dismiss) private var dismiss
    @State private var store = ObjectBrowserStore()
    @State private var searchText = ""

    var body: some View {
        VStack(spacing: 0) {
            ScreenHeader(
                title: Text("objects.title"),
                subtitle: Text(agentName)
            ) {
                dismiss()
            }
            provenanceControl()
            content
        }
        .background(.bg1)
        // The shell hides the navigation bar for its custom chrome; reveal it
        // here so the native search field has a bar to live in. The back
        // affordance stays the ScreenHeader chevron, so hide the system one.
        #if os(iOS)
            .toolbar(.visible, for: .navigationBar)
            .navigationBarBackButtonHidden(true)
        #endif
            .searchable(text: $searchText, prompt: Text("objects.search"))
            .task { await store.load(agentID: agentID) }
            .onChange(of: store.provenance) { _, _ in
                Task { await store.load(agentID: agentID) }
            }
    }

    // MARK: - Chrome

    private func provenanceControl() -> some View {
        @Bindable var store = store
        return Picker("objects.provenance.title", selection: $store.provenance) {
            ForEach(ObjectProvenance.allCases, id: \.self) { provenance in
                Text(provenance.labelKey).tag(provenance)
            }
        }
        .pickerStyle(.segmented)
        .padding(.horizontal, 4 * .grid)
        .padding(.bottom, 2 * .grid)
    }

    // MARK: - Content

    @ViewBuilder
    private var content: some View {
        switch store.loadState {
        case .idle, .loading:
            loadingView()
        case let .failed(message):
            failureView(message)
        case .loaded:
            if store.objects.isEmpty {
                emptyView()
            } else if filteredObjects.isEmpty {
                ContentUnavailableView.search(text: searchText)
            } else {
                list
            }
        }
    }

    private var filteredObjects: [GraphObject] {
        store.objects.filter { $0.matches(searchText) }
    }

    private var list: some View {
        List {
            ForEach(filteredObjects) { object in
                NavigationLink {
                    // The same card a chat reference opens, so an object reads
                    // the same wherever it is reached from.
                    ObjectCardScreen(reference: ObjectReference(ref: object.id))
                } label: {
                    ObjectRow(object: object)
                }
                .listRowBackground(Color.clear)
            }
            if store.hasMore {
                loadMoreRow
            }
        }
        .listStyle(.plain)
        .scrollContentBackground(.hidden)
        .refreshable { await store.load(agentID: agentID) }
    }

    private var loadMoreRow: some View {
        HStack {
            Spacer()
            if store.isLoadingMore {
                Spinner()
            } else {
                Button {
                    Task { await store.loadMore(agentID: agentID) }
                } label: {
                    Text("objects.loadMore")
                        .font(MemoryFont.labelEmphasis)
                        .foregroundStyle(.fgAccent)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
            }
            Spacer()
        }
        .listRowBackground(Color.clear)
    }

    private func loadingView() -> some View {
        LoadingStateView(title: "objects.loading")
    }

    private func emptyView() -> some View {
        EmptyStateView(
            title: "objects.empty",
            description: "objects.empty.description",
            systemImage: "square.stack.3d.up"
        )
    }

    private func failureView(_ message: String) -> some View {
        ErrorStateView(
            title: "objects.error",
            description: "objects.error.description",
            message: message,
            retryLabel: "objects.retry",
            retry: { Task { await store.load(agentID: agentID) } }
        )
    }
}

/// One object row: title, type/status, and labels.
private struct ObjectRow: View {
    let object: GraphObject

    var body: some View {
        VStack(alignment: .leading, spacing: 2 * .grid) {
            Text(object.displayTitle)
                .font(MemoryFont.bodySmall)
                .foregroundStyle(.fg1)
                .lineLimit(2)
                .multilineTextAlignment(.leading)

            HStack(spacing: 2 * .grid) {
                if !object.type.isEmpty {
                    Text(object.type)
                }
                if !object.status.isEmpty {
                    Text(object.status)
                }
                if let labels = object.labels, !labels.isEmpty {
                    Text(labels.joined(separator: ", "))
                }
            }
            .font(MemoryFont.meta)
            .foregroundStyle(.fg3)
            .lineLimit(1)
        }
        .padding(.vertical, 1 * .grid)
    }
}

/// Provenance-mode display labels (grounded copy: "Any", "Created by",
/// "Updated by").
extension ObjectProvenance {
    var labelKey: LocalizedStringKey {
        switch self {
        case .any: "objects.provenance.any"
        case .created: "objects.provenance.created"
        case .updated: "objects.provenance.updated"
        }
    }
}

#Preview {
    NavigationStack {
        ObjectBrowserView(agentID: "00000000-0000-0000-0000-000000000001", agentName: "diane")
    }
}
