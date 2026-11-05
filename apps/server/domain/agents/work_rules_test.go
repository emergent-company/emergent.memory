package agents

import (
	"testing"
)

func TestEffectiveStatusForType(t *testing.T) {
	def := &AgentDefinition{WorkConfig: AgentWorkConfig{
		Status: AgentWorkStatusConfig{Ready: "agent-ready"},
		ObjectRules: []AgentObjectRule{{
			ObjectType: "Bug",
			Status:     &AgentWorkStatusConfig{Ready: "open"},
		}},
	}}
	if got := def.EffectiveWorkStatus("Bug").Ready; got != "open" {
		t.Fatalf("per-type override = %q, want open", got)
	}
	if got := def.EffectiveWorkStatus("Task").Ready; got != "agent-ready" {
		t.Fatalf("agent fallback = %q, want agent-ready", got)
	}
	if got := def.EffectiveWorkStatus("Task").Done; got != "done" {
		t.Fatalf("built-in fallback = %q, want done", got)
	}
}

func TestValidateWorkRules(t *testing.T) {
	types := map[string]WorkRuleType{
		"Bug":  {AllowedStatuses: []string{"open", "triaged", "closed"}, Fields: []string{"title", "severity"}},
		"Task": {AllowedStatuses: []string{"ready", "in_progress", "done"}},
	}
	base := func() *AgentDefinition {
		return &AgentDefinition{WorkConfig: AgentWorkConfig{
			Status:      AgentWorkStatusConfig{Done: "closed"},
			ObjectRules: []AgentObjectRule{{ObjectType: "Bug"}},
		}}
	}

	if err := ValidateWorkRules(base(), types); err != nil {
		t.Fatalf("valid rule rejected: %v", err)
	}

	bad := base()
	bad.WorkConfig.ObjectRules[0].Triggers = []AgentWorkTrigger{{Status: "closed"}}
	if err := ValidateWorkRules(bad, types); err == nil {
		t.Fatal("non-pickup trigger status was accepted")
	}

	bad = base()
	bad.WorkConfig.ObjectRules[0].Triggers = []AgentWorkTrigger{{Status: "bogus"}}
	if err := ValidateWorkRules(bad, types); err == nil {
		t.Fatal("off-vocabulary trigger status was accepted")
	}

	bad = base()
	bad.WorkConfig.ObjectRules[0].Fields = []string{"nope"}
	if err := ValidateWorkRules(bad, types); err == nil {
		t.Fatal("unknown writable field was accepted")
	}

	bad = base()
	bad.WorkConfig.ObjectRules[0].Transitions = []AgentTransition{{From: "open", To: "open"}}
	if err := ValidateWorkRules(bad, types); err == nil {
		t.Fatal("self transition was accepted")
	}
}

func TestPermitsTransition(t *testing.T) {
	st := AgentWorkStatusConfig{}.withDefaults()
	if !(*AgentObjectRule)(nil).PermitsTransition(st.Ready, st.Done, st) {
		t.Fatal("terminal edge should be allowed from a pickup status with an empty matrix")
	}
	if !(*AgentObjectRule)(nil).PermitsTransition(st.Blocked, st.Review, st) {
		t.Fatal("review edge should be allowed with an empty matrix")
	}
	if (*AgentObjectRule)(nil).PermitsTransition(st.Ready, st.Revision, st) {
		t.Fatal("non-terminal edge should be rejected with an empty matrix")
	}
	r := &AgentObjectRule{Transitions: []AgentTransition{{From: st.InProgress, To: st.Revision}}}
	if !r.PermitsTransition(st.InProgress, st.Revision, st) {
		t.Fatal("declared edge should be allowed")
	}
	if r.PermitsTransition(st.InProgress, st.Done, st) {
		t.Fatal("undeclared edge should be rejected when the matrix is non-empty")
	}
	if r.PermitsTransition(st.InProgress, st.InProgress, st) {
		t.Fatal("self transition should be rejected")
	}
}

func TestMatchWorkTrigger(t *testing.T) {
	def := &AgentDefinition{WorkConfig: AgentWorkConfig{ObjectRules: []AgentObjectRule{{
		ObjectType: "Task",
		Triggers:   []AgentWorkTrigger{{Status: "ready", Prompt: "do it"}},
	}}}}
	if tr := def.MatchWorkTrigger("Task", "ready"); tr == nil || tr.Prompt != "do it" {
		t.Fatalf("matching trigger = %#v", tr)
	}
	if tr := def.MatchWorkTrigger("Task", "done"); tr != nil {
		t.Fatalf("unexpected trigger for done: %#v", tr)
	}
	if tr := def.MatchWorkTrigger("Bug", "ready"); tr != nil {
		t.Fatalf("unexpected trigger for other type: %#v", tr)
	}
}

func TestObjectRuleAllowsTriState(t *testing.T) {
	var r AgentObjectRule
	if !r.AllowsCreate() || !r.AllowsUpdate() || !r.AllowsDelete() {
		t.Fatal("nil flags must allow")
	}
	no := false
	r.CanUpdate = &no
	if r.AllowsUpdate() {
		t.Fatal("explicit false must deny")
	}
	if !r.AllowsCreate() {
		t.Fatal("other actions must stay allowed")
	}
}
