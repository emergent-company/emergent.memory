import SwiftUI

/// Search + selection state for the registry browse sheet.
///
/// Kept separate from the view so the request/error transitions are
/// unit-testable against a stubbed client. Selecting an entry only *fetches* the
/// suggestion; nothing is created here.
@MainActor
final class MCPRegistryBrowseModel: ObservableObject {
    enum Phase: Equatable {
        case idle
        case loading
        case loaded
        case empty
        case error
    }

    @Published var query = ""
    @Published private(set) var results: [RegistryServer] = []
    @Published private(set) var phase: Phase = .idle
    @Published private(set) var errorMessage: String?
    @Published private(set) var isLoading = false
    /// Registry id whose detail is currently being fetched (for a row spinner).
    @Published private(set) var loadingName: String?

    private let client: MCPRegistryAPIClient

    init(client: MCPRegistryAPIClient = MCPRegistryAPIClient()) {
        self.client = client
    }

    /// Runs a search. Keeps any existing results visible while a new query is
    /// loading, and only flips to the error/empty state when there is nothing to
    /// show.
    func search() async {
        isLoading = true
        if results.isEmpty {
            phase = .loading
        }
        defer { isLoading = false }

        do {
            let result = try await client.search(query: query)
            results = result.servers
            errorMessage = nil
            phase = result.servers.isEmpty ? .empty : .loaded
        } catch {
            errorMessage = error.localizedDescription
            if results.isEmpty {
                phase = .error
            }
        }
    }

    /// Fetches one entry's detail + suggestion. Returns `nil` and records the
    /// error on failure.
    func detail(name: String) async -> MCPRegistryServerDetail? {
        loadingName = name
        defer { loadingName = nil }

        do {
            let detail = try await client.server(name: name)
            errorMessage = nil
            return detail
        } catch {
            errorMessage = error.localizedDescription
            return nil
        }
    }
}

/// Registry browse sheet: search the official MCP registry (proxied by the
/// connector) and open the Add-server sheet prefilled from the connector's
/// suggestion. Selection never creates a server — the existing create path runs
/// only when the user confirms in the Add sheet.
struct MCPRegistryBrowseSheet: View {
    let store: MCPServersStore
    // Qualified: the app declares its own `Environment` type (Sources/Environment.swift),
    // which shadows SwiftUI's, so the property wrapper must be namespaced.
    @SwiftUI.Environment(\.dismiss) private var dismiss
    @StateObject private var model = MCPRegistryBrowseModel()
    @State private var install: MCPRegistryInstall?
    @State private var notice: String?

    /// A loaded suggestion ready to prefill the Add sheet. Identifiable so it
    /// can drive `.sheet(item:)`.
    private struct MCPRegistryInstall: Identifiable {
        let id = UUID()
        let form: MCPServerForm
        let notes: MCPRegistryNotes
    }

    var body: some View {
        VStack(spacing: 0) {
            header
            Divider()
            searchBar
            Divider()
            content
        }
        .frame(width: 560, height: 600)
        .task {
            if model.phase == .idle {
                await model.search()
            }
        }
        .sheet(item: $install) { install in
            AddMCPServerSheet(store: store,
                              prefill: install.form,
                              notes: install.notes)
        }
    }

    // MARK: - Header

