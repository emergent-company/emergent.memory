package extraction

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// TestRetryJobFailureMapsTheRefusals is the regression test for the admin retry
// route's error mapping.
//
// The route used to wrap every RetryJob failure in NewBadRequest(err.Error()):
// a job that did not exist (or was another project's) came back as 400
// "not_found: extraction job not found" — a status the caller reads as "my
// request is malformed" — and a database failure came back as a 400 whose message
// was the driver's text.
func TestRetryJobFailureMapsTheRefusals(t *testing.T) {
	discard := slog.New(slog.NewTextHandler(io.Discard, nil))

	tests := []struct {
		name        string
		err         error
		wantStatus  int
		wantMessage string
		mustNot     []string
	}{
		{
			name:        "a gone job keeps its 404",
			err:         fmt.Errorf("find job: %w", apperror.ErrNotFound.WithMessage("extraction job not found")),
			wantStatus:  http.StatusNotFound,
			wantMessage: "extraction job not found",
		},
		{
			name:        "a terminal status keeps its 400 and says which",
			err:         apperror.ErrBadRequest.WithMessage("cannot retry job with status completed"),
			wantStatus:  http.StatusBadRequest,
			wantMessage: "cannot retry job with status completed",
		},
		{
			name:        "a live run is a conflict, not a bad request",
			err:         fmt.Errorf("%w: it started 2m ago and is only requeued after the 30-minute stale threshold", ErrJobStillRunning),
			wantStatus:  http.StatusConflict,
			wantMessage: "still running",
		},
		{
			name:        "a driver failure is a 500 with the detail in the log only",
			err:         errors.New("update jobs: pq: deadlock detected"),
			wantStatus:  http.StatusInternalServerError,
			wantMessage: "failed to retry extraction job",
			mustNot:     []string{"pq:", "deadlock", "update jobs"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := retryJobFailure(tt.err, "job-1", "proj-1", discard)
			require.Error(t, err)

			var appErr *apperror.Error
			require.True(t, errors.As(err, &appErr), "the route must return an app error")
			assert.Equal(t, tt.wantStatus, appErr.HTTPStatus)
			assert.Contains(t, appErr.Message, tt.wantMessage)
			for _, forbidden := range tt.mustNot {
				assert.NotContains(t, appErr.Message, forbidden)
			}
		})
	}
}
