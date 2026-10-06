package mcpregistry

import "errors"

// ErrToolNotFound is returned when a tool lookup is scoped to a project and no
// tool with the given ID belongs to that project. Handlers map it to 404 so a
// foreign (or missing) tool is indistinguishable from a non-existent one — the
// same cross-tenant convention as sandboximages.ErrNotFound (#968/#974).
var ErrToolNotFound = errors.New("tool not found")

// ErrServerNotFound is returned when a server lookup is scoped to a project and
// no server with the given ID belongs to that project. Handlers map it to 404 so
// a foreign (or missing) server is indistinguishable from a non-existent one —
// the same convention as ErrToolNotFound (#978).
var ErrServerNotFound = errors.New("server not found")

// ErrServerDisabled is returned when a tool call targets a disabled server.
// Handlers map it to 409 (Conflict).
var ErrServerDisabled = errors.New("server disabled")

// ErrToolDisabled is returned when a tool call targets a disabled tool row.
// Handlers map it to 409 (Conflict).
var ErrToolDisabled = errors.New("tool disabled")

// ErrToolForbidden is returned when the caller is not authorized to invoke a
// tool (agent-only, superadmin-only, or insufficient scope). Handlers map it to
// 403 (Forbidden).
var ErrToolForbidden = errors.New("tool forbidden")

// ErrUpstreamFailure wraps a connection/call failure from an external MCP
// server. Handlers map it to 502 (Bad Gateway).
var ErrUpstreamFailure = errors.New("MCP upstream failure")

// ErrDeviceServerNotOutbound is returned when an outbound server path (inspect,
// discover/sync, call) targets a device-targeted row. Device rows are delivered
// over the relay, never connected outbound by the server.
var ErrDeviceServerNotOutbound = errors.New("device-targeted MCP server is not connected outbound by the server")

// ErrDeviceTargetForbidden is returned when a caller is not the target device's
// owner (or a project admin) for a device-targeted write.
var ErrDeviceTargetForbidden = errors.New("not authorized to target this device")
