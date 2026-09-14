package toolreg

import "sync/atomic"

// Reloadable is a relay tool registry whose backing Registry can be swapped at
// runtime. It implements the relay.ToolRegistry interface (Lookup and
// ToolsListPayload), so the relay client can keep a single reference while a
// config reload replaces the whole tool set — including the clients behind
// hosted MCP tools — without reconnecting.
type Reloadable struct {
	ptr atomic.Pointer[Registry]
}

// NewReloadable wraps r. A nil r yields an empty registry view.
func NewReloadable(r *Registry) *Reloadable {
	rr := &Reloadable{}
	rr.ptr.Store(r)
	return rr
}

// Swap atomically replaces the backing registry and returns the previous one.
func (rr *Reloadable) Swap(r *Registry) *Registry {
	return rr.ptr.Swap(r)
}

// Current returns the current registry, or nil.
func (rr *Reloadable) Current() *Registry {
	return rr.ptr.Load()
}

// Lookup implements relay.ToolRegistry.
func (rr *Reloadable) Lookup(name string) (Tool, bool) {
	r := rr.ptr.Load()
	if r == nil {
		return Tool{}, false
	}
	return r.Lookup(name)
}

// ToolsListPayload implements relay.ToolRegistry. With no backing registry it
// returns an empty (but well-formed) payload so registration never panics.
func (rr *Reloadable) ToolsListPayload() map[string]any {
	r := rr.ptr.Load()
	if r == nil {
		return map[string]any{"tools": []map[string]any{}}
	}
	return r.ToolsListPayload()
}
