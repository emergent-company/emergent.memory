package blueprints_test

import (
	"encoding/json"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/emergent-company/emergent.memory/apps/cli/internal/blueprints"
)

// TestObjectTypeDefBoardFieldsMarshal pins the CLI side of the board-enabled
// object-type contract: the new fields must survive a YAML parse and marshal
// into the pack JSON the applier sends, with the same keys the server's schema
// registry reads.
func TestObjectTypeDefBoardFieldsMarshal(t *testing.T) {
	src := []byte(`
- name: Task
  label: Task
  boardEnabled: true
  allowedStatuses: [todo, doing, done]
  skipEmbeddings: true
  properties:
    title:
      type: string
`)
	var types []blueprints.ObjectTypeDef
	if err := yaml.Unmarshal(src, &types); err != nil {
		t.Fatalf("yaml unmarshal: %v", err)
	}
	if len(types) != 1 {
		t.Fatalf("expected 1 type, got %d", len(types))
	}
	if !types[0].BoardEnabled {
		t.Fatalf("boardEnabled must round-trip, got %+v", types[0])
	}
	if len(types[0].AllowedStatuses) != 3 || types[0].AllowedStatuses[0] != "todo" {
		t.Fatalf("allowedStatuses must round-trip, got %+v", types[0].AllowedStatuses)
	}

	raw, err := json.Marshal(types)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"boardEnabled":true`) {
		t.Fatalf("boardEnabled must marshal into the pack JSON, got %s", raw)
	}
	if !strings.Contains(string(raw), `"allowedStatuses":["todo","doing","done"]`) {
		t.Fatalf("allowedStatuses must marshal, got %s", raw)
	}
}

// TestAgentFileWorkConfigRoundTrip verifies the AgentFile carries the workConfig,
// reactionConfig, triggerType, and cronSchedule blocks through YAML → JSON.
func TestAgentFileWorkConfigRoundTrip(t *testing.T) {
	src := []byte(`
name: board-agent
workConfig:
  status:
    ready: todo
    done: done
  requiresReview: true
triggerType: reaction
reactionConfig:
  objectTypes: [Task]
  events: [created]
cronSchedule: "0 0 * * *"
`)
	var ag blueprints.AgentFile
	if err := yaml.Unmarshal(src, &ag); err != nil {
		t.Fatalf("yaml unmarshal: %v", err)
	}
	if ag.WorkConfig == nil {
		t.Fatalf("workConfig must be parsed")
	}
	if ag.TriggerType != "reaction" {
		t.Fatalf("triggerType must round-trip, got %q", ag.TriggerType)
	}
	if ag.CronSchedule != "0 0 * * *" {
		t.Fatalf("cronSchedule must round-trip, got %q", ag.CronSchedule)
	}
	if ag.ReactionConfig == nil || len(ag.ReactionConfig.ObjectTypes) != 1 || ag.ReactionConfig.ObjectTypes[0] != "Task" {
		t.Fatalf("reactionConfig must round-trip, got %+v", ag.ReactionConfig)
	}

	raw, err := json.Marshal(ag)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(raw)
	if !strings.Contains(s, `"workConfig"`) || !strings.Contains(s, `"reactionConfig"`) {
		t.Fatalf("workConfig/reactionConfig must be emitted, got %s", s)
	}
}

// TestSeedObjectRecordAssigneeRoundTrip verifies assignee survives the JSONL
// dump/seed round-trip.
func TestSeedObjectRecordAssigneeRoundTrip(t *testing.T) {
	raw, err := json.Marshal(blueprints.SeedObjectRecord{Type: "Task", Key: "k1", Assignee: "agent-x"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"assignee":"agent-x"`) {
		t.Fatalf("assignee must marshal, got %s", raw)
	}

	var rec blueprints.SeedObjectRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rec.Assignee != "agent-x" {
		t.Fatalf("assignee must round-trip, got %q", rec.Assignee)
	}
}
