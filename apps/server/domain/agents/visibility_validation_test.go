package agents

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// TestNormalizeVisibility pins the canonical server-side mapping: only the
// three enum levels pass, empty/whitespace normalizes to project (the server
// default), mixed-case/whitespace-padded input is canonicalized, and anything
// else is rejected so it can never be persisted (issue #889).
func TestNormalizeVisibility(t *testing.T) {
	cases := []struct {
		in   AgentVisibility
		want AgentVisibility
		ok   bool
	}{
		{in: VisibilityProject, want: VisibilityProject, ok: true},
		{in: VisibilityExternal, want: VisibilityExternal, ok: true},
		{in: VisibilityInternal, want: VisibilityInternal, ok: true},
		{in: "External", want: VisibilityExternal, ok: true},
		{in: "  internal  ", want: VisibilityInternal, ok: true},
		{in: "", want: VisibilityProject, ok: true},
		{in: "   ", want: VisibilityProject, ok: true},
		{in: "public", want: "", ok: false},
		{in: "bogus", want: "", ok: false},
	}
	for _, tc := range cases {
		t.Run(string(tc.in), func(t *testing.T) {
			got, ok := NormalizeVisibility(tc.in)
			if ok != tc.ok || got != tc.want {
				t.Errorf("NormalizeVisibility(%q) = (%q, %v), want (%q, %v)", tc.in, got, ok, tc.want, tc.ok)
			}
		})
	}
}

// TestCreateDefinition_InvalidVisibilityReturns400 is the fail-first guard for
// the write path: an out-of-enum visibility ("public") must be refused with a
// 400 before any repository call, instead of being silently persisted. It runs
// against a nil-repo handler, so reaching a 400 proves validation fires first.
func TestCreateDefinition_InvalidVisibilityReturns400(t *testing.T) {
	h := newTestHandler()
	body := `{"name":"test-agent","visibility":"public"}`
	c, _ := newEchoContextWithUser(http.MethodPost, "/api/projects/proj-test-id/agent-definitions", body)

	err := h.CreateDefinition(c)

	require.Error(t, err)
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr, "expected *apperror.Error, got %T: %v", err, err)
	if appErr.HTTPStatus != http.StatusBadRequest {
		t.Fatalf("invalid visibility status = %d, want 400", appErr.HTTPStatus)
	}
	if !strings.Contains(appErr.Message, "visibility") {
		t.Fatalf("invalid visibility message must mention visibility, got %q", appErr.Message)
	}
}
