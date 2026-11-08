package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// --- MemoryClient skill-capture proposal methods ---

func TestListSkillCaptureProposalsParamsAndDecode(t *testing.T) {
	var gotPath, gotMethod, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod, gotQuery = r.URL.Path, r.Method, r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"proposals":[
			{"id":"p1","projectId":"proj-1","agentId":"a1","agentName":"triage-agent","kind":"create","name":"fix-flaky-test","description":"Reruns a flaky suite","content":"You rerun…","confidence":0.82,"source":"background","status":"pending","autoApplied":false,"findings":[{"severity":"warning","code":"skill.too_short","message":"Add more detail"}],"createdAt":"2026-11-01T10:00:00Z"}
		],"count":1}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj-1")
	got, err := m.ListSkillProposals(context.Background(), captureProposalPending, 200)
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodGet || gotPath != "/api/projects/proj-1/skills/proposals" {
		t.Errorf("request = %s %s", gotMethod, gotPath)
	}
	if !strings.Contains(gotQuery, "status=pending") || !strings.Contains(gotQuery, "limit=200") {
		t.Errorf("query = %q, want status+limit", gotQuery)
	}
	if len(got) != 1 {
		t.Fatalf("got %d proposals, want 1", len(got))
	}
	p := got[0]
	if p.ID != "p1" || p.Kind != "create" || p.AgentName != "triage-agent" || p.Status != captureProposalPending {
		t.Errorf("proposal = %+v", p)
	}
	if p.Confidence != 0.82 {
		t.Errorf("confidence = %v", p.Confidence)
	}
	if len(p.Findings) != 1 || p.Findings[0].Code != "skill.too_short" || p.Findings[0].Severity != "warning" {
		t.Errorf("findings = %+v", p.Findings)
	}
}

func TestListSkillCaptureProposalsOmitsEmptyFilters(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"proposals":[],"count":0}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj-1")
	if _, err := m.ListSkillProposals(context.Background(), "", 0); err != nil {
		t.Fatal(err)
	}
	if gotQuery != "" {
		t.Errorf("query = %q, want empty", gotQuery)
	}
}

func TestGetSkillCaptureProposalDecodesBareDTO(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"p1","projectId":"proj-1","kind":"patch","name":"fix-flaky-test","content":"merged body","patchOld":"old line","patchNew":"new line","status":"pending","confidence":0.5,"source":"background","createdAt":"2026-11-01T10:00:00Z"}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj-1")
	p, err := m.GetSkillProposal(context.Background(), "p1")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/projects/proj-1/skills/proposals/p1" {
		t.Errorf("path = %q", gotPath)
	}
	if p.Kind != "patch" || p.PatchOld != "old line" || p.Content != "merged body" {
		t.Errorf("proposal = %+v", p)
	}
}

func TestApproveAndRejectSkillCaptureProposal(t *testing.T) {
	var gotPath, gotMethod, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj-1")
	if err := m.ApproveSkillProposal(context.Background(), "p1", "looks right"); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/projects/proj-1/skills/proposals/p1/approve" {
		t.Errorf("approve request = %s %s", gotMethod, gotPath)
	}
	if !strings.Contains(gotBody, `"note":"looks right"`) {
		t.Errorf("approve body = %s", gotBody)
	}

	if err := m.RejectSkillProposal(context.Background(), "p2", ""); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/projects/proj-1/skills/proposals/p2/reject" {
		t.Errorf("reject path = %q", gotPath)
	}
}

// --- render models ---

func samplePendingCaptureProposal() SkillCaptureProposal {
	return SkillCaptureProposal{
		ID:          "p1",
		ProjectID:   "proj-1",
		AgentID:     "a1",
		AgentName:   "triage-agent",
		Kind:        "create",
		Name:        "fix-flaky-test",
		Description: "Reruns a flaky suite before reporting failure",
		Content:     "When a test fails, rerun the suite once before reporting.\n\n## Steps\n\n1. Rerun the failing suite.",
		Evidence:    "The agent reran the suite twice across three runs and both eventually passed.",
		Confidence:  0.82,
		Source:      "background",
		Status:      captureProposalPending,
		CreatedAt:   "2026-11-01T10:00:00Z",
		Findings: []SkillCaptureFinding{
			{Severity: "warning", Code: "skill.too_short", Message: "Add more detail to the instructions."},
		},
	}
}

