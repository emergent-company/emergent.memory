package agents

import (
	"context"
	"fmt"
	"reflect"
	"strings"
)

// ReactionMatchesProperties evaluates the value predicates in a reaction
// config against the current HEAD properties of an object. An empty Match list
// matches every object; every listed predicate must match.
func ReactionMatchesProperties(rc *ReactionConfig, properties map[string]any) bool {
	if rc == nil || len(rc.Match) == 0 {
		return true
	}
	for _, match := range rc.Match {
		value, ok := propertyAt(properties, match.Path)
		if !propertyMatch(value, ok, match) {
			return false
		}
	}
	return true
}

func propertyAt(properties map[string]any, path string) (any, bool) {
	path = strings.TrimSpace(path)
	if path == "" || path == "/" {
		return properties, properties != nil
	}
	// modelref:allow — this splits a JSON-pointer path into property segments,
	// not a routed model name.
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	var cur any = properties
	for _, part := range parts {
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[part]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

func propertyMatch(value any, found bool, match ReactionPropertyMatch) bool {
	op := strings.ToLower(strings.TrimSpace(match.Operator))
	switch op {
	case "", "equals", "eq":
		return found && valuesEqual(value, match.Value)
	case "contains":
		return found && valueContains(value, match.Value)
	case "exists":
		return found && value != nil
	default:
		return false
	}
}

func valuesEqual(a, b any) bool {
	if reflect.DeepEqual(a, b) {
		return true
	}
	return fmt.Sprint(a) == fmt.Sprint(b)
}

func valueContains(value, want any) bool {
	switch v := value.(type) {
	case string:
		return strings.Contains(v, fmt.Sprint(want))
	case []string:
		for _, item := range v {
			if valuesEqual(item, want) {
				return true
			}
		}
	case []any:
		for _, item := range v {
			if valuesEqual(item, want) {
				return true
			}
		}
	}
	return false
}

// FindReviewerAgentsForObject returns enabled reviewer agents whose object type
// matches and whose value predicates accept the supplied HEAD properties.
// Reviewer agents with no Match are the default reviewers.
func (r *Repository) FindReviewerAgentsForObject(ctx context.Context, projectID, objectType string, properties map[string]any) ([]*Agent, error) {
	agents, err := r.FindEnabledByTriggerType(ctx, TriggerTypeReaction)
	if err != nil {
		return nil, err
	}
	out := make([]*Agent, 0)
	for _, agent := range agents {
		if agent == nil || agent.ProjectID != projectID || agent.ReactionConfig == nil || !agent.ReactionConfig.Reviewer {
			continue
		}
		if !reactionMatchesType(agent.ReactionConfig, objectType) {
			continue
		}
		if !ReactionMatchesProperties(agent.ReactionConfig, properties) {
			continue
		}
		out = append(out, agent)
	}
	return out, nil
}
