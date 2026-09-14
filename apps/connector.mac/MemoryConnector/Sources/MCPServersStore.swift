import Combine
import Foundation

/// Data layer for the hosted MCP servers page.
///
/// Owns the list of configured servers plus the relay's runtime status, and
/// performs create/update/enable/delete against the connector's loopback
/// management API. The client is injectable so tests can stub the network; the
/// polling cadence matches the app's other monitors (immediate load, then a
/// fixed interval while the page is visible).
@MainActor
final class MCPServersStore: ObservableObject {

    /// Coarse page state. `servers` is kept across refreshes so a refresh never
    /// blanks an already-populated list.
    enum Phase: Equatable {
        case idle
        case loading
        case loaded
        case error
    }

    /// Identifies one server's tool for in-flight toggle tracking.
    struct ToolKey: Hashable, Sendable {
        let server: String
        let tool: String
    }

    @Published private(set) var phase: Phase = .idle
    @Published private(set) var servers: [HostedMCPServer] = []
    @Published private(set) var status: RelayRuntimeStatus?
    @Published private(set) var errorMessage: String?
    @Published private(set) var isLoading = false
    @Published private(set) var lastUpdated: Date?

    /// Full config + live status per server, keyed by name. Cached so the detail
    /// view can render the current `disabled_tools` list and a tool toggle can
    /// rebuild the complete config without losing any field.
    @Published private(set) var details: [String: HostedMCPServerDetail] = [:]

    /// Discovered tools per server, keyed by name. Kept alongside the cached
    /// detail so the detail view reflects a toggle's refreshed tool set.
    @Published private(set) var toolsByServer: [String: [HostedMCPTool]] = [:]

    /// Server/tool pairs with a sharing toggle in flight. The detail view
    /// disables its toggles for a server while any of its writes are pending, so
    /// two read-modify-write requests never race and drop each other's change.
    @Published private(set) var togglingTools: Set<ToolKey> = []

    private let client: MCPServersAPIClient
    private let pollInterval: TimeInterval
    private var pollTask: Task<Void, Never>?

    init(client: MCPServersAPIClient = MCPServersAPIClient(),
         pollInterval: TimeInterval = 5) {
        self.client = client
        self.pollInterval = pollInterval
    }

    /// Names of currently configured servers, for form validation (duplicate
    /// names are rejected by the engine with HTTP 409).
    var existingNames: Set<String> { Set(servers.map(\.name)) }

    // MARK: - Load

    /// Fetches the server list and relay status. The status request is
    /// best-effort: a list failure is the only thing that flips the page to the
    /// error state, so a partially available API still renders.
    func load() async {
        if servers.isEmpty { phase = .loading }
        isLoading = true
        defer { isLoading = false }

        do {
            async let listTask = client.list()
            async let statusTask = client.status()
            let fetchedServers = try await listTask
            servers = fetchedServers
            if let fetchedStatus = try? await statusTask {
                status = fetchedStatus
            }
            errorMessage = nil
            phase = .loaded
            lastUpdated = Date()
        } catch {
            errorMessage = error.localizedDescription
            if servers.isEmpty { phase = .error }
        }
    }

    /// Resets to the empty state (no request).
    func clear() {
        servers = []
        status = nil
        errorMessage = nil
        phase = .idle
        lastUpdated = nil
    }

    // MARK: - Mutations

    /// `POST /api/mcp-servers`. Returns `true` on success and refreshes the
    /// list. On failure `errorMessage` carries the engine's message.
    @discardableResult
    func create(_ config: HostedMCPServerConfig) async -> Bool {
        do {
            _ = try await client.create(config)
            return await didMutate()
        } catch {
            return didFail(error)
        }
    }

    /// `PUT /api/mcp-servers/{name}/config` (full replace).
    @discardableResult
    func update(name: String, config: HostedMCPServerConfig) async -> Bool {
        do {
            _ = try await client.update(name: name, config: config)
            return await didMutate()
        } catch {
            return didFail(error)
        }
    }

    /// `PUT /api/mcp-servers/{name}/enabled`.
    @discardableResult
    func setEnabled(name: String, enabled: Bool) async -> Bool {
        do {
            _ = try await client.setEnabled(name: name, enabled: enabled)
            return await didMutate()
        } catch {
            return didFail(error)
        }
    }

    /// `DELETE /api/mcp-servers/{name}`.
    @discardableResult
    func delete(name: String) async -> Bool {
        do {
            try await client.delete(name: name)
            return await didMutate()
        } catch {
            return didFail(error)
        }
    }

    /// `GET /api/mcp-servers/{name}/tools`. Returns `[]` and records the error
    /// message on failure (the detail view keeps rendering its other content).
    /// Caches the result so a toggle can refresh the detail view's list.
    @discardableResult
    func fetchTools(name: String) async -> [HostedMCPTool] {
        do {
            let tools = try await client.tools(name: name)
            toolsByServer[name] = tools
            return tools
        } catch {
            errorMessage = error.localizedDescription
            return []
        }
    }