func TestRenderSkillCaptureProposalsPage(t *testing.T) {
	p := samplePendingCaptureProposal()
	html := renderHTML(t, SkillCaptureProposalsPage([]SkillCaptureProposal{p}, captureProposalPending, "suggest", nil, "", nil))
	for _, want := range []string{
		"Skill proposals", "fix-flaky-test", "Reruns a flaky suite before reporting failure",
		`href="/skills/proposals/p1"`, "New skill", "82% confident", "triage-agent",
		"from a background review", "1 lint", "Pending",
		`href="/skills/proposals?status=all"`, "1 proposal",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("proposals page missing %q", want)
		}
	}
}

func TestRenderSkillCaptureProposalsEmptyStates(t *testing.T) {
	pending := renderHTML(t, SkillCaptureProposalsPage(nil, captureProposalPending, "suggest", nil, "", nil))
	if !strings.Contains(pending, "No proposals waiting") {
		t.Error("pending empty state missing")
	}
	if strings.Contains(pending, "Skill capture is off") {
		t.Error("capture-off note rendered while capture is on")
	}

	off := renderHTML(t, SkillCaptureProposalsPage(nil, captureProposalPending, "off", nil, "", nil))
	if !strings.Contains(off, "Skill capture is off for this project") {
		t.Error("capture-off note missing when mode is off")
	}

	applied := renderHTML(t, SkillCaptureProposalsPage(nil, captureProposalApplied, "off", nil, "", nil))
	if !strings.Contains(applied, "No proposals here") {
		t.Error("filtered empty state missing")
	}
	if strings.Contains(applied, "Skill capture is off for this project") {
		t.Error("capture-off note should only explain the pending view")
	}
}

func TestRenderSkillCaptureProposalDetailPending(t *testing.T) {
	p := samplePendingCaptureProposal()
	html := renderHTML(t, SkillCaptureProposalDetailPage(&p, "suggest", nil, "", nil))
	for _, want := range []string{
		"fix-flaky-test", "Reruns a flaky suite before reporting failure",
		"New skill", "Pending", "82% confident", "from triage-agent",
		"Skill content", "Rerun the failing suite.",
		"Why it was proposed", "reran the suite twice",
		"Lint findings", "skill.too_short", "Add more detail to the instructions.",
		`method="post"`, `action="/skills/proposals/p1/approve"`,
		`formaction="/skills/proposals/p1/reject"`,
		"Approve and add skill", "Review note",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("proposal detail missing %q", want)
		}
	}
	if strings.Contains(html, "Outcome") {
		t.Error("outcome block rendered for a pending proposal")
	}
}

func TestRenderSkillCaptureProposalDetailPatchShowsChange(t *testing.T) {
	p := samplePendingCaptureProposal()
	p.Kind = "patch"
	p.PatchOld = "old instruction"
	p.PatchNew = "new instruction"
	html := renderHTML(t, SkillCaptureProposalDetailPage(&p, "suggest", nil, "", nil))
	for _, want := range []string{"Proposed change", "Before", "After", "old instruction", "new instruction", "Approve and update skill"} {
		if !strings.Contains(html, want) {
			t.Errorf("patch detail missing %q", want)
		}
	}
}

func TestRenderSkillCaptureProposalDetailDelete(t *testing.T) {
	p := samplePendingCaptureProposal()
	p.Kind = "delete"
	p.Content = ""
	html := renderHTML(t, SkillCaptureProposalDetailPage(&p, "suggest", nil, "", nil))
	for _, want := range []string{"Proposed deletion", "Approve and delete skill"} {
		if !strings.Contains(html, want) {
			t.Errorf("delete detail missing %q", want)
		}
	}
	if strings.Contains(html, "Skill content") {
		t.Error("content block rendered for a delete proposal")
	}
}

