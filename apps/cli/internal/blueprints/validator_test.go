package blueprints_test

import (
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/apps/cli/internal/blueprints"
)

// TestValidateAgents_ServerEnums verifies the CLI validator accepts the server's
// dispatchMode (sync|queued) and visibility (project|external|internal) enums,
// and rejects the legacy values that no longer map to the server.
func TestValidateAgents_ServerEnums(t *testing.T) {
	t.Run("valid server enums pass", func(t *testing.T) {
		agents := []blueprints.AgentFile{
			{Name: "task-worker", Description: "d", DispatchMode: "queued"},
			{Name: "loop-agent", Description: "d", DispatchMode: "sync"},
			{Name: "seq-agent", Description: "d"},
		}
		report := blueprints.Validate(nil, nil, agents, nil, nil, nil, nil)
		if report.HasErrors() {
			t.Fatalf("expected no errors for server enums, got: %v", report.Errors())
		}
	})

	t.Run("legacy enum values are rejected", func(t *testing.T) {
		agents := []blueprints.AgentFile{
			{Name: "legacy", Description: "d", DispatchMode: "round_robin"},
		}
		report := blueprints.Validate(nil, nil, agents, nil, nil, nil, nil)
		errs := report.Errors()
		if len(errs) != 1 {
			t.Fatalf("expected 1 error (dispatchMode), got %d: %v", len(errs), errs)
		}
		var joined string
		for _, e := range errs {
			joined += e.Message + "\n"
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

// TestValidateAgents_CronSchedule verifies the validator parses a
// schedule-triggered agent's cronSchedule with the same 5-field parser the
// agents API uses (apps/server/domain/agents/triggers.go). A 6-field expression
// used to pass validate and only fail at install with a 400 (#320).
func TestValidateAgents_CronSchedule(t *testing.T) {
	t.Run("6-field cron is rejected", func(t *testing.T) {
		agents := []blueprints.AgentFile{
			{Name: "six-field", Description: "d", CronSchedule: "0 0 2 * * *"},
		}
		report := blueprints.Validate(nil, nil, agents, nil, nil, nil, nil)
		if !hasIssue(report.Errors(), "cronSchedule", "expected exactly 5 fields") {
			t.Fatalf("expected a 6-field cron error, got: %v", report.Issues)
		}
	})

	t.Run("malformed cron is rejected", func(t *testing.T) {
		agents := []blueprints.AgentFile{
			{Name: "garbage", Description: "d", TriggerType: "schedule", CronSchedule: "not-a-cron"},
			{Name: "out-of-range", Description: "d", TriggerType: "schedule", CronSchedule: "0 25 * * *"},
		}
		report := blueprints.Validate(nil, nil, agents, nil, nil, nil, nil)
		errs := report.Errors()
		if len(errs) != 2 {
			t.Fatalf("expected 2 cron errors, got %d: %v", len(errs), errs)
		}
		for _, e := range errs {
			if e.Field != "cronSchedule" {
				t.Fatalf("expected only cronSchedule errors, got field %q: %v", e.Field, e)
			}
		}
	})

	t.Run("whitespace-only cron is rejected", func(t *testing.T) {
		agents := []blueprints.AgentFile{
			{Name: "blank", Description: "d", TriggerType: "schedule", CronSchedule: "   "},
		}
		report := blueprints.Validate(nil, nil, agents, nil, nil, nil, nil)
		if !hasIssue(report.Errors(), "cronSchedule", "expected exactly 5 fields") {
			t.Fatalf("expected a whitespace-only cron error, got: %v", report.Issues)
		}
	})

	t.Run("5-field cron passes", func(t *testing.T) {
		agents := []blueprints.AgentFile{
			{Name: "daily", Description: "d", CronSchedule: "0 2 * * *"},
			{Name: "quarter-hourly", Description: "d", TriggerType: "schedule", CronSchedule: "*/15 * * * *"},
			{Name: "weekly", Description: "d", TriggerType: "schedule", CronSchedule: "0 0 * * 0"},
			{Name: "implicit-schedule", Description: "d", CronSchedule: ""},
		}
		report := blueprints.Validate(nil, nil, agents, nil, nil, nil, nil)
		if report.HasErrors() {
			t.Fatalf("expected no cron errors, got: %v", report.Errors())
		}
	})

	// The agents API only validates the cron for a schedule trigger; a cron on
	// a reaction/manual agent is stored unused, so validate must match.
	t.Run("non-schedule triggers skip the cron check", func(t *testing.T) {
		agents := []blueprints.AgentFile{
			{Name: "reaction", Description: "d", TriggerType: "reaction", CronSchedule: "0 0 2 * * *"},
			{Name: "implicit-reaction", Description: "d", ReactionConfig: &blueprints.ReactionConfig{Events: []string{"created"}}, CronSchedule: "0 0 2 * * *"},
			{Name: "manual", Description: "d", TriggerType: "manual", CronSchedule: "not-a-cron"},
		}
		report := blueprints.Validate(nil, nil, agents, nil, nil, nil, nil)
		if report.HasErrors() {
			t.Fatalf("expected no cron errors for non-schedule triggers, got: %v", report.Errors())
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

// TestValidateAgents_StructuredModelProvider verifies the validator accepts the
// server's structured {name, provider} model shape via the shared modelref
// parser, rejects conflicting/incomplete combinations, and reports the offender.
func TestValidateAgents_StructuredModelProvider(t *testing.T) {
	t.Run("bare name qualified by provider passes", func(t *testing.T) {
		agents := []blueprints.AgentFile{
			{Name: "structured", Description: "d", Model: &blueprints.AgentModel{Name: "gpt-4o", Provider: "openai"}},
		}
		report := blueprints.Validate(nil, nil, agents, nil, nil, nil, nil)
		if report.HasErrors() {
			t.Fatalf("expected no errors for structured provider model, got: %v", report.Errors())
		}
	})

	t.Run("redundant matching provider passes", func(t *testing.T) {
		agents := []blueprints.AgentFile{
			{Name: "redundant", Description: "d", Model: &blueprints.AgentModel{Name: "openai/gpt-4o", Provider: "openai"}},
		}
		report := blueprints.Validate(nil, nil, agents, nil, nil, nil, nil)
		if report.HasErrors() {
			t.Fatalf("expected no errors for redundant provider, got: %v", report.Errors())
		}
	})

	t.Run("conflicting provider is rejected naming the agent and value", func(t *testing.T) {
		agents := []blueprints.AgentFile{
			{Name: "conflicted", Description: "d", Model: &blueprints.AgentModel{Name: "openai/gpt-4o", Provider: "google"}},
		}
		report := blueprints.Validate(nil, nil, agents, nil, nil, nil, nil)
		errs := report.Errors()
		if len(errs) != 1 {
			t.Fatalf("expected 1 error for conflicting provider, got %d: %v", len(errs), errs)
		}
		if errs[0].Name != "conflicted" || errs[0].Field != "model.name" {
			t.Fatalf("expected agent=conflicted field=model.name, got Name=%q Field=%q", errs[0].Name, errs[0].Field)
		}
		if !strings.Contains(errs[0].Message, "provider/model") || !strings.Contains(errs[0].Message, "google") {
			t.Fatalf("expected actionable message mentioning expected form and offender, got: %q", errs[0].Message)
		}
	})

	t.Run("provider without a model name is rejected", func(t *testing.T) {
		agents := []blueprints.AgentFile{
			{Name: "no-name", Description: "d", Model: &blueprints.AgentModel{Provider: "openai"}},
		}
		report := blueprints.Validate(nil, nil, agents, nil, nil, nil, nil)
		if !report.HasErrors() {
			t.Fatal("expected an error for provider without a model name")
		}
	})
}

// TestNormalizeAgentModels verifies canonicalization: a structured provider is
// folded into the "provider/model" name and cleared, prefixed names (including
// slash-bearing model paths) are preserved, and malformed references are
// reported with agent/file context without being rewritten.
func TestNormalizeAgentModels(t *testing.T) {
	t.Run("structured provider folded into name", func(t *testing.T) {
		agents := []blueprints.AgentFile{
			{Name: "structured", SourceFile: "agents/structured.yaml", Model: &blueprints.AgentModel{Name: "gpt-4o", Provider: "openai"}},
		}
		report := blueprints.NormalizeAgentModels(agents)
		if report.HasErrors() {
			t.Fatalf("unexpected errors: %v", report.Errors())
		}
		if agents[0].Model.Name != "openai/gpt-4o" || agents[0].Model.Provider != "" {
			t.Fatalf("expected canonical openai/gpt-4o with cleared provider, got Name=%q Provider=%q",
				agents[0].Model.Name, agents[0].Model.Provider)
		}
	})

	t.Run("prefixed and slash-bearing model paths preserved", func(t *testing.T) {
		agents := []blueprints.AgentFile{
			{Name: "prefixed", Model: &blueprints.AgentModel{Name: "deepseek/deepseek-v4-flash"}},
			{Name: "vertex", Model: &blueprints.AgentModel{Name: "google-vertex/publishers/google/models/gemini-2.5-flash"}},
		}
		report := blueprints.NormalizeAgentModels(agents)
		if report.HasErrors() {
			t.Fatalf("unexpected errors: %v", report.Errors())
		}
		if agents[0].Model.Name != "deepseek/deepseek-v4-flash" {
			t.Fatalf("prefixed name changed: %q", agents[0].Model.Name)
		}
		if agents[1].Model.Name != "google-vertex/publishers/google/models/gemini-2.5-flash" {
			t.Fatalf("slash-bearing path changed: %q", agents[1].Model.Name)
		}
	})

	t.Run("empty reference left empty", func(t *testing.T) {
		agents := []blueprints.AgentFile{
			{Name: "empty", Model: &blueprints.AgentModel{}},
		}
		report := blueprints.NormalizeAgentModels(agents)
		if report.HasErrors() {
			t.Fatalf("unexpected errors: %v", report.Errors())
		}
		if agents[0].Model.Name != "" || agents[0].Model.Provider != "" {
			t.Fatalf("expected empty reference, got Name=%q Provider=%q", agents[0].Model.Name, agents[0].Model.Provider)
		}
	})

	t.Run("malformed reference reported and not rewritten", func(t *testing.T) {
		agents := []blueprints.AgentFile{
			{Name: "bare", SourceFile: "agents/bare.yaml", Model: &blueprints.AgentModel{Name: "gemini-2.5-flash"}},
		}
		report := blueprints.NormalizeAgentModels(agents)
		errs := report.Errors()
		if len(errs) != 1 {
			t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
		}
		if errs[0].Name != "bare" || errs[0].Field != "model.name" || errs[0].SourceFile != "agents/bare.yaml" {
			t.Fatalf("unexpected issue context: %+v", errs[0])
		}
		if !strings.Contains(errs[0].Message, "gemini-2.5-flash") || !strings.Contains(errs[0].Message, "provider/model") {
			t.Fatalf("expected message naming the offender and expected form, got: %q", errs[0].Message)
		}
		// Malformed input must be left untouched so the caller can abort with a
		// clear message rather than silently sending a half-normalized value.
		if agents[0].Model.Name != "gemini-2.5-flash" {
			t.Fatalf("malformed name must not be rewritten, got %q", agents[0].Model.Name)
		}
	})
}

// TestValidatePacks_CrossPackRelationshipEndpoints verifies that a relationship
// may name a source/target object type owned by another pack in the same
// blueprint: a blueprint is applied as a unit, so a cross-pack endpoint is
// legitimate (#1853). A type that exists in no pack in the blueprint still
// fails red.
func TestValidatePacks_CrossPackRelationshipEndpoints(t *testing.T) {
	t.Run("cross-pack target is accepted", func(t *testing.T) {
		packs := []blueprints.PackFile{
			{
				Name:        "core",
				Version:     "1.0.0",
				SourceFile:  "schemas/core.yaml",
				ObjectTypes: []blueprints.ObjectTypeDef{{Name: "Task", Label: "Task"}},
				RelationshipTypes: []blueprints.RelationshipTypeDef{
					{Name: "delivers", SourceType: "Task", TargetType: "Note"},
				},
			},
			{
				Name:        "notes",
				Version:     "1.0.0",
				SourceFile:  "schemas/notes.yaml",
				ObjectTypes: []blueprints.ObjectTypeDef{{Name: "Note", Label: "Note"}},
			},
		}
		report := blueprints.Validate(nil, packs, nil, nil, nil, nil, nil)
		if report.HasErrors() {
			t.Fatalf("expected a cross-pack target to validate green, got: %v", report.Errors())
		}
	})

	t.Run("cross-pack source is accepted", func(t *testing.T) {
		packs := []blueprints.PackFile{
			{
				Name:        "notes",
				Version:     "1.0.0",
				SourceFile:  "schemas/notes.yaml",
				ObjectTypes: []blueprints.ObjectTypeDef{{Name: "Note", Label: "Note"}},
				RelationshipTypes: []blueprints.RelationshipTypeDef{
					{Name: "attached_to", SourceType: "Task", TargetType: "Note"},
				},
			},
			{
				Name:        "core",
				Version:     "1.0.0",
				SourceFile:  "schemas/core.yaml",
				ObjectTypes: []blueprints.ObjectTypeDef{{Name: "Task", Label: "Task"}},
			},
		}
		report := blueprints.Validate(nil, packs, nil, nil, nil, nil, nil)
		if report.HasErrors() {
			t.Fatalf("expected a cross-pack source to validate green, got: %v", report.Errors())
		}
	})

	t.Run("unknown target still fails red", func(t *testing.T) {
		packs := []blueprints.PackFile{
			{
				Name:        "core",
				Version:     "1.0.0",
				SourceFile:  "schemas/core.yaml",
				ObjectTypes: []blueprints.ObjectTypeDef{{Name: "Task", Label: "Task"}},
				RelationshipTypes: []blueprints.RelationshipTypeDef{
					{Name: "delivers", SourceType: "Task", TargetType: "Note"},
				},
			},
		}
		report := blueprints.Validate(nil, packs, nil, nil, nil, nil, nil)
		errs := report.Errors()
		if len(errs) != 1 {
			t.Fatalf("expected 1 error for an unknown target, got %d: %v", len(errs), errs)
		}
		if errs[0].Field != "relationshipTypes[0].targetType" {
			t.Fatalf("expected field relationshipTypes[0].targetType, got %q (%v)", errs[0].Field, errs[0])
		}
		if !strings.Contains(errs[0].Message, "blueprint") || !strings.Contains(errs[0].Message, "Note") {
			t.Fatalf("expected the error to name the blueprint scope and the offender, got: %q", errs[0].Message)
		}
	})

	t.Run("plural target resolves against a sibling pack", func(t *testing.T) {
		packs := []blueprints.PackFile{
			{
				Name:        "core",
				Version:     "1.0.0",
				SourceFile:  "schemas/core.yaml",
				ObjectTypes: []blueprints.ObjectTypeDef{{Name: "Task", Label: "Task"}},
				RelationshipTypes: []blueprints.RelationshipTypeDef{
					{Name: "links", SourceTypes: []string{"Task"}, TargetTypes: []string{"Note", "Unknown"}},
				},
			},
			{
				Name:        "notes",
				Version:     "1.0.0",
				SourceFile:  "schemas/notes.yaml",
				ObjectTypes: []blueprints.ObjectTypeDef{{Name: "Note", Label: "Note"}},
			},
		}
		report := blueprints.Validate(nil, packs, nil, nil, nil, nil, nil)
		errs := report.Errors()
		if len(errs) != 1 {
			t.Fatalf("expected exactly 1 error (the Unknown target), got %d: %v", len(errs), errs)
		}
		if !strings.Contains(errs[0].Message, "Unknown") {
			t.Fatalf("expected the error to name the unknown target, got: %q", errs[0].Message)
		}
	})
}

// TestHasUnknownRelationshipEndpoints pins the lazy gate the install path uses to
// decide whether it must consult the project's object types (#1853).
func TestHasUnknownRelationshipEndpoints(t *testing.T) {
	t.Run("all endpoints resolve locally", func(t *testing.T) {
		packs := []blueprints.PackFile{
			{
				Name: "core", Version: "1.0.0",
				ObjectTypes: []blueprints.ObjectTypeDef{{Name: "Task", Label: "Task"}},
				RelationshipTypes: []blueprints.RelationshipTypeDef{
					{Name: "delivers", SourceType: "Task", TargetType: "Note"},
				},
			},
			{Name: "notes", Version: "1.0.0", ObjectTypes: []blueprints.ObjectTypeDef{{Name: "Note", Label: "Note"}}},
		}
		if blueprints.HasUnknownRelationshipEndpoints(packs) {
			t.Fatal("expected a cross-pack endpoint defined by a sibling pack to resolve locally")
		}
	})

	t.Run("unknown endpoint needs the project", func(t *testing.T) {
		packs := []blueprints.PackFile{
			{
				Name: "core", Version: "1.0.0",
				ObjectTypes: []blueprints.ObjectTypeDef{{Name: "Task", Label: "Task"}},
				RelationshipTypes: []blueprints.RelationshipTypeDef{
					{Name: "mentions", SourceType: "Task", TargetType: "Message"},
				},
			},
		}
		if !blueprints.HasUnknownRelationshipEndpoints(packs) {
			t.Fatal("expected an endpoint defined by no pack to require the project's types")
		}
	})

	t.Run("plural unknown target needs the project", func(t *testing.T) {
		packs := []blueprints.PackFile{
			{
				Name: "core", Version: "1.0.0",
				ObjectTypes: []blueprints.ObjectTypeDef{{Name: "Task", Label: "Task"}},
				RelationshipTypes: []blueprints.RelationshipTypeDef{
					{Name: "links", SourceTypes: []string{"Task"}, TargetTypes: []string{"Task", "Message"}},
				},
			},
		}
		if !blueprints.HasUnknownRelationshipEndpoints(packs) {
			t.Fatal("expected an unknown plural target to require the project's types")
		}
	})

	t.Run("no object types needs no project read", func(t *testing.T) {
		packs := []blueprints.PackFile{{Name: "empty", Version: "1.0.0"}}
		if blueprints.HasUnknownRelationshipEndpoints(packs) {
			t.Fatal("expected no object types to skip the project read (validatePacks also skips)")
		}
	})
}
