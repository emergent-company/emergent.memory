package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// approvalsPageBody renders GET /settings/approvals for the fixture and returns
// the body, failing on a non-200.
func approvalsPageBody(t *testing.T, f *fakeMemory) string {
	t.Helper()
	_, e := newTestServer(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/approvals", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// TestApprovalsPageRendersQuestionSessionLink pins the tool-approval-policy
// requirement "Pending question links to its session": a pending question whose
// run belongs to a chat conversation must render a session link to
// /chat?c=<conversationId> with a stable testid, so an operator can jump from a
// question to the conversation waiting on its answer.
func TestApprovalsPageRendersQuestionSessionLink(t *testing.T) {
	body := approvalsPageBody(t, &fakeMemory{questions: []AgentQuestionItem{{
		ID:              "q-chat",
		Question:        "Which environment?",
		Status:          "pending",
		InteractionType: "text",
		ConversationID:  "conv-1",
	}}})

	for _, want := range []string{
		`data-testid="question-session-link"`,
		`href="/chat?c=conv-1"`,
		"lucide--messages-square",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("pending question session link missing %q:\n%s", want, body)
		}
	}
}

// TestApprovalsPageOmitsQuestionSessionLinkWithoutConversation guards the
// complement: a question whose run has no chat conversation (for example a
// scheduled run) renders without a session link.
func TestApprovalsPageOmitsQuestionSessionLinkWithoutConversation(t *testing.T) {
	body := approvalsPageBody(t, &fakeMemory{questions: []AgentQuestionItem{{
		ID:              "q-scheduled",
		Question:        "What should I name it?",
		Status:          "pending",
		InteractionType: "text",
	}}})

	if strings.Contains(body, `data-testid="question-session-link"`) {
		t.Errorf("question must not render a session link without a conversation:\n%s", body)
	}
	// The question itself still renders.
	if !strings.Contains(body, "What should I name it?") {
		t.Errorf("question not rendered:\n%s", body)
	}
}

// TestApprovalsPageApprovalSessionLinkHasTestID gives the pre-existing approval
// audit link its stable testid, matching the question link.
func TestApprovalsPageApprovalSessionLinkHasTestID(t *testing.T) {
	body := approvalsPageBody(t, &fakeMemory{approvals: []ToolApprovalItem{{
		ID:             "a1",
		QuestionID:     "q1",
		ToolName:       "create_note",
		Decision:       "approved",
		ConversationID: "conv-2",
	}}})

	for _, want := range []string{
		`data-testid="approval-session-link"`,
		`href="/chat?c=conv-2"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("approval session link missing %q:\n%s", want, body)
		}
	}
}
