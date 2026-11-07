// Package branchscope is the per-agent-run read boundary for unmerged graph
// branches (issue #265).
//
// An agent definition may declare that its runs read not only the main graph but
// also branches that have not been merged yet, narrowed by why a branch exists
// (`origin`, immutable) and by branch name (mutable, an escape hatch). The scope
// is derived once per run from the definition and threaded through
// context.Context, exactly like the object-type scope, so every execution
// surface resolves it the same way and no tool argument can widen it.
//
// Two invariants keep the feature safe:
//
//   - The default is main only. An absent or empty configuration yields a nil
//     Scope, and every consumer treats nil as "main only", reproducing the
//     previous behaviour byte for byte.
//   - The scope is read-only. Nothing here grants a write; write paths do not
//     consult it. An agent that can read a reviewer's pending branch must not be
//     able to change what that reviewer is about to merge.
//
// A Scope is immutable once built; Narrow returns a new Scope so a delegated
// child can never widen its caller's boundary.
package branchscope

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/emergent-company/emergent.memory/pkg/branchorigin"
)

// DefaultMaxBranches bounds how many branches one scoped read may fan out to
// when the definition does not say. Reading is per-branch, so this is also the
// bound on query count for one search.
const DefaultMaxBranches = 3

// MaxMaxBranches is the hard ceiling a definition may request. It exists so a
// typo cannot turn one search into a hundred queries.
const MaxMaxBranches = 20

// Config is the JSON shape stored under AgentDefinition.Config["branchScope"] and
// accepted by the agent-definition API. Origin values are the four stored ones
// (see pkg/branchorigin); Branches entries are glob patterns where only `*` is
// special and it crosses `/`, so `extraction/*` matches
// `extraction/1413e4cf/20fa1f14`.
type Config struct {
	// Unmerged enables reads from unmerged branches. False/absent means main
	// only and is the default.
	Unmerged bool `json:"unmerged"`
	// Origins narrows the branches by why they exist. Empty means any origin.
	Origins []string `json:"origins,omitempty"`
	// Branches narrows by name glob. Empty means any name.
	Branches []string `json:"branches,omitempty"`
	// MaxBranches caps how many branches one read fans out to. Zero means
	// DefaultMaxBranches.
	MaxBranches int `json:"maxBranches,omitempty"`
}

// Scope is a validated, immutable branch read boundary. A nil *Scope means main
// only; there is no "unrestricted" state beyond the configured origins/globs,
// because every branch is read the same way regardless of origin.
type Scope struct {
	unmerged bool

	origins           map[string]bool
	originsRestricted bool

	globs           []string
	globsRestricted bool

	maxBranches int
}

// New validates a configuration and returns the scope it describes. It returns
// (nil, nil) when the configuration does not enable unmerged reads, so callers
// can store nil — the main-only default — without special-casing. An invalid
// origin, a blank glob, or an out-of-range MaxBranches is an error rather than a
// silently narrowed scope: a typo must not hide data the user asked for.
func New(cfg Config) (*Scope, error) {
	if !cfg.Unmerged {
		return nil, nil
	}

	origins := make(map[string]bool, len(cfg.Origins))
	for _, raw := range cfg.Origins {
		o := strings.ToLower(strings.TrimSpace(raw))
		if o == "" {
			continue
		}
		if !branchorigin.Valid(o) {
			return nil, fmt.Errorf("branchScope.origins: %q is not one of extraction, agent, manual, system", raw)
		}
		origins[o] = true
	}

	globs := make([]string, 0, len(cfg.Branches))
	for _, raw := range cfg.Branches {
		g := strings.TrimSpace(raw)
		if g == "" {
			return nil, fmt.Errorf("branchScope.branches: blank name pattern")
		}
		globs = append(globs, g)
	}

	maxBranches := cfg.MaxBranches
	if maxBranches == 0 {
		maxBranches = DefaultMaxBranches
	}
	if maxBranches < 1 || maxBranches > MaxMaxBranches {
		return nil, fmt.Errorf("branchScope.maxBranches: must be between 1 and %d", MaxMaxBranches)
	}

	return &Scope{
		unmerged:          true,
		origins:           origins,
		originsRestricted: len(origins) > 0,
		globs:             globs,
		globsRestricted:   len(globs) > 0,
		maxBranches:       maxBranches,
	}, nil
}

// IsMainOnly reports whether the scope reads the main graph only. A nil scope is
// main only, which is the default and the backwards-compatible behaviour.
func (s *Scope) IsMainOnly() bool {
	return s == nil || !s.unmerged
}

// Origins returns the sorted origin allowlist (nil when unrestricted).
func (s *Scope) Origins() []string {
	if s == nil || !s.originsRestricted {
		return nil
	}
	out := make([]string, 0, len(s.origins))
	for o := range s.origins {
		out = append(out, o)
	}
	sort.Strings(out)
	return out
}

// BranchPatterns returns the configured name globs (nil when unrestricted).
func (s *Scope) BranchPatterns() []string {
	if s == nil || !s.globsRestricted {
		return nil
	}
	return append([]string(nil), s.globs...)
}

