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
    @StateObject private var store = ObjectBrowserStore()
    @State private var searchText = ""

    var body: some View {
        VStack(spacing: 0) {
            header()
            provenanceControl()
            searchField()
            content
        }
        .background(.bg1)
        .task { await store.load(agentID: agentID) }
        .onChange(of: store.provenance) { _, _ in
            Task { await store.load(agentID: agentID) }
        }
    }

    // MARK: - Chrome

    private func header() -> some View {
        HStack(spacing: 2 * .grid) {
            Button {
                dismiss()
            } label: {
                Image(systemName: "chevron.left")
                    .font(.system(size: 16, weight: .medium))
                    .foregroundStyle(.fg3)
                    .padding(2 * .grid)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel("agents.level.back")

            VStack(alignment: .leading, spacing: 1 * .grid) {
                Text("objects.title")
                    .font(.system(size: 18, weight: .semibold))
                    .foregroundStyle(.fg0)
                Text(agentName)
                    .font(.system(size: 12))
                    .foregroundStyle(.fg3)
                    .lineLimit(1)
            }
            Spacer()
        }
        .padding(.horizontal, 4 * .grid)
        .padding(.top, 1 * .grid)
        .padding(.bottom, 2 * .grid)
    }

    private func provenanceControl() -> some View {
        Picker("objects.provenance.title", selection: $store.provenance) {
            ForEach(ObjectProvenance.allCases, id: \.self) { provenance in
                Text(provenance.labelKey).tag(provenance)
            }
        }
        .pickerStyle(.segmented)
        .padding(.horizontal, 4 * .grid)
        .padding(.bottom, 2 * .grid)
    }

    private func searchField() -> some View {
        HStack(spacing: 2 * .grid) {
            Image(systemName: "magnifyingglass")
                .foregroundStyle(.fg3)
            TextField("objects.search", text: $searchText)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .foregroundStyle(.fg0)
            if !searchText.isEmpty {
                Button {
                    searchText = ""
                } label: {
                    Image(systemName: "xmark.circle.fill")
                        .foregroundStyle(.fg3)
                }
                .buttonStyle(.plain)
                .accessibilityLabel("objects.search.clear")
            }
        }
        .font(.system(size: 14))
        .padding(.horizontal, 3 * .grid)
        .padding(.vertical, 2 * .grid)
        .background(.bg2, in: RoundedRectangle(cornerRadius: .cornerRadiusSmall))
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
                    ObjectDetailView(object: object)
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
                        .font(.system(size: 14, weight: .semibold))
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
        VStack(spacing: 3 * .grid) {
            Spinner()
            Text("objects.loading")
                .font(.system(size: 13))
                .foregroundStyle(.fg3)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    private func emptyView() -> some View {
        ContentUnavailableView {
            Label("objects.empty", systemImage: "square.stack.3d.up")
        } description: {
            Text("objects.empty.description")
        }
    }

    private func failureView(_ message: String) -> some View {
        ContentUnavailableView {
            Label("objects.error", systemImage: "wifi.exclamationmark")
        } description: {
            VStack(spacing: 1 * .grid) {
                Text("objects.error.description")
                Text(message)
                    .font(.system(size: 12))
                    .foregroundStyle(.fg3)
            }
        } actions: {
            Button {
                Task { await store.load(agentID: agentID) }
            } label: {
                Text("objects.retry")
                    .font(.system(size: 14, weight: .semibold))
                    .foregroundStyle(.fgAccent)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .padding(.top, 2 * .grid)
        }
    }
}

/// One object row: title, type/status, and labels.
private struct ObjectRow: View {
    let object: GraphObject

    var body: some View {
        VStack(alignment: .leading, spacing: 2 * .grid) {
            Text(object.displayTitle)
                .font(.system(size: 15))
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
            .font(.system(size: 11))
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