    /// `GET /api/mcp-servers/{name}` — full config + live status for the detail
    /// sheet. Returns nil and records the error on failure. Caches the detail so
    /// the view (and `setToolShared`) can read the current `disabled_tools`.
    @discardableResult
    func detail(name: String) async -> HostedMCPServerDetail? {
        do {
            let detail = try await client.get(name: name)
            details[name] = detail
            return detail
        } catch {
            errorMessage = error.localizedDescription
            return nil
        }
    }

    // MARK: - Per-tool sharing

    /// Whether any tool toggle for `server` is currently in flight.
    func isSavingTools(server: String) -> Bool {
        togglingTools.contains { $0.server == server }
    }

    /// Whether the toggle for one specific tool is currently in flight.
    func isSavingTool(server: String, tool: String) -> Bool {
        togglingTools.contains(ToolKey(server: server, tool: tool))
    }

    /// The server's own, un-namespaced tool name.
    ///
    /// The tools endpoint returns the registry name `<server>_<tool>`, but
    /// `disabled_tools` stores the server-local name, so the prefix is dropped
    /// before a name is persisted (and restored when rendering a configured
    /// disabled tool that the endpoint no longer reports).
    static func ownToolName(_ discovered: String, server: String) -> String {
        let prefix = server + "_"
        if discovered.hasPrefix(prefix) {
            return String(discovered.dropFirst(prefix.count))
        }
        return discovered
    }

    /// Shares (`shared == true`) or unshares one of a server's own tools by
    /// adding/removing it from that server's `disabled_tools` list.
    ///
    /// The write is a read-modify-write: the current full config is fetched
    /// first, the one list is edited, and the *entire* config is PUT back so the
    /// transport, connection fields, enabled flag, and secrets are all preserved.
    /// A concurrent toggle for the same server is coalesced (ignored) because two
    /// overlapping read-modify-writes would drop each other's change. On any
    /// failure the error is surfaced and no partial state is published.
    @discardableResult
    func setToolShared(server: String, tool: HostedMCPTool, shared: Bool) async -> Bool {
        guard !isSavingTools(server: server) else { return false }
        let key = ToolKey(server: server, tool: tool.name)
        togglingTools.insert(key)
        defer { togglingTools.remove(key) }

        let current: HostedMCPServerDetail
        do {
            current = try await client.get(name: server)
        } catch {
            errorMessage = error.localizedDescription
            return false
        }

        var config = current.config
        let own = Self.ownToolName(tool.name, server: server)
        var disabled = config.disabledTools ?? []
        if shared {
            disabled.removeAll { $0 == own }
        } else if !disabled.contains(own) {
            disabled.append(own)
        }
        config.disabledTools = disabled.isEmpty ? nil : disabled

        do {
            _ = try await client.update(name: server, config: config)
        } catch {
            errorMessage = error.localizedDescription
            return false
        }

        errorMessage = nil
        // Publish the written config immediately so the view stays correct even
        // if the best-effort refresh below fails.
        details[server] = HostedMCPServerDetail(config: config, status: current.status)
        await refreshAfterToolChange(server: server)
        return true
    }

    /// Best-effort refresh of the server list, relay status, cached detail, and
    /// discovered tools after a sharing write. Every request uses `try?` so a
    /// refresh failure never overrides the write's success.
    private func refreshAfterToolChange(server: String) async {
        if let fetched = try? await client.list() { servers = fetched }
        if let fetched = try? await client.status() { status = fetched }
        if let detail = try? await client.get(name: server) { details[server] = detail }
        if let tools = try? await client.tools(name: server) { toolsByServer[server] = tools }
        lastUpdated = Date()
    }

    /// Clears the last error and refreshes the list after a successful change.
    private func didMutate() async -> Bool {
        errorMessage = nil
        await load()
        return true
    }

    /// Records a mutation failure and returns `false`.
    private func didFail(_ error: Error) -> Bool {
        errorMessage = error.localizedDescription
        return false
    }

    // MARK: - Polling

    /// Starts a background refresh loop. Idempotent; every tick is skipped
    /// while the engine is not running (the management API only exists then).
    /// The first refresh happens after one interval — call `load()` for the
    /// initial fetch.
    func startPolling() {
        guard pollTask == nil else { return }
        pollTask = Task { [weak self] in
            while !Task.isCancelled {
                guard let self else { return }
                try? await Task.sleep(for: .seconds(self.pollInterval))
                if Task.isCancelled { return }
                if EngineManager.shared.state == .running { await self.load() }
            }
        }
    }

    func stopPolling() {
        pollTask?.cancel()
        pollTask = nil
    }
}
