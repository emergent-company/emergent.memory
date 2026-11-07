package blueprints

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdkagents "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/agentdefinitions"
	sdkruntime "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/agents"
)

// newAgentOutputBlueprinter wires the definition and runtime clients to the same
// fake server so createAgent's output classification can be exercised.
func newAgentOutputBlueprinter(serverURL string, out *bytes.Buffer) *Blueprinter {
	return &Blueprinter{
		projectID:     "proj-1",
		agents:        sdkagents.NewClient(http.DefaultClient, serverURL, noopAuthRuntime{}, "", "proj-1"),
		runtimeAgents: sdkruntime.NewClient(http.DefaultClient, serverURL, noopAuthRuntime{}, "", "proj-1"),
		out:           out,
	}
}

// newAgentOutputServer returns a fake definition+runtime server and counters for
// runtime creates and the last create body.
func newAgentOutputServer(t *testing.T) (*httptest.Server, *int, *map[string]any) {
	t.Helper()
	var runtimeCreates int
	var lastRuntimeBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/projects/proj-1/agent-definitions":
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":"def-1"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/projects/proj-1/agents":
			_, _ = w.Write([]byte(`{"success":true,"data":[]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/projects/proj-1/agents":
			if err := json.NewDecoder(r.Body).Decode(&lastRuntimeBody); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			runtimeCreates++
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":"ra-1"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &runtimeCreates, &lastRuntimeBody
}

// TestCreateAgentOutput_ClassifiesDefinitionVsRuntimeAgent pins the install
// output for #321: a blueprint agent without triggerType installs as a
// definition only and must not print the same "created agent" line as an agent
// that gets a runtime row. Before the fix both cases printed `created  agent
// "<name>"`, hiding which agents would ever run.
func TestCreateAgentOutput_ClassifiesDefinitionVsRuntimeAgent(t *testing.T) {
	srv, runtimeCreates, lastRuntimeBody := newAgentOutputServer(t)

	tests := []struct {
		name        string
		agent       AgentFile
		wantLabel   string
		wantRuntime bool
		wantTrigger string
	}{
		{
			name:      "no trigger installs definition only",
			agent:     AgentFile{Name: "memory-extractor", SourceFile: "agents/memory-extractor.yaml"},
			wantLabel: `created  definition "memory-extractor" (definition only; trigger manually or add triggerType)`,
		},
		{
			name:        "cron installs runtime schedule agent",
			agent:       AgentFile{Name: "daily-briefing", CronSchedule: "0 8 * * *"},
			wantLabel:   `created  runtime agent "daily-briefing" (trigger: schedule)`,
			wantRuntime: true,
			wantTrigger: "schedule",
		},
		{
			name:        "manual installs runtime demand agent",
			agent:       AgentFile{Name: "assistant", TriggerType: "manual"},
			wantLabel:   `created  runtime agent "assistant" (trigger: manual)`,
			wantRuntime: true,
			wantTrigger: "manual",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			*runtimeCreates = 0
			*lastRuntimeBody = nil

			var buf bytes.Buffer
			b := newAgentOutputBlueprinter(srv.URL, &buf)

			res := b.createAgent(context.Background(), tc.agent)
			if res.Action != BlueprintsActionCreated {
				t.Fatalf("expected created result, got %s (%v)", res.Action, res.Error)
			}
			if got := res.RuntimeTrigger != ""; got != tc.wantRuntime {
				t.Fatalf("RuntimeTrigger presence = %v, want %v", got, tc.wantRuntime)
			}

			b.printResult(res)
			if !strings.Contains(buf.String(), tc.wantLabel) {
				t.Fatalf("output missing %q:\n%s", tc.wantLabel, buf.String())
			}

			if tc.wantRuntime {
				if *runtimeCreates != 1 {
					t.Fatalf("expected 1 runtime agent create, got %d", *runtimeCreates)
				}
				if (*lastRuntimeBody)["triggerType"] != tc.wantTrigger {
					t.Fatalf("runtime triggerType = %v, want %q", (*lastRuntimeBody)["triggerType"], tc.wantTrigger)
				}
			} else if *runtimeCreates != 0 {
				t.Fatalf("definition-only agent must not create a runtime agent, got %d create(s)", *runtimeCreates)
			}
		})
	}
}

// TestDryRunAgents_ClassifiesAndKeepsSourceFile pins the dry-run classifier and
// keeps the source-file suffix every other dry-run line prints.
func TestDryRunAgents_ClassifiesAndKeepsSourceFile(t *testing.T) {
	var buf bytes.Buffer
	b := NewBlueprintsApplier(nil, "", nil, nil, nil, nil, true /* dryRun */, false, &buf)

	agents := []AgentFile{
		{Name: "memory-extractor", SourceFile: "agents/memory-extractor.yaml"},
		{Name: "daily-briefing", SourceFile: "agents/daily-briefing.yaml", CronSchedule: "0 8 * * *"},
	}
	results := b.dryRunAgents(agents)

	out := buf.String()
	wantDefinition := `[dry-run] would create definition "memory-extractor" (agents/memory-extractor.yaml) (definition only; trigger manually or add triggerType)`
	if !strings.Contains(out, wantDefinition) {
		t.Fatalf("output missing %q:\n%s", wantDefinition, out)
	}
	wantRuntime := `[dry-run] would create runtime agent "daily-briefing" (agents/daily-briefing.yaml) (trigger: schedule)`
	if !strings.Contains(out, wantRuntime) {
		t.Fatalf("output missing %q:\n%s", wantRuntime, out)
	}
	if len(results) != 2 || results[0].RuntimeTrigger != "" || results[1].RuntimeTrigger != "schedule" {
		t.Fatalf("unexpected dry-run results: %+v", results)
	}
}

// TestSkippedAgentOutput_SaysWhatWasEnsured verifies a skipped definition does
// not imply the runtime row was skipped too: the label must say the runtime
// agent was ensured (or that the agent is definition-only).
func TestSkippedAgentOutput_SaysWhatWasEnsured(t *testing.T) {
	srv, runtimeCreates, _ := newAgentOutputServer(t)

	tests := []struct {
		name      string
		agent     AgentFile
		wantLabel string
		wantCalls int
	}{
		{
			name:      "triggered agent reports runtime ensured",
			agent:     AgentFile{Name: "daily-briefing", CronSchedule: "0 8 * * *"},
			wantLabel: `skipped  definition "daily-briefing" (already exists; use --upgrade to update; runtime agent ensured, trigger: schedule)`,
			wantCalls: 1,
		},
		{
			name:      "untriggered agent reports definition only",
			agent:     AgentFile{Name: "memory-extractor"},
			wantLabel: `skipped  definition "memory-extractor" (already exists; use --upgrade to update; definition only — trigger manually or add triggerType)`,
			wantCalls: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			*runtimeCreates = 0

			var buf bytes.Buffer
			b := newAgentOutputBlueprinter(srv.URL, &buf)
			existing := map[string]sdkagents.AgentDefinitionSummary{
				tc.agent.Name: {ID: "def-1", Name: tc.agent.Name},
			}

			res := b.blueprintAgent(context.Background(), tc.agent, existing)
			if res.Action != BlueprintsActionSkipped {
				t.Fatalf("expected skipped result, got %s (%v)", res.Action, res.Error)
			}
			b.printResult(res)

			if !strings.Contains(buf.String(), tc.wantLabel) {
				t.Fatalf("output missing %q:\n%s", tc.wantLabel, buf.String())
			}
			if *runtimeCreates != tc.wantCalls {
				t.Fatalf("runtime creates = %d, want %d", *runtimeCreates, tc.wantCalls)
			}
		})
	}
}
