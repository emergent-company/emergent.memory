package documents

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// TestRetryFailureRendersOnlyClientFacingText pins how a retry refusal becomes
// the response. Before this the handler wrapped every failure in
// apperror.ErrBadRequest.WithMessage(err.Error()), so a database error came back
// as a 400 whose message was the driver's text — and an actionable refusal lost
// its own status.
func TestRetryFailureRendersOnlyClientFacingText(t *testing.T) {
	h := &Handler{log: slog.New(slog.NewTextHandler(io.Discard, nil))}

	tests := []struct {
		name        string
		err         error
		wantStatus  int
		wantMessage string
	}{
		{
			name:        "in-flight run stays a conflict",
			err:         NewRetryConflict("extraction job is still running: it started 1m0s ago"),
			wantStatus:  http.StatusConflict,
			wantMessage: "extraction job is still running: it started 1m0s ago",
		},
		{
			name:        "a gone job keeps the extractor's refusal",
			err:         apperror.ErrNotFound.WithMessage("extraction job not found"),
			wantStatus:  http.StatusNotFound,
			wantMessage: "extraction job not found",
		},
		{
			name:        "a terminal job keeps the extractor's refusal",
			err:         apperror.ErrBadRequest.WithMessage("cannot retry job with status completed"),
			wantStatus:  http.StatusBadRequest,
			wantMessage: "cannot retry job with status completed",
		},
		{
			name:        "a wrapped app error is unwrapped",
			err:         fmt.Errorf("retry: %w", apperror.ErrBadRequest.WithMessage("cannot retry job with status ready")),
			wantStatus:  http.StatusBadRequest,
			wantMessage: "cannot retry job with status ready",
		},
		{
			name:        "a driver failure is a server error with no detail",
			err:         errors.New(`update kb.object_extraction_jobs: pq: deadlock detected`),
			wantStatus:  http.StatusInternalServerError,
			wantMessage: "failed to retry extraction",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := h.retryFailure(tt.err)

			var appErr *apperror.Error
			require.ErrorAs(t, got, &appErr)
			require.Equal(t, tt.wantStatus, appErr.HTTPStatus)
			require.Equal(t, tt.wantMessage, appErr.Message)
			require.NotContains(t, appErr.Message, "pq:",
				"driver text must never become the response message")
			require.False(t, strings.Contains(appErr.Message, "deadlock"),
				"driver text must never become the response message")
		})
	}
}
