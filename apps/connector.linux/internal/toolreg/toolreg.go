// Package toolreg implements a registry of MCP tools that a connector
// instance registers with the relay hub and serves on relayed tools/call
// requests.
package toolreg

import (
	"context"
	"fmt"
	"sync"
)

// Handler executes a tool invocation with validated arguments and returns an
// MCP-shaped result map.
type Handler func(ctx context.Context, args map[string]any) (map[string]any, error)

// Tool is one MCP tool: its identity, JSON-schema input, and local handler.
type Tool struct {
	Name        string
	Description string
	InputSchema map[string]any
	Handler     Handler
}

// Registry holds tools in registration order, keyed by name. Tools can be
// disabled after registration: a disabled tool stays resolvable by Lookup so
// dispatch rejects calls to it with a clear error, but it is excluded from
// List and ToolsListPayload (never registered with the hub).
type Registry struct {
	mu       sync.RWMutex
	tools    map[string]Tool
	order    []string
	disabled map[string]struct{}
}

// New returns an empty registry.
func New() *Registry {
	return &Registry{tools: make(map[string]Tool), disabled: make(map[string]struct{})}
}

// Register adds t. Empty names, nil handlers, and duplicate names are errors.
func (r *Registry) Register(t Tool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if t.Name == "" {
		return fmt.Errorf("register tool: name must not be empty")
	}
	if t.Handler == nil {
		return fmt.Errorf("register tool %q: handler must not be nil", t.Name)
	}
	if _, ok := r.tools[t.Name]; ok {
		return fmt.Errorf("register tool %q: already registered", t.Name)
	}
	r.tools[t.Name] = t
	r.order = append(r.order, t.Name)
	return nil
}

// MarkDisabled disables the registered tool name: its handler is replaced by a
// rejecting stub (dispatch returns an error naming the tool as disabled) and
// the tool is dropped from List/ToolsListPayload. Names that are not
// registered are ignored — a stale disabled_tools entry must never fail.
func (r *Registry) MarkDisabled(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.tools[name]; !ok {
		return
	}
	if _, ok := r.disabled[name]; ok {
		return
	}
	t := r.tools[name]
	t.Handler = disabledToolHandler(name)
	r.tools[name] = t
	r.disabled[name] = struct{}{}
}

// disabledToolHandler rejects calls to a disabled tool, naming it.
func disabledToolHandler(name string) Handler {
	return func(context.Context, map[string]any) (map[string]any, error) {
		return nil, fmt.Errorf("tool %q is disabled", name)
	}
}

// List returns the registered (non-disabled) tools in registration order.
func (r *Registry) List() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Tool, 0, len(r.order))
	for _, name := range r.order {
		if _, ok := r.disabled[name]; ok {
			continue
		}
		out = append(out, r.tools[name])
	}
	return out
}

// Lookup returns the tool registered under name, including disabled tools (so
// dispatch can answer with the disabled error from the swapped handler).
func (r *Registry) Lookup(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// FilterDisabled returns tools with registration order preserved, minus any
// whose Name appears in disabled. Names in disabled that match no tool are
// ignored (unknown names must not fail configuration).
func FilterDisabled(tools []Tool, disabled []string) []Tool {
	if len(disabled) == 0 {
		return tools
	}
	drop := make(map[string]struct{}, len(disabled))
	for _, name := range disabled {
		drop[name] = struct{}{}
	}
	out := make([]Tool, 0, len(tools))
	for _, tool := range tools {
		if _, ok := drop[tool.Name]; ok {
			continue
		}
		out = append(out, tool)
	}
	return out
}

// ToolsListPayload returns the nested shape the hub re-serves for a session:
// {"tools":[{name,description,inputSchema}, ...]}. Registration order and
// metadata only; handlers are not serialized. Disabled tools are excluded.
func (r *Registry) ToolsListPayload() map[string]any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]map[string]any, 0, len(r.order))
	for _, name := range r.order {
		if _, ok := r.disabled[name]; ok {
			continue
		}
		t := r.tools[name]
		items = append(items, map[string]any{
			"name":        t.Name,
			"description": t.Description,
			"inputSchema": t.InputSchema,
		})
	}
	return map[string]any{"tools": items}
}
