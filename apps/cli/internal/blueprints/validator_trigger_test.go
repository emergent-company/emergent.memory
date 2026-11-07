package blueprints_test

import (
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/apps/cli/internal/blueprints"
)

// TestValidateAgents_DefinitionOnlyInfoNote pins the #321 validate note: an
// agent with no triggerType is valid, but validate must tell the operator it
// installs as a definition only and will never run on its own. The note is
// info-level so it cannot fail a blueprint (not even under --strict).
func TestValidateAgents_DefinitionOnlyInfoNote(t *testing.T) {
	agents := []blueprints.AgentFile{
		{Name: "memory-extractor", Description: "d", SourceFile: "agents/memory-extractor.yaml"},
		{Name: "daily-briefing", Description: "d", TriggerType: "schedule", CronSchedule: "0 8 * * *"},
		{Name: "implicit-reaction", Description: "d", ReactionConfig: &blueprints.ReactionConfig{Events: []string{"created"}}},
		{Name: "assistant", Description: "d", TriggerType: "manual"},
	}

	report := blueprints.Validate(nil, nil, agents, nil, nil, nil, nil)

	if report.HasErrors() {
		t.Fatalf("definition-only agents must stay valid, got errors: %v", report.Errors())
	}
	if warns := report.Warnings(); len(warns) != 0 {
		t.Fatalf("the note must be info-level, got warnings: %v", warns)
	}

	infos := report.Infos()
	if len(infos) != 1 {
		t.Fatalf("expected exactly one info note (memory-extractor), got %d: %v", len(infos), infos)
	}
	info := infos[0]
	if info.Severity != blueprints.ValidationInfo {
		t.Fatalf("severity = %q, want %q", info.Severity, blueprints.ValidationInfo)
	}
	if info.ResourceType != "agent" || info.Name != "memory-extractor" {
		t.Fatalf("note attributed to %s %q, want agent \"memory-extractor\"", info.ResourceType, info.Name)
	}
	if info.Field != "triggerType" {
		t.Fatalf("note field = %q, want triggerType", info.Field)
	}
	if !strings.Contains(info.Message, "definition only; trigger manually or add triggerType") {
		t.Fatalf("note message missing the actionable phrase: %q", info.Message)
	}
}
