package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/internal/storage"
)

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
