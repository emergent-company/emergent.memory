package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/emergent-company/emergent.memory/internal/storage"
)

// refusedErr mirrors the error the S3 SDK returns when the gateway has not
// opened 8333 yet: an *url.Error wrapping a *net.OpError with ECONNREFUSED.
func refusedErr() error {
	return &url.Error{
		Op:  "Head",
		URL: "http://seaweedfs:8333/documents",
		Err: &net.OpError{
			Op:  "dial",
			Net: "tcp",
			Err: os.NewSyscallError("connect", syscall.ECONNREFUSED),
		},
	}
}

// fakeHTTPStatusError stands in for awshttp.ResponseError, whose
// HTTPStatusCode() the retry policy reads.
type fakeHTTPStatusError struct{ status int }

func (e *fakeHTTPStatusError) Error() string {
	return fmt.Sprintf("https response error StatusCode: %d", e.status)
}
func (e *fakeHTTPStatusError) HTTPStatusCode() int { return e.status }

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestSelectBucketsDefaults(t *testing.T) {
	cfg := &storage.Config{BucketDocuments: "documents", BucketTemp: "document-temp"}
	got := selectBuckets(cfg)
	want := []string{"documents", "document-temp"}
	if len(got) != len(want) {
		t.Fatalf("selectBuckets() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("selectBuckets() = %v, want %v", got, want)
		}
	}
}

func TestSelectBucketsEnvOverrides(t *testing.T) {
	t.Setenv("STORAGE_BUCKET_DOCUMENTS", "docs-override")
	t.Setenv("STORAGE_BUCKET_TEMP", "temp-override")

	cfg := storage.NewConfig()
	got := selectBuckets(cfg)
	want := []string{"docs-override", "temp-override"}
	if len(got) != len(want) {
		t.Fatalf("selectBuckets() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("selectBuckets() = %v, want %v", got, want)
		}
	}
}

func TestSelectBucketsDedupesAndSkipsEmpty(t *testing.T) {
	cfg := &storage.Config{BucketDocuments: "same", BucketTemp: "same"}
	if got := selectBuckets(cfg); len(got) != 1 || got[0] != "same" {
		t.Fatalf("selectBuckets() = %v, want [same]", got)
	}

	cfg = &storage.Config{BucketDocuments: "", BucketTemp: ""}
	if got := selectBuckets(cfg); len(got) != 0 {
		t.Fatalf("selectBuckets() = %v, want empty", got)
	}
}

func TestEnsureBucketsSuccess(t *testing.T) {
	var created []string
	err := ensureBuckets(context.Background(), []string{"documents", "document-temp"},
		func(_ context.Context, bucket string) error {
			created = append(created, bucket)
			return nil
		})
	if err != nil {
		t.Fatalf("ensureBuckets() returned error: %v", err)
	}
	if len(created) != 2 {
		t.Fatalf("created = %v, want both buckets", created)
	}
}

// TestEnsureBucketsFailurePropagates covers the exit-code-on-failure contract:
// a backend error bubbles up so main() can exit non-zero and compose can block
// the server from starting.
func TestEnsureBucketsFailurePropagates(t *testing.T) {
	sentinel := errors.New("backend unreachable")
	calls := 0
	err := ensureBuckets(context.Background(), []string{"documents", "document-temp"},
		func(_ context.Context, bucket string) error {
			calls++
			if bucket == "document-temp" {
				return sentinel
			}
			return nil
		})
	if err == nil {
		t.Fatal("ensureBuckets() returned nil, want error")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("error = %v, want wrapped sentinel", err)
	}
	if !strings.Contains(err.Error(), "document-temp") {
		t.Errorf("error %q does not name the failing bucket", err)
	}
	if calls != 2 {
		t.Errorf("ensure called %d times, want 2 (stop after failure)", calls)
	}
}

func TestRunFailsWhenStorageUnconfigured(t *testing.T) {
	cfg := &storage.Config{BucketDocuments: "documents", BucketTemp: "document-temp"}
	err := run(context.Background(), cfg, discardLogger())
	if err == nil {
		t.Fatal("run() returned nil, want configuration error")
	}
	if !strings.Contains(err.Error(), "not configured") {
		t.Errorf("error %q is not actionable", err)
	}
}

func TestRunFailsOnUnknownProvider(t *testing.T) {
	cfg := &storage.Config{
		Endpoint:        "http://example.invalid:8333",
		Provider:        "not-a-backend",
		AccessKey:       "access",
		SecretKey:       "secret",
		BucketDocuments: "documents",
		BucketTemp:      "document-temp",
	}
	err := run(context.Background(), cfg, discardLogger())
	if err == nil {
		t.Fatal("run() returned nil, want provider validation error")
	}
	if !strings.Contains(err.Error(), "STORAGE_PROVIDER") {
		t.Errorf("error %q does not mention STORAGE_PROVIDER", err)
	}
}