    private var header: some View {
        HStack(spacing: 10) {
            Image(systemName: "magnifyingglass.circle")
                .foregroundStyle(.secondary)
            VStack(alignment: .leading, spacing: 2) {
                Text("Browse MCP Registry")
                    .font(.headline)
                Text("Search the official registry and review a suggested server before adding it.")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            Spacer(minLength: 0)
            Button("Close") { dismiss() }
                .keyboardShortcut(.cancelAction)
        }
        .padding(16)
    }

    // MARK: - Search bar

    private var searchBar: some View {
        HStack(spacing: 8) {
            Image(systemName: "magnifyingglass")
                .foregroundStyle(.secondary)
            TextField("Search by name", text: $model.query)
                .textFieldStyle(.plain)
                .autocorrectionDisabled()
                .onSubmit { Task { await model.search() } }
            if model.isLoading {
                ProgressView()
                    .controlSize(.small)
                    .help("Searching…")
            }
            Button("Search") { Task { await model.search() } }
                .disabled(model.isLoading)
        }
        .padding(12)
    }

    // MARK: - Content

    @ViewBuilder
    private var content: some View {
        if model.results.isEmpty {
            switch model.phase {
            case .idle, .loading:
                ConnectorLoadingRow(caption: "Searching the MCP registry…")
                Spacer()
            case .error:
                stateMessage(icon: "exclamationmark.triangle.fill",
                             tint: .orange,
                             title: "Can't load the registry",
                             message: model.errorMessage ?? "The connector's management API did not return a result.",
                             retry: true)
            case .empty, .loaded:
                stateMessage(icon: "magnifyingglass",
                             tint: .secondary,
                             title: "No servers found",
                             message: "Try a different search term.",
                             retry: false)
            }
        } else {
            list
        }
    }

    private var list: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 0) {
                if let errorMessage = model.errorMessage {
                    Label(errorMessage, systemImage: "exclamationmark.triangle.fill")
                        .font(.caption)
                        .foregroundStyle(.orange)
                        .fixedSize(horizontal: false, vertical: true)
                        .padding(.horizontal, 16)
                        .padding(.vertical, 8)
                    Divider()
                }
                if let notice {
                    Label(notice, systemImage: "info.circle")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                        .padding(.horizontal, 16)
                        .padding(.vertical, 8)
                    Divider()
                }
                ForEach(model.results) { server in
                    serverRow(server)
                    Divider()
                }
            }
        }
    }

    private func serverRow(_ server: RegistryServer) -> some View {
        MCPRegistryServerRow(server: server,
                             isLoading: model.loadingName == server.name,
                             isDisabled: model.loadingName != nil)
        {
            select(server)
        }
    }

    private func stateMessage(icon: String,
                              tint: Color,
                              title: String,
                              message: String,
                              retry: Bool) -> some View
    {
        HStack(alignment: .top, spacing: 12) {
            Image(systemName: icon)
                .font(.title3)
                .foregroundStyle(tint)
            VStack(alignment: .leading, spacing: 4) {
                Text(title)
                    .font(.headline)
                Text(message)
                    .font(.callout)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
            Spacer(minLength: 0)
            if retry {
                Button("Retry") { Task { await model.search() } }
            }
        }
        .padding(24)
    }

    // MARK: - Selection

    /// Fetches the entry's suggestion and opens the prefilled Add sheet. On an
    /// entry the connector cannot map locally, shows a plain notice instead.
    private func select(_ server: RegistryServer) {
        Task {
            notice = nil
            guard let detail = await model.detail(name: server.name) else { return }
            guard let suggested = detail.suggested else {
                notice = "\(server.localName) has no local package or remote endpoint the connector can suggest."
                return
            }
            install = MCPRegistryInstall(
                form: MCPServerForm(suggested: suggested, fallbackName: server.localName),
                notes: MCPRegistryNotes(detail: detail)
            )
        }
    }
}

// MARK: - Result row

/// One registry search result. Plain button styling; the sheet supplies the
/// selection action and the in-flight state.
private struct MCPRegistryServerRow: View {
    let server: RegistryServer
    let isLoading: Bool
    let isDisabled: Bool
    let onSelect: () -> Void

    var body: some View {
        Button(action: onSelect) {
            label
        }
        .buttonStyle(.plain)
        .disabled(isDisabled)
        .help("Review a suggested \(server.localName) server")
    }

    private var label: some View {
        HStack(alignment: .top, spacing: 12) {
            Image(systemName: server.hasPackages ? "shippingbox" : "network")
                .font(.title3)
                .foregroundStyle(.secondary)
                .frame(width: 22)
            VStack(alignment: .leading, spacing: 3) {
                HStack(spacing: 8) {
                    Text(server.displayTitle)
                        .font(.callout.weight(.medium))
                        .lineLimit(1)
                    badge
                }
                Text(server.name)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                    .truncationMode(.middle)
                if let description = server.description, !description.isEmpty {
                    Text(description)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(2)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
            Spacer(minLength: 8)
            if isLoading {
                ProgressView()
                    .controlSize(.small)
            } else {
                Image(systemName: "chevron.right")
                    .font(.caption.weight(.semibold))
                    .foregroundStyle(.tertiary)
            }
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 10)
        .contentShape(Rectangle())
    }

    private var badge: some View {
        Text(server.hasPackages ? "local" : "remote")
            .font(.caption2.weight(.semibold))
            .foregroundStyle(.secondary)
            .padding(.horizontal, 6)
            .padding(.vertical, 2)
            .background(
                Capsule().fill(.quaternary.opacity(0.6))
            )
    }
}
