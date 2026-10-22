package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/modelconfig"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/projects"
	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v3"
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

// ── show-models output contract (#2067) ───────────────────────────────────────

func TestRenderShowModels(t *testing.T) {
	full := &modelconfig.EffectiveModelConfig{
		GenerativeModel:       "litellm/deepseek-v4-flash",
		GenerativeModelSource: "project",
		EmbeddingModel:        "litellm/gemini-embedding-001",
		EmbeddingModelSource:  "provider",
	}

	tests := []struct {
		name    string
		format  string
		wantErr bool
		// check asserts the render actually produced *that* format: a format
		// that silently falls back to text would still be non-empty.
		check func(t *testing.T, out string)
	}{
		{
			name:   "table is the default",
			format: "table",
			check: func(t *testing.T, out string) {
				assert.Contains(t, out, "Effective model config for project proj-1")
				assert.Contains(t, out, "Generative:")
			},
		},
		{
			name:   "empty format renders table",
			format: "",
			check: func(t *testing.T, out string) {
				assert.Contains(t, out, "Effective model config for project proj-1")
			},
		},
		{
			name:   "json",
			format: "json",
			check: func(t *testing.T, out string) {
				var got map[string]any
				assert.NoError(t, json.Unmarshal([]byte(out), &got), "output must be valid JSON")
				assert.Equal(t, "proj-1", got["projectId"])
			},
		},
		{
			name:   "yaml",
			format: "yaml",
			check: func(t *testing.T, out string) {
				var got map[string]any
				assert.NoError(t, yaml.Unmarshal([]byte(out), &got), "output must be valid YAML")
				assert.Equal(t, "proj-1", got["projectId"])
			},
		},
		{name: "csv is rejected", format: "csv", wantErr: true},
		{name: "unknown format is rejected", format: "xml", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			cmd := &cobra.Command{}
			cmd.SetOut(&buf)

			err := renderShowModels(cmd, tt.format, "proj-1", full)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Empty(t, buf.String(), "rejected formats must not print anything")
				return
			}
			assert.NoError(t, err)
			if tt.check != nil {
				tt.check(t, buf.String())
			}
		})
	}
}

// TestRenderShowModelsNilConfig pins that a nil config from the client cannot
// panic the table renderer.
func TestRenderShowModelsNilConfig(t *testing.T) {
	for _, format := range []string{"table", "json", "yaml"} {
		var buf bytes.Buffer
		cmd := &cobra.Command{}
		cmd.SetOut(&buf)
		assert.NoError(t, renderShowModels(cmd, format, "proj-3", nil), "format %q", format)
		assert.Contains(t, buf.String(), "proj-3")
	}
}

func TestRenderShowModelsJSONShape(t *testing.T) {
	cfg := &modelconfig.EffectiveModelConfig{
		GenerativeModel:       "litellm/deepseek-v4-flash",
		GenerativeModelSource: "project",
		EmbeddingModel:        "litellm/gemini-embedding-001",
		EmbeddingModelSource:  "provider",
	}

	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)
	assert.NoError(t, renderShowModels(cmd, "json", "proj-1", cfg))

	var got map[string]any
	assert.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	assert.Equal(t, "proj-1", got["projectId"])

	for _, key := range []string{"generative", "embedding"} {
		nested, ok := got[key].(map[string]any)
		if !ok {
			t.Fatalf("%s must be an object, got %T", key, got[key])
		}
		for _, field := range []string{"model", "source"} {
			if _, ok := nested[field]; !ok {
				t.Errorf("%s.%s must always be present", key, field)
			}
		}
	}

	gen, ok := got["generative"].(map[string]any)
	if !ok {
		t.Fatalf("generative must be an object, got %T", got["generative"])
	}
	assert.Equal(t, "litellm/deepseek-v4-flash", gen["model"])
	assert.Equal(t, "project", gen["source"])
}

// TestRenderShowModelsJSONKeepsUnsetKeys pins that a model the server could not
// resolve is an empty string rather than a missing key, so scripts can rely on
// the shape (the class of bug fixed for `projects get` in #2065).
func TestRenderShowModelsJSONKeepsUnsetKeys(t *testing.T) {
	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)
	assert.NoError(t, renderShowModels(cmd, "json", "proj-2", &modelconfig.EffectiveModelConfig{}))

	var got map[string]any
	assert.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	embedding, ok := got["embedding"].(map[string]any)
	if !ok {
		t.Fatalf("embedding must be an object, got %T", got["embedding"])
	}
	assert.Equal(t, "", embedding["model"])
	assert.Equal(t, "", embedding["source"])
}

// ── create-token env opt-in (#2068) ──────────────────────────────────────────

func TestShouldWriteTokenEnv(t *testing.T) {
	tests := []struct {
		name     string
		writeEnv bool
		noEnv    bool
		want     bool
		wantErr  bool
	}{
		{name: "default writes nothing", want: false},
		{name: "write-env opts in", writeEnv: true, want: true},
		{name: "no-env alone stays off", noEnv: true, want: false},
		{name: "contradictory flags are rejected", writeEnv: true, noEnv: true, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := shouldWriteTokenEnv(tt.writeEnv, tt.noEnv)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestWriteTokenToEnvLocal(t *testing.T) {
	t.Run("creates the file with mode 0600", func(t *testing.T) {
		dir := t.TempDir()

		path, err := writeTokenToEnvLocal(dir, "emt_secret")
		assert.NoError(t, err)
		assert.Equal(t, filepath.Join(dir, ".env.local"), path)
		assert.True(t, filepath.IsAbs(path))

		info, err := os.Stat(path)
		assert.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

		env, err := godotenv.Read(path)
		assert.NoError(t, err)
		assert.Equal(t, "emt_secret", env["MEMORY_PROJECT_API_KEY"])

		raw, err := os.ReadFile(path)
		assert.NoError(t, err)
		assert.True(t, strings.HasSuffix(string(raw), "\n"), "the file must end with a newline")
	})

	t.Run("preserves other keys and tightens an existing 0644 file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, ".env.local")
		assert.NoError(t, os.WriteFile(path, []byte("MEMORY_PROJECT_API_KEY=emt_old\nOTHER_KEY=keep-me\n"), 0o644))
		assert.NoError(t, os.Chmod(path, 0o644))

		_, err := writeTokenToEnvLocal(dir, "emt_new")
		assert.NoError(t, err)

		info, err := os.Stat(path)
		assert.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

		env, err := godotenv.Read(path)
		assert.NoError(t, err)
		assert.Equal(t, "emt_new", env["MEMORY_PROJECT_API_KEY"])
		assert.Equal(t, "keep-me", env["OTHER_KEY"])
	})

	// An unparseable .env.local must not be silently replaced: that would drop
	// every key it holds, including other credentials.
	t.Run("refuses to overwrite an unparseable file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, ".env.local")
		original := []byte("KEY=\"unterminated\n")
		assert.NoError(t, os.WriteFile(path, original, 0o600))

		_, err := writeTokenToEnvLocal(dir, "emt_new")
		assert.Error(t, err)

		after, readErr := os.ReadFile(path)
		assert.NoError(t, readErr)
		assert.Equal(t, original, after, "the original file must be left untouched")
	})
}
