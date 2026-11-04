package agents

import "testing"

// A resumed run must inherit the work-item link. HasLiveRunForSubject only
// counts queued/running/paused runs, and MarkRunResumed finalizes the paused
// parent — without the subject on the child, the work reaper would see nothing
// live and re-dispatch the item while the resumed run is still executing.
func TestNewAgentRun_CarriesSubjectObject(t *testing.T) {
	subjectID := "0b4c9a4e-8f4d-4a6b-9f36-4d1e0d6f2a11"
	subjectType := "work_item"

	run := newAgentRun(CreateRunOptions{
		AgentID:           "agent-1",
		SubjectObjectID:   &subjectID,
		SubjectObjectType: &subjectType,
	})

	if run.SubjectObjectID == nil || *run.SubjectObjectID != subjectID {
		t.Fatalf("SubjectObjectID = %v, want %q", run.SubjectObjectID, subjectID)
	}
	if run.SubjectObjectType == nil || *run.SubjectObjectType != subjectType {
		t.Fatalf("SubjectObjectType = %v, want %q", run.SubjectObjectType, subjectType)
	}
}
