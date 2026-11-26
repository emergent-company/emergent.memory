package chat

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/agents"
)

// TestMapAsyncRunCreateError_BudgetExceeded pins the mapping the agents and
// agentcompat handlers already use: a project at its budget window gets a 402
// with the product's error code, not a 500 "internal error". The async remember
// / remember file / forget paths pre-create their run through
// CreateRunWithOptions, which runs the budget gate, so this is the error they
// can actually receive.
func TestMapAsyncRunCreateError_BudgetExceeded(t *testing.T) {
	appErr := mapAsyncRunCreateError(&agents.BudgetExceededError{
		ProjectID: "proj-123",
		Message:   "budget exceeded",
	})
	require.NotNil(t, appErr)
	assert.Equal(t, http.StatusPaymentRequired, appErr.HTTPStatus)
	assert.Equal(t, "budget_exceeded", appErr.Code)
	assert.Contains(t, appErr.Message, "budget")
}

// TestMapAsyncRunCreateError_WrappedBudgetExceeded confirms the typed error is
// found through a wrap, which is how the repository returns it from the gate.
func TestMapAsyncRunCreateError_WrappedBudgetExceeded(t *testing.T) {
	wrapped := fmt.Errorf("create run: %w", &agents.BudgetExceededError{ProjectID: "p1"})
	appErr := mapAsyncRunCreateError(wrapped)
	require.NotNil(t, appErr)
	assert.Equal(t, http.StatusPaymentRequired, appErr.HTTPStatus)
	assert.Equal(t, "budget_exceeded", appErr.Code)
}

// TestMapAsyncRunCreateError_OtherFailure keeps an unrelated failure a 500: the
// budget branch must not swallow database errors as a payment problem.
func TestMapAsyncRunCreateError_OtherFailure(t *testing.T) {
	appErr := mapAsyncRunCreateError(assert.AnError)
	require.NotNil(t, appErr)
	assert.Equal(t, http.StatusInternalServerError, appErr.HTTPStatus)
	assert.Contains(t, appErr.Message, "failed to create async run record")
}

// TestAsyncCaptureRunCreateBudgetMappingWiring makes a revert of any one of the
// three async capture sites fail. The helper's own unit tests prove the mapping
// is right; this wiring pin proves every Remember/Forget async kickoff still
// routes its CreateRunWithOptions failure through it, the same fail-first shape
// as TestRememberForgetRuntimeAgentWiring.
func TestAsyncCaptureRunCreateBudgetMappingWiring(t *testing.T) {
	const file = "handler.go"
	src, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read %s: %v", file, err)
	}
	for _, fn := range []string{"RememberStream", "RememberFile", "ForgetStream"} {
		body := chatFuncBody(t, file, string(src), fn)
		if body == "" {
			t.Errorf("%s: function %s not found", file, fn)
			continue
		}
		if got := strings.Count(body, "mapAsyncRunCreateError("); got != 1 {
			t.Errorf("%s: %s maps its async run-create failure %d time(s) through mapAsyncRunCreateError, want 1 — a budget block must be a 402, not a 500",
				file, fn, got)
		}
		if strings.Contains(body, `apperror.NewInternal("failed to create async run record"`) {
			t.Errorf("%s: %s still turns a budget block into an internal error (#1080)", file, fn)
		}
	}
}
