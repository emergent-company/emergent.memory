package mcpregistry

// serverTypeContract is the registry-side contract for one MCPServerType.
//
// Two properties the registry API must never conflate:
//
//   - Who owns the row. registryManaged types (stdio/sse/http) are created,
//     updated and deleted through the registry API and dialed outbound by the
//     proxy. In-process types are provisioned by another domain — builtin by
//     domain/mcp, source by domain/sources — and are read-only here.
//   - How a tool call runs. inProcess types dispatch through mcp.Service;
//     registry-managed types dispatch through the outbound proxy.
//
// Every registry path that used to special-case ServerTypeBuiltin consults this
// contract instead of comparing against a literal, and the repository SQL
// filters are derived from the same table (registryManagedServerTypes /
// inProcessServerTypes). A newly added type therefore cannot silently fall
// through to the external proxy/CRUD path: until the single switch in
// MCPServerType.contract names it, its zero contract denies every
// registry-managed operation.
type serverTypeContract struct {
	// known is false for a type this build does not define.
	known bool
	// registryManaged means the registry API owns the row: CreateServer accepts
	// it, Update/Delete may mutate it, and the outbound proxy may dial it.
	registryManaged bool
	// inProcess means tool calls are dispatched in-process by domain/mcp
	// (mcp.Service), never over an outbound proxy connection.
	inProcess bool
	// listed means the row appears in the registry list reads that back the
	// settings/UI. Builtin is listed because it is a user-visible, read-only
	// catalogue; source rows are not, because they are per-account tool servers
	// owned by domain/sources and must not look user-editable.
	listed bool
	// syntheticInspect means InspectServer answers a synthetic OK instead
	// of dialing, because the server has no external connection to probe.
	syntheticInspect bool
	// remote means the type talks to an HTTP(S)/SSE endpoint, so the outbound
	// URL guard and stored OAuth credentials apply.
	remote bool
}

// contract returns the registry-side contract for t.
//
// This switch is the one place that classifies server types. Do not add a
// `|| t == ServerTypeX` at a call site: extend this switch instead.
func (t MCPServerType) contract() serverTypeContract {
	switch t {
	case ServerTypeStdio:
		return serverTypeContract{known: true, registryManaged: true, listed: true}
	case ServerTypeSSE, ServerTypeHTTP:
		return serverTypeContract{known: true, registryManaged: true, listed: true, remote: true}
	case ServerTypeBuiltin:
		// In-process tool server owned by domain/mcp. Inspect has no external
		// connection, so it answers synthetically (the historical behaviour);
		// it is intentionally listed read-only in the registry UI.
		return serverTypeContract{known: true, inProcess: true, listed: true, syntheticInspect: true}
	case ServerTypeSource:
		// In-process tool server owned by domain/sources. It has no external
		// connection and no inspectable handshake, so inspect refuses it. It is
		// not listed: source rows must never look user-editable.
		return serverTypeContract{known: true, inProcess: true}
	}
	// Unknown type: deny everything. It is not user-creatable, not deletable,
	// not inspectable, not callable and never dialed outbound.
	return serverTypeContract{}
}

// allServerTypes is every MCPServerType this build knows. The classification
// test iterates it, so a new constant must be added here as well as to the
// contract switch.
func allServerTypes() []MCPServerType {
	return []MCPServerType{
		ServerTypeBuiltin,
		ServerTypeStdio,
		ServerTypeSSE,
		ServerTypeHTTP,
		ServerTypeSource,
	}
}

// registryManagedServerTypes returns the types the registry API owns. SQL
// filters use it so list reads never expose in-process rows as user-editable
// servers without a second, hand-maintained exclusion list.
func registryManagedServerTypes() []MCPServerType {
	types := make([]MCPServerType, 0, len(allServerTypes()))
	for _, t := range allServerTypes() {
		if t.contract().registryManaged {
			types = append(types, t)
		}
	}
	return types
}

// inProcessServerTypes returns the types served by mcp.Service in process.
func inProcessServerTypes() []MCPServerType {
	types := make([]MCPServerType, 0, len(allServerTypes()))
	for _, t := range allServerTypes() {
		if t.contract().inProcess {
			types = append(types, t)
		}
	}
	return types
}

// listedServerTypes returns the types the registry list reads expose: the
// external types plus the read-only builtin catalogue, but never source rows.
func listedServerTypes() []MCPServerType {
	types := make([]MCPServerType, 0, len(allServerTypes()))
	for _, t := range allServerTypes() {
		if t.contract().listed {
			types = append(types, t)
		}
	}
	return types
}
