package agents

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// TestCreateDefinition_InvalidModelReturns400 is the fail-first guard for the
// agent-definition write path: a malformed model.name (empty provider segment,
// no provider segment, or empty model segment) must be refused with a 400
// before any repository call, instead of being persisted verbatim. It runs
// against a nil-repo handler, so reaching a 400 proves validation fires first.
func TestCreateDefinition_InvalidModelReturns400(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{name: "empty provider segment", body: `{"name":"a","model":{"name":"/deepseek-v4-flash"}}`},
		{name: "no provider segment", body: `{"name":"a","model":{"name":"deepseek-v4-flash"}}`},
		{name: "empty model segment", body: `{"name":"a","model":{"name":"openai/"}}`},
		// #1466: doubled separator must be rejected at the write boundary.
		{name: "double separator", body: `{"name":"a","model":{"name":"openai//gpt-4o"}}`},
		// #1467: provider without a model name is not a valid structured ref.
		{name: "provider without model", body: `{"name":"a","model":{"provider":"openai"}}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestHandler()
			c, _ := newEchoContextWithUser(http.MethodPost, "/api/projects/proj-test-id/agent-definitions", tc.body)

			err := h.CreateDefinition(c)

			require.Error(t, err)
			var appErr *apperror.Error
			require.ErrorAs(t, err, &appErr, "expected *apperror.Error, got %T: %v", err, err)
			if appErr.HTTPStatus != http.StatusBadRequest {
				t.Fatalf("invalid model status = %d, want 400", appErr.HTTPStatus)
			}
			if !strings.Contains(appErr.Message, "provider/model") {
				t.Fatalf("invalid model message must mention 'provider/model', got %q", appErr.Message)
			}
		})
	}
}

// TestUpdateDefinition_InvalidModelReturns400 mirrors the create-path guard for
// PATCH: malformed model.name references (empty provider segment, no provider
// segment, or empty model segment) must be refused with a 400 before the
// repository lookup, not persisted.
func TestUpdateDefinition_InvalidModelReturns400(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{name: "empty provider segment", body: `{"model":{"name":"/deepseek-v4-flash"}}`},
		{name: "no provider segment", body: `{"model":{"name":"deepseek-v4-flash"}}`},
		{name: "empty model segment", body: `{"model":{"name":"openai/"}}`},
		// #1466: doubled separator must be rejected at the write boundary.
		{name: "double separator", body: `{"model":{"name":"openai//gpt-4o"}}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestHandler()
			c, _ := newEchoContextWithUser(http.MethodPatch, "/api/projects/proj-test-id/agent-definitions/00000000-0000-0000-0000-000000000000", tc.body)
			c.SetParamNames("id")
			c.SetParamValues("00000000-0000-0000-0000-000000000000")

			err := h.UpdateDefinition(c)

			require.Error(t, err)
			var appErr *apperror.Error
			require.ErrorAs(t, err, &appErr, "expected *apperror.Error, got %T: %v", err, err)
			if appErr.HTTPStatus != http.StatusBadRequest {
				t.Fatalf("invalid model status = %d, want 400", appErr.HTTPStatus)
			}
			if !strings.Contains(appErr.Message, "provider/model") {
				t.Fatalf("invalid model message must mention 'provider/model', got %q", appErr.Message)
			}
		})
	}
}