func TestRenderSkillCaptureProposalDetailAppliedShowsOutcome(t *testing.T) {
	p := samplePendingCaptureProposal()
	p.Status = captureProposalApplied
	p.AppliedAt = "2026-11-02T09:00:00Z"
	p.AutoApplied = true
	p.ReviewNote = "Matches how we handle retries."
	html := renderHTML(t, SkillCaptureProposalDetailPage(&p, "suggest", nil, "", nil))
	for _, want := range []string{"Outcome", "Applied", "auto-applied", "View in skills", "Matches how we handle retries."} {
		if !strings.Contains(html, want) {
			t.Errorf("applied detail missing %q", want)
		}
	}
	if strings.Contains(html, "formaction=") {
		t.Error("decision form rendered for a settled proposal")
	}
}

func TestRenderSkillCaptureProposalDetailError(t *testing.T) {
	html := renderHTML(t, SkillCaptureProposalDetailPage(nil, "", errors.New("proposal not found"), "", nil))
	if !strings.Contains(html, "Proposal unavailable") {
		t.Error("error state missing")
	}
}

// --- handlers ---

func TestUISkillCaptureProposalsDefaultFilter(t *testing.T) {
	f := &fakeMemory{proposals: []SkillCaptureProposal{samplePendingCaptureProposal()}}
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	e.GET("/skills/proposals", s.uiSkillCaptureProposals)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/skills/proposals", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if f.proposalStatus != captureProposalPending {
		t.Errorf("status filter = %q, want pending", f.proposalStatus)
	}
	if !strings.Contains(rec.Body.String(), "fix-flaky-test") {
		t.Error("body missing the proposal")
	}
}

func TestUISkillCaptureProposalsFilterAndFlash(t *testing.T) {
	f := &fakeMemory{proposals: []SkillCaptureProposal{samplePendingCaptureProposal()}}
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	e.GET("/skills/proposals", s.uiSkillCaptureProposals)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/skills/proposals?status=applied&approved=1", nil))
	if f.proposalStatus != captureProposalApplied {
		t.Errorf("status filter = %q, want applied", f.proposalStatus)
	}
	if !strings.Contains(rec.Body.String(), "Proposal approved") {
		t.Error("approval flash missing")
	}
}

func TestUISkillCaptureProposalsLoadError(t *testing.T) {
	f := &fakeMemory{proposalErr: errors.New("memory down")}
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	e.GET("/skills/proposals", s.uiSkillCaptureProposals)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/skills/proposals", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Failed to load proposals") {
		t.Error("load error notice missing")
	}
}

func TestUISkillCaptureProposalDetail(t *testing.T) {
	p := samplePendingCaptureProposal()
	f := &fakeMemory{proposalByID: map[string]*SkillCaptureProposal{"p1": &p}}
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	e.GET("/skills/proposals/:id", s.uiSkillCaptureProposal)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/skills/proposals/p1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Approve and add skill") {
		t.Error("detail body missing the decision form")
	}

	// An unknown id renders the error state rather than a 500.
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/skills/proposals/missing", nil))
	if !strings.Contains(rec.Body.String(), "Proposal unavailable") {
		t.Error("missing proposal should render the error state")
	}
}

func TestUISkillCaptureProposalApprove(t *testing.T) {
	p := samplePendingCaptureProposal()
	f := &fakeMemory{proposalByID: map[string]*SkillCaptureProposal{"p1": &p}}
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	e.POST("/skills/proposals/:id/approve", s.uiApproveSkillCaptureProposal)

	form := url.Values{"note": {"matches our retry policy"}}
	req := httptest.NewRequest(http.MethodPost, "/skills/proposals/p1/approve", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/skills/proposals/p1?approved=1" {
		t.Errorf("location = %q", loc)
	}
	if f.approvedProposalID != "p1" {
		t.Errorf("approved id = %q", f.approvedProposalID)
	}
	if f.proposalNote != "matches our retry policy" {
		t.Errorf("note = %q", f.proposalNote)
	}
}

