package tracing

import (
	"strings"
	"testing"
)

func TestScopeTraceQLEmpty(t *testing.T) {
	got, err := scopeTraceQL("", "proj-1")
	if err != nil {
		t.Fatalf("scopeTraceQL empty: unexpected error %v", err)
	}
	want := `{ ( .memory.project.id = "proj-1" || .emergent.project.id = "proj-1" ) }`
	if got != want {
		t.Fatalf("scopeTraceQL(empty) = %q, want %q", got, want)
	}
}

func TestScopeTraceQLBracedCondition(t *testing.T) {
	got, err := scopeTraceQL(`{ rootName = "agent.run" }`, "proj-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, `( .memory.project.id = "proj-1" || .emergent.project.id = "proj-1" ) && ( rootName = "agent.run" )`) {
		t.Fatalf("scopeTraceQL(braced) = %q, missing atomic project AND", got)
	}
}

func TestScopeTraceQLBracedWithPipeline(t *testing.T) {
	got, err := scopeTraceQL(`{ rootName = "agent.run" } | select(span.memory.agent.run_id)`, "proj-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, `&& ( rootName = "agent.run" ) } | select(span.memory.agent.run_id)`) {
		t.Fatalf("scopeTraceQL(pipeline) = %q, pipeline not preserved", got)
	}
}

func TestScopeTraceQLBareCondition(t *testing.T) {
	got, err := scopeTraceQL(`.service.name = "api"`, "proj-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, `&& ( .service.name = "api" )`) {
		t.Fatalf("scopeTraceQL(bare) = %q, missing AND", got)
	}
}

func TestScopeTraceQLRejectsInjectionVectors(t *testing.T) {
	cases := []struct {
		name string
		q    string
	}{
		{"paren escape + OR negation", `{ .foo = "bar") || .memory.project.id != "proj-1" }`},
		{"balanced paren escape targeting foreign tenant", `{ .foo = "bar") || .memory.project.id != "proj-1" && (.memory.project.id = "proj-2" }`},
		{"empty block then OR foreign", `{ } || .memory.project.id != "proj-1"`},
		{"top-level OR into pipeline", `{ rootName = "agent.run" } | select(span.a) || .memory.project.id = "proj-2"`},
		{"OR operator inside condition", `{ .memory.project.id = "proj-1" || .memory.project.id = "proj-2" }`},
		{"unterminated string", `{ .foo = "bar }`},
		{"unbalanced brace", `{ .foo = "bar"`},
	}
	for _, tc := range cases {
		if _, err := scopeTraceQL(tc.q, "proj-1"); err == nil {
			t.Errorf("%s: expected rejection, got nil error", tc.name)
		}
	}
}

func TestScopeTraceQLAllowsSafeAndOnly(t *testing.T) {
	// A well-formed AND-only query referencing a foreign project must still be
	// accepted (the project predicate gates it), not rejected.
	got, err := scopeTraceQL(`{ .memory.project.id = "proj-2" && .service.name = "api" }`, "proj-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, `&& ( .memory.project.id = "proj-2" && .service.name = "api" )`) {
		t.Fatalf("scopeTraceQL(foreign-and) = %q", got)
	}
}
