package adk

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestIsRetryableProviderStatus(t *testing.T) {
	tests := map[int]bool{
		200: false,
		400: false,
		401: false,
		403: false,
		404: false,
		422: false,
		429: true,
		500: true,
		502: true,
		503: true,
		504: true,
	}
	for code, want := range tests {
		if got := isRetryableProviderStatus(code); got != want {
			t.Errorf("isRetryableProviderStatus(%d) = %v, want %v", code, got, want)
		}
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)

	tests := []struct {
		name   string
		value  string
		want   time.Duration
		wantOK bool
	}{
		{"empty", "", 0, false},
		{"garbage", "soon", 0, false},
		{"seconds", "5", 5 * time.Second, true},
		{"seconds with spaces", " 7 ", 7 * time.Second, true},
		{"zero", "0", 0, true},
		{"negative is zero", "-3", 0, true},
		{"http date", now.Add(12 * time.Second).Format(http.TimeFormat), 12 * time.Second, true},
		{"past http date is zero", now.Add(-time.Minute).Format(http.TimeFormat), 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseRetryAfter(tt.value, now)
			if ok != tt.wantOK {
				t.Fatalf("parseRetryAfter(%q) ok = %v, want %v", tt.value, ok, tt.wantOK)
			}
			if ok && got != tt.want {
				t.Errorf("parseRetryAfter(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestProviderRetryDelay(t *testing.T) {
	tests := []struct {
		name       string
		attempt    int
		retryAfter string
		want       time.Duration
	}{
		{"header wins", 0, "2", 2 * time.Second},
		{"header is capped", 0, "3600", providerRetryMax},
		{"backoff first attempt", 0, "", providerRetryBase},
		{"backoff doubles", 1, "", 2 * providerRetryBase},
		{"unparseable header falls back to backoff", 1, "later", 2 * providerRetryBase},
		{"backoff is capped", 5, "", providerRetryMax},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := providerRetryDelay(tt.attempt, tt.retryAfter); got != tt.want {
				t.Errorf("providerRetryDelay(%d, %q) = %v, want %v", tt.attempt, tt.retryAfter, got, tt.want)
			}
		})
	}
}

func TestSleepWithContext(t *testing.T) {
	if !sleepWithContext(context.Background(), 0) {
		t.Error("a zero wait should complete immediately")
	}
	if !sleepWithContext(context.Background(), time.Millisecond) {
		t.Error("a short wait should complete")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if sleepWithContext(ctx, time.Hour) {
		t.Error("a cancelled context should end the wait")
	}
}

// TestProviderRetryWait pins the single retry decision the three model clients
// share, including the cap: a provider asking for longer than providerRetryMax
// is not retried inside the call at all (#254).
func TestProviderRetryWait(t *testing.T) {
	tests := []struct {
		name       string
		attempt    int
		statusCode int
		retryAfter string
		wantWait   time.Duration
		wantRetry  bool
	}{
		{"rate limit is retried", 0, http.StatusTooManyRequests, "", providerRetryBase, true},
		{"server error is retried", 0, http.StatusServiceUnavailable, "0", 0, true},
		{"attempts are bounded", maxProviderRetries, http.StatusTooManyRequests, "0", 0, false},
		{"client error is not retried", 0, http.StatusBadRequest, "", 0, false},
		{"success is not retried", 0, http.StatusOK, "", 0, false},
		{"hint beyond the cap gives up at once", 0, http.StatusTooManyRequests, "120", 0, false},
		{"hint at the cap is honoured", 0, http.StatusTooManyRequests, "30", providerRetryMax, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wait, retry := providerRetryWait(tt.attempt, tt.statusCode, tt.retryAfter)
			if retry != tt.wantRetry {
				t.Fatalf("providerRetryWait(%d, %d, %q) retry = %v, want %v",
					tt.attempt, tt.statusCode, tt.retryAfter, retry, tt.wantRetry)
			}
			if wait != tt.wantWait {
				t.Errorf("providerRetryWait(%d, %d, %q) wait = %v, want %v",
					tt.attempt, tt.statusCode, tt.retryAfter, wait, tt.wantWait)
			}
		})
	}
}

// TestRetryContextError verifies a retry abandoned because the context ended is
// reported as cancellation, not as the provider response that preceded it
// (#254).
func TestRetryContextError(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	err := retryContextError("client", cancelled)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("errors.Is(%v, context.Canceled) = false, want true", err)
	}
	if !strings.Contains(err.Error(), "client") {
		t.Errorf("error %q should name the client", err.Error())
	}

	expired, expire := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer expire()
	err = retryContextError("client", expired)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("errors.Is(%v, context.DeadlineExceeded) = false, want true", err)
	}

	// A context that somehow reports no error must still surface as
	// cancellation rather than as success or a provider fault.
	err = retryContextError("client", context.Background())
	if !errors.Is(err, context.Canceled) {
		t.Errorf("errors.Is(%v, context.Canceled) = false, want true", err)
	}
}

// retryableResponseTransport answers every round trip with one retryable
// response and, when cancel is set, cancels the request context as RoundTrip
// returns. The cancellation tests therefore reach the client's backoff with the
// context already ended: no network and no wall-clock wait.
type retryableResponseTransport struct {
	status     int
	body       string
	retryAfter string
	cancel     context.CancelFunc
	calls      int
}

func (tr *retryableResponseTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	tr.calls++
	header := make(http.Header)
	if tr.retryAfter != "" {
		header.Set("Retry-After", tr.retryAfter)
	}
	resp := &http.Response{
		StatusCode: tr.status,
		Status:     http.StatusText(tr.status),
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(tr.body)),
		Request:    req,
	}
	if tr.cancel != nil {
		tr.cancel()
	}
	return resp, nil
}
