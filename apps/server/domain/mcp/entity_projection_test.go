package mcp

import "testing"

func TestEntityPropertiesProjection(t *testing.T) {
	tests := []struct {
		name     string
		strategy string
		fields   any
		want     string
	}{
		{
			name:     "compact omits properties",
			strategy: "compact",
			want:     "'{}'::jsonb",
		},
		{
			name:     "minimal omits properties even with fields",
			strategy: "minimal",
			fields:   []any{"method", "path"},
			want:     "'{}'::jsonb",
		},
		{
			name:     "empty strategy omits properties",
			strategy: "",
			want:     "'{}'::jsonb",
		},
		{
			name:     "full with no fields returns full map",
			strategy: "full",
			want:     "go.properties",
		},
		{
			name:     "full with fields builds jsonb_build_object with name",
			strategy: "full",
			fields:   []any{"method", "path"},
			want:     "jsonb_build_object('name', go.properties->'name', 'method', go.properties->'method', 'path', go.properties->'path')",
		},
		{
			name:     "full with name field keeps single name",
			strategy: "full",
			fields:   []any{"name"},
			want:     "jsonb_build_object('name', go.properties->'name')",
		},
		{
			name:     "full with string slice fields",
			strategy: "full",
			fields:   []string{"method"},
			want:     "jsonb_build_object('name', go.properties->'name', 'method', go.properties->'method')",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := entityPropertiesProjection(tc.strategy, tc.fields); got != tc.want {
				t.Errorf("entityPropertiesProjection(%q, %v) = %q, want %q", tc.strategy, tc.fields, got, tc.want)
			}
		})
	}
}
