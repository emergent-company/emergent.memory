package agents

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/session"
)

// TestParseRelationships_DegradesInsteadOfFailing pins the nil-event fix: the
// relationship pass must never fail the pipeline when it produced nothing or
// malformed JSON, because entities are already extracted at that point.
func TestParseRelationships_DegradesInsteadOfFailing(t *testing.T) {
	cases := []struct {
		name      string
		raw       any
		wantRels  int
		wantIssue bool
	}{
		{name: "nil", raw: nil, wantIssue: true},
		{name: "empty string", raw: "", wantIssue: true},
		{name: "malformed string", raw: `{"relationships": "broken`, wantIssue: true},
		{name: "wrong shape", raw: map[string]any{"relationships": "nope"}, wantIssue: true},
		{name: "empty list", raw: []ExtractedRelationship{}, wantRels: 0},
		{
			name: "valid output",
			raw: &RelationshipExtractionOutput{Relationships: []ExtractedRelationship{
				{SourceRef: "a", TargetRef: "b", Type: "REL"},
			}},
			wantRels: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rels, issue := parseRelationships(tc.raw)
			if len(rels) != tc.wantRels {
				t.Fatalf("relationships = %d, want %d", len(rels), tc.wantRels)
			}
			if (issue != "") != tc.wantIssue {
				t.Fatalf("issue = %q, wantIssue = %v", issue, tc.wantIssue)
			}
		})
	}
}

type fakeInvocation struct {
	agent.StrictContextMock
	sess session.Session
	id   string
}

func (f *fakeInvocation) Session() session.Session { return f.sess }
func (f *fakeInvocation) InvocationID() string     { return f.id }

// TestRelationshipProcessorYieldsEventWithoutState runs the real processor
// body, not just the parsing helper: an empty session must produce a non-nil
// event, because the ADK runner treats a nil event as a fatal pipeline error.
func TestRelationshipProcessorYieldsEventWithoutState(t *testing.T) {
	ctx := context.Background()
	sessSvc := session.InMemoryService()
	resp, err := sessSvc.Create(ctx, &session.CreateRequest{AppName: "test", UserID: "u"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	p := &ExtractionPipeline{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	fake := &fakeInvocation{
		StrictContextMock: agent.NewStrictContextMock(ctx),
		sess:              resp.Session,
		id:                "inv-1",
	}

	event, err := p.buildRelationshipProcessorEvent(fake)
	if err != nil {
		t.Fatalf("processor: %v", err)
	}
	if event == nil {
		t.Fatal("processor returned a nil event")
	}
	if event.Actions.StateDelta["extracted_relationships"] == nil {
		t.Fatal("processor did not persist the degraded relationship set")
	}
}