// MaxBranches returns the fan-out cap. A nil scope returns 0; callers only use it
// for a non-main-only scope.
func (s *Scope) MaxBranches() int {
	if s == nil {
		return 0
	}
	return s.maxBranches
}

// AllowsOrigin reports whether a branch with the given stored origin is in
// scope. An empty origin reads as manual, matching ToResponse's normalisation.
func (s *Scope) AllowsOrigin(origin string) bool {
	if s == nil {
		return false
	}
	if !s.originsRestricted {
		return true
	}
	origin = strings.ToLower(strings.TrimSpace(origin))
	if origin == "" {
		origin = branchorigin.Manual
	}
	return s.origins[origin]
}

// AllowsName reports whether a branch name matches the configured globs. A glob
// only gives `*` special meaning and `*` crosses `/`, because branch names are
// slash-delimited paths and a caller writing `extraction/*` means the subtree.
func (s *Scope) AllowsName(name string) bool {
	if s == nil {
		return false
	}
	if !s.globsRestricted {
		return true
	}
	for _, g := range s.globs {
		if GlobMatch(g, name) {
			return true
		}
	}
	return false
}

// GlobMatch reports whether name matches pattern, where `*` matches any
// sequence of characters (including none, including `/`) and every other
// character is literal. It is deliberately smaller than path.Match: a branch
// name is a single string, not a filesystem path, so a `*` that stopped at `/`
// would surprise the caller who wrote `extraction/*`.
func GlobMatch(pattern, name string) bool {
	if pattern == "*" {
		return true
	}
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return pattern == name
	}
	if !strings.HasPrefix(name, parts[0]) {
		return false
	}
	rest := name[len(parts[0]):]
	for i := 1; i < len(parts)-1; i++ {
		idx := strings.Index(rest, parts[i])
		if idx < 0 {
			return false
		}
		rest = rest[idx+len(parts[i]):]
	}
	return strings.HasSuffix(rest, parts[len(parts)-1])
}

// Narrow returns the intersection of an inherited scope and a declared one, so a
// delegated run can never see more than its caller permits. A nil inherited
// scope is main only, and main-only on either side yields main only — a parent
// that cannot read branches must not gain that ability by delegating.
//
// Origins intersect exactly. Globs are only kept when the declared pattern is
// provably covered by an inherited pattern, so the rule is conservative: it may
// drop a branch that both could have read, never keep one only the child could
// have read. A restricted axis that intersects to nothing stays restricted and
// matches nothing, rather than becoming unrestricted.
func Narrow(inherited, declared *Scope) *Scope {
	if inherited.IsMainOnly() || declared.IsMainOnly() {
		return nil
	}

	out := &Scope{
		unmerged:    true,
		maxBranches: min(inherited.maxBranches, declared.maxBranches),
	}

	// Origins: restricted when either side is, intersect when both are.
	out.originsRestricted = inherited.originsRestricted || declared.originsRestricted
	if out.originsRestricted {
		out.origins = map[string]bool{}
		switch {
		case inherited.originsRestricted && declared.originsRestricted:
			for o := range declared.origins {
				if inherited.origins[o] {
					out.origins[o] = true
				}
			}
		case inherited.originsRestricted:
			out.origins = inherited.origins
		default:
			out.origins = declared.origins
		}
	}

	// Globs: restricted when either side is; keep only declared patterns the
	// inherited side provably covers (or vice versa when only one restricts).
	out.globsRestricted = inherited.globsRestricted || declared.globsRestricted
	switch {
	case inherited.globsRestricted && declared.globsRestricted:
		for _, g := range declared.globs {
			for _, parent := range inherited.globs {
				if GlobCovers(parent, g) {
					out.globs = append(out.globs, g)
					break
				}
			}
		}
	case inherited.globsRestricted:
		out.globs = inherited.globs
	default:
		out.globs = declared.globs
	}

	return out
}

// GlobCovers reports whether every name matched by child is also matched by
// parent, for the conservative subset of cases that can be decided structurally:
// an exact pattern, a parent of `*`, and a parent with a `*` suffix that is a
// prefix of the child. Anything else is "not covered" — dropping a branch is
// always safe during narrowing, keeping one that widens is not.
func GlobCovers(parent, child string) bool {
	if parent == child || parent == "*" {
		return true
	}
	if strings.HasSuffix(parent, "*") && !strings.Contains(strings.TrimSuffix(parent, "*"), "*") {
		return strings.HasPrefix(child, strings.TrimSuffix(parent, "*"))
	}
	return false
}

// branchScopeCtxKey is the context key for the per-run Scope. It is unexported
// so the only way to install or read a scope is through this package, which keeps
// the MCP facade and the search layer on the same key.
type branchScopeCtxKey struct{}

// ContextWith returns a context carrying the scope. A nil scope is a no-op so
// the main-only default is preserved without a value lookup.
func ContextWith(ctx context.Context, scope *Scope) context.Context {
	if scope == nil || scope.IsMainOnly() {
		return ctx
	}
	return context.WithValue(ctx, branchScopeCtxKey{}, scope)
}

// FromContext returns the scope installed by ContextWith, or nil (main only).
func FromContext(ctx context.Context) *Scope {
	if scope, ok := ctx.Value(branchScopeCtxKey{}).(*Scope); ok {
		return scope
	}
	return nil
}
