package mcp

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

// ObjectWriteRule is the per-object-type write policy derived from an agent
// definition's work rules. It mirrors the subset the tool layer can enforce:
// create/update/delete flags and a writable-property allowlist.
type ObjectWriteRule struct {
	CanCreate bool
	CanUpdate bool
	CanDelete bool
	// Fields is the writable-property allowlist. Empty means all fields.
	Fields []string
}

// AgentWriteRules is a per-run map of object-type write policies, threaded into
// the tool layer the same way TypeScope is. A nil rules value, or a missing
// type, means unrestricted.
type AgentWriteRules struct {
	byType map[string]ObjectWriteRule
}

// NewAgentWriteRules builds the per-run policy map. A nil/empty map yields nil
// (unrestricted).
func NewAgentWriteRules(byType map[string]ObjectWriteRule) *AgentWriteRules {
	if len(byType) == 0 {
		return nil
	}
	return &AgentWriteRules{byType: byType}
}

// ruleFor returns the type's rule and whether one exists.
func (r *AgentWriteRules) ruleFor(objectType string) (ObjectWriteRule, bool) {
	if r == nil {
		return ObjectWriteRule{}, false
	}
	rule, ok := r.byType[objectType]
	return rule, ok
}

// CreateAllowed reports whether the agent may create an object of the type.
func (r *AgentWriteRules) CreateAllowed(objectType string) bool {
	rule, ok := r.ruleFor(objectType)
	return !ok || rule.CanCreate
}

// UpdateAllowed reports whether the agent may update an object of the type.
func (r *AgentWriteRules) UpdateAllowed(objectType string) bool {
	rule, ok := r.ruleFor(objectType)
	return !ok || rule.CanUpdate
}

// DeleteAllowed reports whether the agent may delete an object of the type.
func (r *AgentWriteRules) DeleteAllowed(objectType string) bool {
	rule, ok := r.ruleFor(objectType)
	return !ok || rule.CanDelete
}

// CheckFields rejects a write carrying properties outside the type's allowlist.
// A missing rule or an empty allowlist permits every field.
func (r *AgentWriteRules) CheckFields(objectType string, properties map[string]any) error {
	rule, ok := r.ruleFor(objectType)
	if !ok || len(rule.Fields) == 0 {
		return nil
	}
	var denied []string
	for name := range properties {
		if !slices.Contains(rule.Fields, name) {
			denied = append(denied, name)
		}
	}
	if len(denied) == 0 {
		return nil
	}
	slices.Sort(denied)
	return fmt.Errorf("%s: fields not writable by this agent: %s (allowed: %s)",
		objectType, strings.Join(denied, ", "), strings.Join(rule.Fields, ", "))
}

// agentWriteRulesCtxKey is the context key for the per-run write rules.
type agentWriteRulesCtxKey struct{}

// ContextWithAgentWriteRules returns a context carrying the per-run rules. A
// nil value is a no-op so the unrestricted default is preserved.
func ContextWithAgentWriteRules(ctx context.Context, rules *AgentWriteRules) context.Context {
	if rules == nil {
		return ctx
	}
	return context.WithValue(ctx, agentWriteRulesCtxKey{}, rules)
}

// AgentWriteRulesFromContext extracts the per-run write rules (nil when unset).
func AgentWriteRulesFromContext(ctx context.Context) *AgentWriteRules {
	if ctx == nil {
		return nil
	}
	rules, _ := ctx.Value(agentWriteRulesCtxKey{}).(*AgentWriteRules)
	return rules
}
