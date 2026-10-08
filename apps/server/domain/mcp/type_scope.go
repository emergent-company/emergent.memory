package mcp

import (
	"context"
	"sort"
	"strings"
)

// TypeScope is a per-agent-run allowlist of graph object types and relationship
// types. It is derived once per run from the agent definition's config
// (`objectTypes` / `relationshipTypes`) and threaded into the tool layer via
// context (see ContextWithTypeScope), mirroring how the graph namespace is
// threaded (auth.ContextWithNamespace). Every graph-touching MCP tool reads it
// before returning data so an excluded type is never offered or returned.
//
// Semantics (issue #1695):
//   - a nil TypeScope, or a scope with no restriction for a given axis, means
//     "all types are allowed" — the backwards-compatible default;
//   - an empty allowlist for an axis means that axis is unrestricted;
//   - a non-empty allowlist restricts that axis to exactly the listed names.
//
// Object and relationship allowlists are independent: setting only
// relationshipTypes leaves object types unrestricted.
type TypeScope struct {
	objectTypes       map[string]bool
	relationshipTypes map[string]bool
}

// NewTypeScope builds a TypeScope from the agent definition's allowlists. It
// returns nil when neither axis is restricted, so callers can store a nil scope
// and get the unrestricted default with no allocation.
func NewTypeScope(objectTypes, relationshipTypes []string) *TypeScope {
	objs := normalizeScopeList(objectTypes)
	rels := normalizeScopeList(relationshipTypes)
	if len(objs) == 0 && len(rels) == 0 {
		return nil
	}
	return &TypeScope{objectTypes: objs, relationshipTypes: rels}
}