func TestUISkillCaptureProposalApproveError(t *testing.T) {
	f := &fakeMemory{proposalApproveErr: errors.New("security scan blocked the write")}
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	e.POST("/skills/proposals/:id/approve", s.uiApproveSkillCaptureProposal)

	req := httptest.NewRequest(http.MethodPost, "/skills/proposals/p1/approve", strings.NewReader("note="))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	loc := rec.Header().Get("Location")
	// A locally-authored error is redacted to the generic message on the way
	// out; a memory 4xx keeps its own copy (see userFacingMemoryError).
	if !strings.Contains(loc, "/skills/proposals/p1?") || !strings.Contains(loc, "err=Something+went+wrong") {
		t.Errorf("location = %q, want an err flash on the detail path", loc)
	}
	if f.approvedProposalID != "" {
		t.Error("approval recorded despite the error")
	}
}

func TestUISkillCaptureProposalReject(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	e.POST("/skills/proposals/:id/reject", s.uiRejectSkillCaptureProposal)

	req := httptest.NewRequest(http.MethodPost, "/skills/proposals/p9/reject", strings.NewReader("note=not+worth+keeping"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/skills/proposals/p9?rejected=1" {
		t.Errorf("location = %q", loc)
	}
	if f.rejectedProposalID != "p9" || f.proposalNote != "not worth keeping" {
		t.Errorf("reject = %q note %q", f.rejectedProposalID, f.proposalNote)
	}
}

// TestSkillsHeaderProposalCount pins the inbox link and its pending badge: the
// count is the only discoverability signal on the skills page.
func TestSkillsHeaderProposalCount(t *testing.T) {
	html := renderHTML(t, SkillsPage(nil, nil, "", nil, "", nil, 3))
	for _, want := range []string{
		`href="/skills/proposals"`, "Proposals", "3 awaiting review", "Add skill",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("skills header missing %q", want)
		}
	}
	none := renderHTML(t, SkillsPage(nil, nil, "", nil, "", nil, 0))
	if !strings.Contains(none, "none waiting") {
		t.Error("aria label should say nothing is waiting")
	}
}

// TestCaptureDecisionError pins what a failed approve/reject shows: the memory
// 4xx copy (actionable — "proposal is not pending") but never the raw
// host-shaped error, and the generic/unavailable copy otherwise.
func TestCaptureDecisionError(t *testing.T) {
	got := captureDecisionError(&memoryHTTPError{
		Status:  http.StatusConflict,
		Code:    "conflict",
		Message: "proposal is not pending",
	}).Error()
	if got != "proposal is not pending" {
		t.Errorf("4xx message = %q, want the backend copy", got)
	}

	if got := captureDecisionError(errors.New("boom")).Error(); got != genericErrorMessage {
		t.Errorf("local error = %q, want %q", got, genericErrorMessage)
	}

	if got := captureDecisionError(&memoryHTTPError{Status: http.StatusBadGateway, Message: "down"}).Error(); !strings.Contains(got, "temporarily unavailable") {
		t.Errorf("5xx message = %q, want the unavailable copy", got)
	}
}

func TestRenderSkillCaptureProposalsFilterLabels(t *testing.T) {
	html := renderHTML(t, SkillCaptureProposalsPage(nil, captureProposalApplied, "suggest", nil, "", nil))
	for _, want := range []string{">Pending<", ">Applied<", ">Rejected<", ">Failed<", ">All<"} {
		if !strings.Contains(html, want) {
			t.Errorf("filter strip missing %q", want)
		}
	}
	if strings.Contains(html, "Pendings") || strings.Contains(html, "Applieds") {
		t.Error("filter labels pluralised a status adjective")
	}
}

// TestRenderSkillCaptureProposalApplyingIsNotActionable pins the race guard: a
// row another reviewer already claimed must not offer the buttons again.
func TestRenderSkillCaptureProposalApplyingIsNotActionable(t *testing.T) {
	p := samplePendingCaptureProposal()
	p.Status = captureProposalApplying
	html := renderHTML(t, SkillCaptureProposalDetailPage(&p, "suggest", nil, "", nil))
	if strings.Contains(html, "formaction=") {
		t.Error("decision form rendered for a row that is being applied")
	}
	if !strings.Contains(html, "Applying") {
		t.Error("applying status missing from the outcome block")
	}
}
