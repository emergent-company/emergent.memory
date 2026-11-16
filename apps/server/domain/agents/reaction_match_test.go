package agents

import "testing"

func TestReactionMatchesProperties(t *testing.T) {
	props := map[string]any{
		"labels":   []any{"bug", "review:ui"},
		"head_sha": "abc",
		"draft":    false,
		"nested":   map[string]any{"kind": "pull_request"},
	}
	tests := []struct {
		name  string
		match []ReactionPropertyMatch
		ok    bool
	}{
		{"empty", nil, true},
		{"contains label", []ReactionPropertyMatch{{Path: "/labels", Operator: "contains", Value: "review:ui"}}, true},
		{"missing label", []ReactionPropertyMatch{{Path: "/labels", Operator: "contains", Value: "review:security"}}, false},
		{"equals string", []ReactionPropertyMatch{{Path: "/head_sha", Operator: "equals", Value: "abc"}}, true},
		{"equals false", []ReactionPropertyMatch{{Path: "/draft", Operator: "equals", Value: false}}, true},
		{"exists", []ReactionPropertyMatch{{Path: "/nested/kind", Operator: "exists"}}, true},
		{"missing exists", []ReactionPropertyMatch{{Path: "/nested/missing", Operator: "exists"}}, false},
		{"all predicates", []ReactionPropertyMatch{
			{Path: "/labels", Operator: "contains", Value: "review:ui"},
			{Path: "/head_sha", Operator: "equals", Value: "abc"},
		}, true},
		{"one fails", []ReactionPropertyMatch{
			{Path: "/labels", Operator: "contains", Value: "review:ui"},
			{Path: "/head_sha", Operator: "equals", Value: "other"},
		}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rc := &ReactionConfig{Match: tc.match}
			if got := ReactionMatchesProperties(rc, props); got != tc.ok {
				t.Fatalf("match = %v, want %v", got, tc.ok)
			}
		})
	}
}
