package blueprints

import (
	"encoding/json"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestProjectFileIdentityParsing proves ProjectFile reads name/version/
// description/author alongside the existing projectInfo, matching the bundled
// blueprint manifest header (operator/project.yaml).
func TestProjectFileIdentityParsing(t *testing.T) {
	yamlDoc := "name: operator\nversion: 1.0.1\ndescription: >\n  The general assistant.\nauthor: memory\nprojectInfo: |\n  # Operator Blueprint\n  Body here.\n"

	var pf ProjectFile
	if err := yaml.Unmarshal([]byte(yamlDoc), &pf); err != nil {
		t.Fatalf("yaml decode: %v", err)
	}
	if pf.Name != "operator" {
		t.Fatalf("Name = %q, want operator", pf.Name)
	}
	if pf.Version != "1.0.1" {
		t.Fatalf("Version = %q, want 1.0.1", pf.Version)
	}
	if strings.TrimSpace(pf.Description) != "The general assistant." {
		t.Fatalf("Description = %q", pf.Description)
	}
	if pf.Author != "memory" {
		t.Fatalf("Author = %q, want memory", pf.Author)
	}
	if !strings.Contains(pf.ProjectInfo, "Operator Blueprint") {
		t.Fatalf("ProjectInfo = %q", pf.ProjectInfo)
	}
}

// TestProjectFileIdentityJSONParsing proves the JSON form reads the identity
// fields too (the loader decodes .json project files the same way).
func TestProjectFileIdentityJSONParsing(t *testing.T) {
	doc := `{"name":"operator","version":"1.0.1","description":"d","author":"memory","projectInfo":"info"}`
	var pf ProjectFile
	if err := json.Unmarshal([]byte(doc), &pf); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if pf.Name != "operator" || pf.Version != "1.0.1" || pf.Author != "memory" {
		t.Fatalf("unexpected identity: Name=%q Version=%q Author=%q", pf.Name, pf.Version, pf.Author)
	}
}

func testProviders() []ProjectProvider {
	return []ProjectProvider{
		{Provider: "deepseek", GenerativeModel: "deepseek-v4-flash", EmbeddingModel: "deepseek-embed-001"},
		{Provider: "google", GenerativeModel: "gemini-2.5-flash"},
		{Provider: "openai"}, // configured but no generative model selected
	}
}

// TestModelMap_ResolvePrecedence locks the install-time choice resolution:
// stored override > pinned model (if servable) > "" (project default).
func TestModelMap_ResolvePrecedence(t *testing.T) {
	providers := testProviders()
	// deepseek is the only servable provider for a pinned model here.
	mm := NewModelMap(providers, map[string]string{"a": "openai/gpt-4o"})

	cases := []struct {
		name   string
		agent  AgentFile
		expect string
	}{
		{
			name:   "stored override wins over servable pinned model",
			agent:  AgentFile{Name: "a", Model: &AgentModel{Name: "deepseek/deepseek-v4-flash"}},
			expect: "openai/gpt-4o",
		},
		{
			name:   "servable pinned model with no override",
			agent:  AgentFile{Name: "b", Model: &AgentModel{Name: "deepseek/deepseek-v4-flash"}},
			expect: "deepseek/deepseek-v4-flash",
		},
		{
			name:   "non-servable pinned model falls back to project default",
			agent:  AgentFile{Name: "c", Model: &AgentModel{Name: "anthropic/claude-sonnet"}},
			expect: "",
		},
		{
			name:   "stored empty override is an explicit project default",
			agent:  AgentFile{Name: "d", Model: &AgentModel{Name: "deepseek/deepseek-v4-flash"}},
			expect: "",
		},
		{
			name:   "agent without a model block resolves to project default",
			agent:  AgentFile{Name: "e"},
			expect: "",
		},
	}

	// Pre-seed the empty-override case.
	mm.overrides["d"] = ""

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mm.Resolve(tc.agent); got != tc.expect {
				t.Fatalf("Resolve(%q) = %q, want %q", tc.agent.Name, got, tc.expect)
			}
		})
	}
}