// --- retry/backoff coverage (issue #2004) ---

// TestEnsureBucketsWithRetryAbsorbsRefusedConnection is the regression test for
// the SeaweedFS readiness race: the first HeadBucket hits "connection refused"
// (the S3 gateway is not listening yet) and the bootstrap must retry instead of
// exiting 1 and blocking server start.
func TestEnsureBucketsWithRetryAbsorbsRefusedConnection(t *testing.T) {
	buckets := []string{"documents"}
	calls := 0
	err := ensureBucketsWithRetry(context.Background(), buckets,
		func(_ context.Context, _ string) error {
			calls++
			if calls <= 2 {
				return refusedErr()
			}
			return nil
		},
		5, 0, 0, discardLogger())
	if err != nil {
		t.Fatalf("ensureBucketsWithRetry() returned error after transient failures: %v", err)
	}
	if calls != 3 {
		t.Fatalf("ensure called %d times, want 3 (two retries then success)", calls)
	}
}

func TestEnsureBucketsWithRetryGivesUpAfterAttempts(t *testing.T) {
	calls := 0
	err := ensureBucketsWithRetry(context.Background(), []string{"documents"},
		func(_ context.Context, _ string) error {
			calls++
			return refusedErr()
		},
		3, 0, 0, discardLogger())
	if err == nil {
		t.Fatal("ensureBucketsWithRetry() returned nil, want error once attempts are spent")
	}
	if calls != 3 {
		t.Errorf("ensure called %d times, want 3", calls)
	}
	if !strings.Contains(err.Error(), "3 attempt(s)") {
		t.Errorf("error %q does not report the spent attempt budget", err)
	}
	if !errors.Is(err, syscall.ECONNREFUSED) {
		t.Errorf("error %v does not wrap the underlying connection error", err)
	}
}

// TestEnsureBucketsWithRetryFailsFastOnPermanentError pins the other half of
// the policy: bad credentials (403) must not burn the retry budget.
func TestEnsureBucketsWithRetryFailsFastOnPermanentError(t *testing.T) {
	calls := 0
	permanent := errors.New(`operation error S3: HeadBucket, https response error StatusCode: 403, api error AccessDenied`)
	err := ensureBucketsWithRetry(context.Background(), []string{"documents"},
		func(_ context.Context, _ string) error {
			calls++
			return permanent
		},
		10, 0, 0, discardLogger())
	if err == nil {
		t.Fatal("ensureBucketsWithRetry() returned nil, want error")
	}
	if calls != 1 {
		t.Errorf("ensure called %d times, want 1 (permanent error is not retried)", calls)
	}
	if !errors.Is(err, permanent) {
		t.Errorf("error %v does not wrap the permanent failure", err)
	}
	if !strings.Contains(err.Error(), "1 attempt(s)") {
		t.Errorf("error %q should report the one attempt actually made", err)
	}
}

func TestEnsureBucketsWithRetryAbortsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	err := ensureBucketsWithRetry(ctx, []string{"documents"},
		func(_ context.Context, _ string) error {
			calls++
			cancel() // simulate compose stopping the container mid-backoff
			return refusedErr()
		},
		10, time.Hour, time.Hour, discardLogger())
	if err == nil {
		t.Fatal("ensureBucketsWithRetry() returned nil, want context error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error %v does not wrap context.Canceled", err)
	}
	if calls != 1 {
		t.Errorf("ensure called %d times, want 1", calls)
	}
}

func TestEnsureBucketsWithRetryZeroAttemptsMeansOneAttempt(t *testing.T) {
	calls := 0
	_ = ensureBucketsWithRetry(context.Background(), []string{"documents"},
		func(_ context.Context, _ string) error {
			calls++
			return refusedErr()
		},
		0, 0, 0, discardLogger())
	if calls != 1 {
		t.Errorf("ensure called %d times, want 1", calls)
	}
}

func TestIsRetryableBucketErr(t *testing.T) {
	aws5xx := &fakeHTTPStatusError{status: 503}
	aws4xx := &fakeHTTPStatusError{status: 403}

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"refused connection", refusedErr(), true},
		{"net op error", &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}, true},
		{"dns error", &net.DNSError{Err: "no such host", Name: "seaweedfs"}, true},
		{"context deadline", fmt.Errorf("head bucket: %w", context.DeadlineExceeded), true},
		{"unreachable host", os.NewSyscallError("connect", syscall.EHOSTUNREACH), true},
		{"s3 503", fmt.Errorf("head bucket: %w", aws5xx), true},
		{"s3 403", fmt.Errorf("head bucket: %w", aws4xx), false},
		{"plain error", errors.New("boom"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRetryableBucketErr(tt.err); got != tt.want {
				t.Errorf("isRetryableBucketErr(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
