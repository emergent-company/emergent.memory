package extraction

import (
	"context"
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/pkg/branchorigin"
)

// TestExtractionBranchRequestAttribution pins the attribution the extraction
// worker hands its staging branch: origin=extraction, the job id as origin_id,
// and the source document's filename as the label the UI shows instead of the
// opaque extraction/<doc>/<job> name (#242). The id is also what cleanup uses to
// find a deleted document's branches (#246), so it must be present.
func TestExtractionBranchRequestAttribution(t *testing.T) {
	docID := "aaaaaaaa-0000-0000-0000-000000000001"
	job := &ObjectExtractionJob{
		ID:         "bbbbbbbb-0000-0000-0000-000000000002",
		ProjectID:  "cccccccc-0000-0000-0000-000000000003",
		DocumentID: &docID,
	}
	label := "Invoice.pdf"

	req := extractionBranchRequest(job, "extraction/aaaa/bbbb", &label)

	if req.Name != "extraction/aaaa/bbbb" {
		t.Errorf("Name = %q", req.Name)
	}
	if req.ProjectID == nil || *req.ProjectID != job.ProjectID {
		t.Errorf("ProjectID = %v, want %q", req.ProjectID, job.ProjectID)
	}
	if req.Description == nil || !strings.Contains(*req.Description, job.ID) {
		t.Errorf("Description = %v, want it to name the job", req.Description)
	}
	if req.Attribution == nil {
		t.Fatal("Attribution is nil; the branch would be stored as manual")
	}
	if req.Attribution.Origin != branchorigin.Extraction {
		t.Errorf("Origin = %q, want %q", req.Attribution.Origin, branchorigin.Extraction)
	}
	if req.Attribution.ID == nil || *req.Attribution.ID != job.ID {
		t.Errorf("OriginID = %v, want %q", req.Attribution.ID, job.ID)
	}
	if req.Attribution.Label == nil || *req.Attribution.Label != label {
		t.Errorf("OriginLabel = %v, want %q", req.Attribution.Label, label)
	}
}

// TestStagingBranchLabelNilSafe proves the label lookup is best-effort: a worker
// without a document service, a job without a document, or a document without a
// filename must all yield nil rather than failing branch creation (the branch is
// created even when extraction later fails, and a missing label only costs a UI
// fallback).
func TestStagingBranchLabelNilSafe(t *testing.T) {
	w := &ObjectExtractionWorker{}
	docID := "aaaaaaaa-0000-0000-0000-000000000001"

	if got := w.stagingBranchLabel(context.Background(), &ObjectExtractionJob{ID: "job"}); got != nil {
		t.Errorf("no document service: label = %q, want nil", *got)
	}
	if got := w.stagingBranchLabel(context.Background(), &ObjectExtractionJob{ID: "job", DocumentID: &docID}); got != nil {
		t.Errorf("no document service: label = %q, want nil", *got)
	}
}
