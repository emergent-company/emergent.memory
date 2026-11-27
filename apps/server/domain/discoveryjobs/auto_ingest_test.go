package discoveryjobs

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/domain/extraction"
	"github.com/emergent-company/emergent.memory/domain/pipeline"
)

type ingestPolicyReader struct{ mode string }

func (f ingestPolicyReader) Policy(context.Context, uuid.UUID) (pipeline.Policy, error) {
	return pipeline.Policy{Mode: f.mode, Extract: pipeline.ExtractPolicy{BackfillLimitPerRun: 50}}, nil
}

func (f ingestPolicyReader) BumpSchemaGeneration(context.Context, uuid.UUID) error { return nil }

type recordingBackfill struct{ filters []extraction.BackfillFilter }

func (r *recordingBackfill) Backfill(_ context.Context, _ string, filter extraction.BackfillFilter, _ int, _ bool) (*extraction.BackfillResult, error) {
	r.filters = append(r.filters, filter)
	return &extraction.BackfillResult{Enqueued: 1}, nil
}

// TestMaybeAutoIngestExtractsNeverExtractedInAutoMode pins the P3 fix: in auto
// mode, discovery enqueues never-extracted documents regardless of a schema
// change, so a new document that introduces no new types still reaches the graph.
func TestMaybeAutoIngestExtractsNeverExtractedInAutoMode(t *testing.T) {
	bf := &recordingBackfill{}
	svc := &Service{policyReader: ingestPolicyReader{mode: pipeline.ModeAuto}, backfill: bf, log: quietLogger()}
	svc.maybeAutoIngest(context.Background(), uuid.New())
	if len(bf.filters) != 1 || bf.filters[0] != extraction.BackfillNever {
		t.Fatalf("filters = %v, want one BackfillNever", bf.filters)
	}
}

func TestMaybeAutoIngestNoopOutsideAutoMode(t *testing.T) {
	bf := &recordingBackfill{}
	svc := &Service{policyReader: ingestPolicyReader{mode: pipeline.ModeSuggest}, backfill: bf, log: quietLogger()}
	svc.maybeAutoIngest(context.Background(), uuid.New())
	if len(bf.filters) != 0 {
		t.Fatalf("no ingest expected outside auto mode, got %v", bf.filters)
	}
}

func TestMaybeAutoIngestNilDeps(t *testing.T) {
	// No policy reader / backfill → no panic, no-op.
	(&Service{log: quietLogger()}).maybeAutoIngest(context.Background(), uuid.New())
}