// normalizeScopeList trims, drops blanks, de-duplicates and sorts a configured
// allowlist. It always returns a non-nil map (possibly empty) so presence of a
// restriction is unambiguous, and sorting makes the resulting scope
// deterministic for tests and logs.
func normalizeScopeList(types []string) map[string]bool {
	if len(types) == 0 {
		return nil
	}
	out := make(map[string]bool, len(types))
	for _, t := range types {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		out[t] = true
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// scopeTypeNames returns the sorted names of an allowlist map (nil for empty).
func scopeTypeNames(m map[string]bool) []string {
	if len(m) == 0 {
		return nil
	}
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ObjectTypes returns the sorted object-type allowlist, or nil when object types
// are unrestricted.
func (s *TypeScope) ObjectTypes() []string { return scopeTypeNames(s.objectTypes) }

// RelationshipTypes returns the sorted relationship-type allowlist, or nil when
// relationship types are unrestricted.
func (s *TypeScope) RelationshipTypes() []string { return scopeTypeNames(s.relationshipTypes) }

// HasObjectRestriction reports whether this scope restricts object types.
func (s *TypeScope) HasObjectRestriction() bool {
	return s != nil && len(s.objectTypes) > 0
}

// HasRelationshipRestriction reports whether this scope restricts relationship
// types.
func (s *TypeScope) HasRelationshipRestriction() bool {
	return s != nil && len(s.relationshipTypes) > 0
}

// AllowsObject reports whether an object type is permitted. A nil scope or an
// unrestricted object axis allows everything.
func (s *TypeScope) AllowsObject(typeName string) bool {
	if !s.HasObjectRestriction() {
		return true
	}
	return s.objectTypes[typeName]
}

// AllowsRelationship reports whether a relationship type is permitted. A nil
// scope or an unrestricted relationship axis allows everything.
func (s *TypeScope) AllowsRelationship(typeName string) bool {
	if !s.HasRelationshipRestriction() {
		return true
	}
	return s.relationshipTypes[typeName]
}

// FilterRequestedObjectTypes narrows a tool call's requested object types to the
// scope. It returns (filtered, denied):
//
//   - nil/unrestricted scope: returns (requested, false) unchanged;
//   - requested is empty (caller asked for "all"): returns (allowlist, false) so
//     an unrestricted request is narrowed to the permitted set;
//   - requested names types but none are permitted: returns (nil, true) — the
//     caller MUST short-circuit and return an empty result rather than query,
//     because an empty `types` argument to the search layer means "all types"
//     and would otherwise leak excluded types.
func (s *TypeScope) FilterRequestedObjectTypes(requested []string) (filtered []string, denied bool) {
	if !s.HasObjectRestriction() {
		return requested, false
	}
	if len(requested) == 0 {
		return s.ObjectTypes(), false
	}
	out := make([]string, 0, len(requested))
	for _, t := range requested {
		if s.objectTypes[t] && !containsScopeString(out, t) {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return nil, true
	}
	return out, false
}

// FilterRequestedRelationshipTypes is the relationship-type analogue of
// FilterRequestedObjectTypes. An empty requested list narrows to the allowlist;
// a request naming only excluded types is denied.
func (s *TypeScope) FilterRequestedRelationshipTypes(requested []string) (filtered []string, denied bool) {
	if !s.HasRelationshipRestriction() {
		return requested, false
	}
	if len(requested) == 0 {
		return s.RelationshipTypes(), false
	}
	out := make([]string, 0, len(requested))
	for _, t := range requested {
		if s.relationshipTypes[t] && !containsScopeString(out, t) {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return nil, true
	}
	return out, false
}

func containsScopeString(haystack []string, needle string) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}

// FilterObjectTypes returns the subset of object type names that are allowed by
// the scope, preserving input order and dropping excluded names. A nil or
// unrestricted scope returns the input unchanged (no allocation). This is the
// result-filter counterpart to FilterRequestedObjectTypes: it narrows an
// already-fetched list of type names rather than a tool call's requested set.
func (s *TypeScope) FilterObjectTypes(names []string) []string {
	if !s.HasObjectRestriction() {
		return names
	}
	return filterScopeNames(names, s.objectTypes)
}

// FilterRelationshipTypes returns the subset of relationship type names that are
// allowed by the scope, preserving input order and dropping excluded names. A
// nil or unrestricted scope returns the input unchanged (no allocation).
func (s *TypeScope) FilterRelationshipTypes(names []string) []string {
	if !s.HasRelationshipRestriction() {
		return names
	}
	return filterScopeNames(names, s.relationshipTypes)
}

// filterScopeNames keeps only the names present in the allowlist map, preserving
// input order. It always allocates, unlike Filter*Types which skip the
// unrestricted fast path.
func filterScopeNames(names []string, allowed map[string]bool) []string {
	if len(names) == 0 {
		return names
	}
	out := make([]string, 0, len(names))
	for _, n := range names {
		if allowed[n] {
			out = append(out, n)
		}
	}
	return out
}

// AllowsRelationshipEndpoint reports whether a relationship whose connected
// object has the given type may be surfaced: both the relationship type and the
// endpoint object type must be permitted on their respective axes. Used by
// edge/enrichment paths that return a relationship together with its target
// object, so an allowed relationship cannot leak an excluded object type.
func (s *TypeScope) AllowsRelationshipEndpoint(relType, endpointObjectType string) bool {
	return s.AllowsRelationship(relType) && s.AllowsObject(endpointObjectType)
}

// AllowsRelationshipRow reports whether a relationship type may be advertised
// given its from/to object types. entity-type-list surfaces from_type/to_type
// object names alongside the relationship type, so all three names must be
// permitted on their respective axes.
func (s *TypeScope) AllowsRelationshipRow(relType, fromType, toType string) bool {
	return s.AllowsRelationship(relType) && s.AllowsObject(fromType) && s.AllowsObject(toType)
}

// PermitsDelegation reports whether a delegated/child agent whose declared
// allowlists are objectTypes/relationshipTypes stays within this scope. It is
// the delegation guard: a scoped parent must not hand work to a child that would
// let the model reach a type the parent cannot (issue #1695).
//
// A nil/unrestricted parent permits anything. On an axis the parent restricts,
// the child must also restrict and its allowlist must be a subset of the
// parent's — an empty/nil child list means "all types" and therefore widens,
// which is refused. Object and relationship axes are checked independently.
func (s *TypeScope) PermitsDelegation(objectTypes, relationshipTypes []string) bool {
	if s == nil {
		return true
	}
	if s.HasObjectRestriction() && !scopeSubset(normalizeScopeList(objectTypes), s.objectTypes) {
		return false
	}
	if s.HasRelationshipRestriction() && !scopeSubset(normalizeScopeList(relationshipTypes), s.relationshipTypes) {
		return false
	}
	return true
}

// scopeSubset reports whether every name in child is present in parent. An empty
// child (nil or zero-length) is "unrestricted" and is therefore NOT a subset of a
// restricted parent — it would widen, so it returns false.
func scopeSubset(child, parent map[string]bool) bool {
	if len(child) == 0 {
		return false
	}
	for name := range child {
		if !parent[name] {
			return false
		}
	}
	return true
}

// typeScopeCtxKey is the context key for the per-run TypeScope.
type typeScopeCtxKey struct{}

// ContextWithTypeScope returns a new context carrying the per-run type scope.
// The executor sets it once per run in runPipeline so every graph-touching tool
// on every surface (trigger, A2A, chat, chat-completions) enforces the same
// allowlist. A nil scope is a no-op so the unrestricted default is preserved.
func ContextWithTypeScope(ctx context.Context, scope *TypeScope) context.Context {
	if scope == nil {
		return ctx
	}
	return context.WithValue(ctx, typeScopeCtxKey{}, scope)
}

// TypeScopeFromContext extracts the per-run type scope. It returns nil when no
// scope was set (HTTP MCP transports, tests), meaning all types are allowed.
func TypeScopeFromContext(ctx context.Context) *TypeScope {
	if scope, ok := ctx.Value(typeScopeCtxKey{}).(*TypeScope); ok {
		return scope
	}
	return nil
}
