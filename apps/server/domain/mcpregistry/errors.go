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
