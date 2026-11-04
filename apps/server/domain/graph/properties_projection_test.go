package graph

import (
	"reflect"
	"testing"
)

func TestProjectProperties(t *testing.T) {
	props := map[string]any{
		"title":   "t",
		"content": "big",
		"tier":    3,
	}

	tests := []struct {
		name       string
		projection *GraphExpandProjection
		want       map[string]any
	}{
		{
			name:       "nil projection returns input unchanged",
			projection: nil,
			want:       props,
		},
		{
			name:       "no include or exclude returns input unchanged",
			projection: &GraphExpandProjection{},
			want:       props,
		},
		{
			name:       "exclude drops the named key",
			projection: &GraphExpandProjection{ExcludeObjectProperties: []string{"content"}},
			want:       map[string]any{"title": "t", "tier": 3},
		},
		{
			name:       "include whitelists keys",
			projection: &GraphExpandProjection{IncludeObjectProperties: []string{"title"}},
			want:       map[string]any{"title": "t"},
		},
		{
			name: "include then exclude composes",
			projection: &GraphExpandProjection{
				IncludeObjectProperties: []string{"title", "content"},
				ExcludeObjectProperties: []string{"content"},
			},
			want: map[string]any{"title": "t"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := projectProperties(props, tt.projection)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("projectProperties() = %#v, want %#v", got, tt.want)
			}
		})
	}
}
