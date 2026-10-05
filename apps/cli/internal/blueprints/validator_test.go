package blueprints_test

import (
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/apps/cli/internal/blueprints"
)

// TestValidateAgents_ServerEnums verifies the CLI validator accepts the server's
// flowType (single|sequential|loop), dispatchMode (sync|queued), and visibility
// (project|external|internal) enums, and rejects the legacy values that no longer
// map to the server.
func TestValidateAgents_ServerEnums(t *testing.T) {
	t.Run("valid server enums pass", func(t *testing.T) {
		agents := []blueprints.AgentFile{
			{Name: "task-worker", Description: "d", FlowType: "single", DispatchMode: "queued"},
			{Name: "loop-agent", Description: "d", FlowType: "loop", DispatchMode: "sync"},
			{Name: "seq-agent", Description: "d", FlowType: "sequential"},
		}
		report := blueprints.Validate(nil, nil, agents, nil, nil, nil, nil)
		if report.HasErrors() {
			t.Fatalf("expected no errors for server enums, got: %v", report.Errors())
		}
	})

	t.Run("legacy enum values are rejected", func(t *testing.T) {
		agents := []blueprints.AgentFile{
			{Name: "legacy", Description: "d", FlowType: "reactive", DispatchMode: "round_robin"},
		}
		report := blueprints.Validate(nil, nil, agents, nil, nil, nil, nil)
		errs := report.Errors()
		if len(errs) != 2 {
			t.Fatalf("expected 2 errors (flowType + dispatchMode), got %d: %v", len(errs), errs)
		}
		var joined string
		for _, e := range errs {
			joined += e.Message + "\n"
		}
		if !strings.Contains(joined, "single, sequential, loop") {
			t.Fatalf("expected flowType error to list server enums, got: %s", joined)
		}
		if !strings.Contains(joined, "sync, queued") {
			t.Fatalf("expected dispatchMode error to list server enums, got: %s", joined)
		}
	})

	// The server's authoritative set lives in
	// apps/server/domain/agents/entity.go (VisibilityProject|External|Internal);
	// empty is normalized to "project". See issue #1480.
	t.Run("valid visibility values pass", func(t *testing.T) {
		agents := []blueprints.AgentFile{
			{Name: "project-agent", Description: "d", Visibility: "project"},
			{Name: "external-agent", Description: "d", Visibility: "external"},
			{Name: "internal-agent", Description: "d", Visibility: "internal"},
			{Name: "empty-agent", Description: "d", Visibility: ""},
		}
		report := blueprints.Validate(nil, nil, agents, nil, nil, nil, nil)
		if report.HasErrors() {
			t.Fatalf("expected no errors for server visibility enums, got: %v", report.Errors())
		}
	})

	t.Run("legacy visibility values are rejected", func(t *testing.T) {
		agents := []blueprints.AgentFile{
			{Name: "public-agent", Description: "d", Visibility: "public"},
			{Name: "private-agent", Description: "d", Visibility: "private"},
			{Name: "system-agent", Description: "d", Visibility: "system"},
		}
		report := blueprints.Validate(nil, nil, agents, nil, nil, nil, nil)
		errs := report.Errors()
		if len(errs) != 3 {
			t.Fatalf("expected 3 errors (one per legacy visibility), got %d: %v", len(errs), errs)
		}
		var joined string
		for _, e := range errs {
			if e.Field != "visibility" {
				t.Fatalf("expected only visibility errors, got field %q: %v", e.Field, e)
			}
			joined += e.Message + "\n"
		}
		if !strings.Contains(joined, "project, external, internal") {
			t.Fatalf("expected visibility error to list server enums (project, external, internal), got: %s", joined)
		}
		if strings.Contains(joined, "public, private, system") {
			t.Fatalf("expected visibility error to no longer list legacy enums, got: %s", joined)
		}
	})
}

// TestValidateAgents_ModelNameProviderPrefix verifies the blueprint validator
// rejects a model.name that is not in the server's required "provider/model"
// form, so an offline `memory blueprints validate` fails before the server
// would 400 on apply (#1489).
func TestValidateAgents_ModelNameProviderPrefix(t *testing.T) {
	t.Run("prefixed model names pass", func(t *testing.T) {
		agents := []blueprints.AgentFile{
			{Name: "prefixed", Description: "d", Model: &blueprints.AgentModel{Name: "deepseek/deepseek-v4-flash"}},
			{Name: "vertex", Description: "d", Model: &blueprints.AgentModel{Name: "google-vertex/publishers/google/models/gemini-2.5-flash"}},
		}
		report := blueprints.Validate(nil, nil, agents, nil, nil, nil, nil)
		if report.HasErrors() {
			t.Fatalf("expected no errors for prefixed model names, got: %v", report.Errors())
		}
	})

	t.Run("bare model name is rejected", func(t *testing.T) {
		agents := []blueprints.AgentFile{
			{Name: "bare", Description: "d", Model: &blueprints.AgentModel{Name: "gemini-2.5-flash"}},
		}
		report := blueprints.Validate(nil, nil, agents, nil, nil, nil, nil)
		errs := report.Errors()
		if len(errs) != 1 {
			t.Fatalf("expected 1 error for bare model name, got %d: %v", len(errs), errs)
		}
		if errs[0].Field != "model.name" {
			t.Fatalf("expected field model.name, got %q", errs[0].Field)
		}
		if !strings.Contains(errs[0].Message, "provider/model") {
			t.Fatalf("expected message to mention provider/model form, got: %q", errs[0].Message)
		}
	})

	t.Run("malformed prefixed names are rejected", func(t *testing.T) {
		for _, name := range []string{"openai/", "/gpt-4o", "/"} {
			agents := []blueprints.AgentFile{
				{Name: "malformed", Description: "d", Model: &blueprints.AgentModel{Name: name}},
			}
			report := blueprints.Validate(nil, nil, agents, nil, nil, nil, nil)
			if !report.HasErrors() {
				t.Fatalf("expected an error for malformed model name %q, got none", name)
			}
		}
	})

	t.Run("absent model block passes", func(t *testing.T) {
		agents := []blueprints.AgentFile{{Name: "no-model", Description: "d"}}
		report := blueprints.Validate(nil, nil, agents, nil, nil, nil, nil)
		if report.HasErrors() {
			t.Fatalf("expected no errors without a model block, got: %v", report.Errors())
		}
	})

	t.Run("empty model name reports required, not form", func(t *testing.T) {
		agents := []blueprints.AgentFile{
			{Name: "empty-model", Description: "d", Model: &blueprints.AgentModel{Name: ""}},
		}
		report := blueprints.Validate(nil, nil, agents, nil, nil, nil, nil)
		errs := report.Errors()
		if len(errs) != 1 {
			t.Fatalf("expected 1 error for empty model name, got %d: %v", len(errs), errs)
		}
		if !strings.Contains(errs[0].Message, "required") {
			t.Fatalf("expected required message for empty model name, got: %q", errs[0].Message)
		}
	})
}
