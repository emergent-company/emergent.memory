package blueprints_test

import (
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/apps/cli/internal/blueprints"
)

// TestValidateAgents_ServerEnums verifies the CLI validator accepts the server's
// flowType (single|sequential|loop) and dispatchMode (sync|queued) enums, and
// rejects the legacy values that no longer map to the server.
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
}