// TestModelMap_ServableModels proves the select options are the project's
// configured generative models only, sorted and deduplicated.
func TestModelMap_ServableModels(t *testing.T) {
	mm := NewModelMap(testProviders(), nil)
	got := mm.ServableModels()
	want := []string{"deepseek/deepseek-v4-flash", "google/gemini-2.5-flash"}
	if len(got) != len(want) {
		t.Fatalf("ServableModels = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ServableModels[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestModelMap_IsServable proves servability is provider-prefix based and that
// malformed or empty references are never servable.
func TestModelMap_IsServable(t *testing.T) {
	mm := NewModelMap(testProviders(), nil)

	if !mm.IsServable("deepseek/deepseek-v4-flash") {
		t.Fatal("expected deepseek ref to be servable")
	}
	if !mm.IsServable("google/gemini-2.5-flash") {
		t.Fatal("expected google ref to be servable")
	}
	if mm.IsServable("anthropic/claude-sonnet") {
		t.Fatal("expected anthropic ref to be non-servable")
	}
	if mm.IsServable("bare-model") {
		t.Fatal("expected bare model ref to be non-servable (malformed)")
	}
	if mm.IsServable("") {
		t.Fatal("expected empty ref to be non-servable")
	}
}

// TestParseMapModel proves --map-model parsing accepts agent=provider/model and
// agent= (project default), and rejects malformed values.
func TestParseMapModel(t *testing.T) {
	ok := []struct {
		raw          string
		agent, model string
	}{
		{"operator=deepseek/deepseek-v4-flash", "operator", "deepseek/deepseek-v4-flash"},
		{"operator=", "operator", ""},
		{" operator = deepseek/deepseek-v4-flash ", "operator", "deepseek/deepseek-v4-flash"},
		{"vertex=google-vertex/publishers/google/models/gemini-2.5-flash", "vertex", "google-vertex/publishers/google/models/gemini-2.5-flash"},
	}
	for _, tc := range ok {
		agent, model, err := ParseMapModel(tc.raw)
		if err != nil {
			t.Fatalf("ParseMapModel(%q) unexpected error: %v", tc.raw, err)
		}
		if agent != tc.agent || model != tc.model {
			t.Fatalf("ParseMapModel(%q) = (%q, %q), want (%q, %q)", tc.raw, agent, model, tc.agent, tc.model)
		}
	}

	bad := []string{
		"operator",                    // no '='
		"=deepseek/deepseek-v4-flash", // empty agent
		"operator=baremodel",          // model not in provider/model form
		"operator=openai//gpt-4o",     // double slash
		"operator=deepseek/",          // missing model name
	}
	for _, raw := range bad {
		if _, _, err := ParseMapModel(raw); err == nil {
			t.Fatalf("ParseMapModel(%q) expected error, got nil", raw)
		}
	}
}

// TestApplyModelChoice proves an empty choice clears the model name (project
// default) and a non-empty choice replaces it, mirroring NormalizeAgentModels.
func TestApplyModelChoice(t *testing.T) {
	ag := &AgentFile{Name: "a", Model: &AgentModel{Name: "openai/gpt-4o", Temperature: f32(0.7)}}
	ApplyModelChoice(ag, "")
	if ag.Model.Name != "" || ag.Model.Provider != "" {
		t.Fatalf("empty choice: Name=%q Provider=%q, want both empty", ag.Model.Name, ag.Model.Provider)
	}
	if ag.Model.Temperature == nil || *ag.Model.Temperature != 0.7 {
		t.Fatal("temperature tuning was not preserved")
	}

	ApplyModelChoice(ag, "deepseek/deepseek-v4-flash")
	if ag.Model.Name != "deepseek/deepseek-v4-flash" || ag.Model.Provider != "" {
		t.Fatalf("non-empty choice: Name=%q Provider=%q", ag.Model.Name, ag.Model.Provider)
	}

	// No model block: no-op.
	ag2 := &AgentFile{Name: "b"}
	ApplyModelChoice(ag2, "deepseek/deepseek-v4-flash")
	if ag2.Model != nil {
		t.Fatal("ApplyModelChoice should not create a model block")
	}
}

// TestModelMap_ResolveUnknownProviders proves that when the provider list is
// unknown (fetch failed), a pinned model is kept rather than downgraded to the
// project default.
func TestModelMap_ResolveUnknownProviders(t *testing.T) {
	mm := NewModelMap(nil, nil)
	mm.MarkProvidersUnknown()

	// Without an override and with unknown availability, the pinned model stays.
	ag := AgentFile{Name: "a", Model: &AgentModel{Name: "anthropic/claude-sonnet"}}
	if got := mm.Resolve(ag); got != "anthropic/claude-sonnet" {
		t.Fatalf("Resolve = %q, want pinned model kept", got)
	}

	// A stored override still wins.
	mm.overrides["a"] = "openai/gpt-4o"
	if got := mm.Resolve(ag); got != "openai/gpt-4o" {
		t.Fatalf("Resolve with override = %q, want openai/gpt-4o", got)
	}
}

func f32(v float32) *float32 { return &v }
