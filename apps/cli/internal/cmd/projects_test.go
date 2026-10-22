package cmd

import (
	"encoding/json"
	"testing"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/projects"
	"github.com/stretchr/testify/assert"
)

func TestIsUUID(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "valid UUID v4",
			input:    "550e8400-e29b-41d4-a716-446655440000",
			expected: true,
		},
		{
			name:     "valid UUID lowercase",
			input:    "123e4567-e89b-12d3-a456-426614174000",
			expected: true,
		},
		{
			name:     "valid UUID uppercase",
			input:    "123E4567-E89B-12D3-A456-426614174000",
			expected: true,
		},
		{
			name:     "valid UUID mixed case",
			input:    "123e4567-E89B-12d3-A456-426614174000",
			expected: true,
		},
		{
			name:     "invalid - too short",
			input:    "550e8400-e29b-41d4-a716",
			expected: false,
		},
		{
			name:     "invalid - too long",
			input:    "550e8400-e29b-41d4-a716-446655440000-extra",
			expected: false,
		},
		{
			name:     "invalid - missing hyphens",
			input:    "550e8400e29b41d4a716446655440000",
			expected: false,
		},
		{
			name:     "invalid - wrong hyphen positions",
			input:    "550e8400-e29b41d4-a716-446655440000",
			expected: false,
		},
		{
			name:     "invalid - hyphen at position 0",
			input:    "-50e8400-e29b-41d4-a716-446655440000",
			expected: false,
		},
		{
			name:     "invalid - contains non-hex chars (g)",
			input:    "550e8400-e29b-41d4-a716-44665544000g",
			expected: false,
		},
		{
			name:     "invalid - contains non-hex chars (z)",
			input:    "550e8400-e29b-41d4-a716-4466554400z0",
			expected: false,
		},
		{
			name:     "project name",
			input:    "My Project",
			expected: false,
		},
		{
			name:     "empty string",
			input:    "",
			expected: false,
		},
		{
			name:     "single word",
			input:    "Production",
			expected: false,
		},
		{
			name:     "number string",
			input:    "12345",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isUUID(tt.input)
			assert.Equal(t, tt.expected, result, "isUUID(%q) should be %v", tt.input, tt.expected)
		})
	}
}

// TestProjectJSONAlwaysEmitsDocumentedFields pins the --json contract (#2065):
// a project whose optional fields were never set must still produce every
// documented key, with null values. The SDK type alone cannot do this — its
// omitempty tags drop them.
func TestProjectJSONAlwaysEmitsDocumentedFields(t *testing.T) {
	p := projects.Project{
		ID:    "11111111-1111-1111-1111-111111111111",
		Name:  "fresh",
		OrgID: "0aaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
	}

	b, err := json.Marshal(newProjectJSON(p))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got map[string]json.RawMessage
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	for _, key := range []string{
		"id", "name", "orgId",
		"project_info", "chat_prompt_template", "auto_extract_objects",
		"auto_extract_config", "budget_usd", "budget_alert_threshold",
	} {
		if _, ok := got[key]; !ok {
			t.Errorf("--json output is missing %q (keys are not schema-stable)", key)
		}
	}

	for _, key := range []string{
		"project_info", "chat_prompt_template", "auto_extract_objects",
		"auto_extract_config", "budget_usd", "budget_alert_threshold",
	} {
		if string(got[key]) != "null" {
			t.Errorf("%q = %s, want null for an unset field", key, got[key])
		}
	}

	// stats is flag-gated: --stats fetches it, every other field is unconditional.
	if _, ok := got["stats"]; ok {
		t.Error("stats must be omitted from --json unless --stats was passed")
	}
}

func TestProjectJSONPreservesSetValuesAndStats(t *testing.T) {
	info := "project brief"
	budget := 12.5
	threshold := 80.0
	autoExtract := true

	p := projects.Project{
		ID:                   "11111111-1111-1111-1111-111111111111",
		Name:                 "configured",
		OrgID:                "0aaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		ProjectInfo:          &info,
		BudgetUSD:            &budget,
		BudgetAlertThreshold: &threshold,
		AutoExtractObjects:   &autoExtract,
		Stats:                &projects.ProjectStats{DocumentCount: 3},
	}

	b, err := json.Marshal(newProjectJSON(p))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got map[string]json.RawMessage
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	assert.Equal(t, `"project brief"`, string(got["project_info"]))
	assert.Equal(t, "12.5", string(got["budget_usd"]))
	assert.Equal(t, "80", string(got["budget_alert_threshold"]))
	assert.Equal(t, "true", string(got["auto_extract_objects"]))
	if _, ok := got["stats"]; !ok {
		t.Error("stats must be present when the caller fetched it with --stats")
	}
}

// TestProjectJSONListEmptyIsArray keeps `--json` emitting [] rather than null
// when there are no projects to show.
func TestProjectJSONListEmptyIsArray(t *testing.T) {
	b, err := json.Marshal(newProjectJSONList(nil))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	assert.Equal(t, "[]", string(b))
}
