// Command storage-init is a one-shot bucket bootstrap for the self-hosted
// deployment. It ensures the documents and document-temp buckets exist in the
// configured S3-compatible object store (SeaweedFS by default) using the same
// backend-agnostic S3 client as the server.
//
// It runs as a compose service gated by
// `depends_on: condition: service_completed_successfully`, mirroring the
// emergent-migrate pattern, so the server starts only after bucket
// provisioning succeeds. Any failure exits non-zero.
//
// The first attempt can race the object store: SeaweedFS reports master
// readiness (and its compose healthcheck used to go green) before the S3
// gateway listens on 8333, so the first HeadBucket hits "connection refused"
// (issue #2004). Transient failures are therefore retried with exponential
// backoff inside the compose service's own 5-minute budget instead of exiting
// non-zero on the first refused connection.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"syscall"
	"time"

	"github.com/emergent-company/emergent.memory/internal/storage"
)

// Bucket-bootstrap retry policy. Ten attempts with a 1s→15s doubling backoff
// span roughly 90 seconds, which comfortably covers the window between master
// readiness and the S3 listener accepting connections while staying well inside
// the context timeout in main.
const (
	ensureAttempts   = 10
	ensureInitial    = 1 * time.Second
	ensureMaxBackoff = 15 * time.Second
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if err := run(ctx, storage.NewConfig(), newLogger()); err != nil {
		fmt.Fprintf(os.Stderr, "storage-init: %v\n", err)
		os.Exit(1)
	}
}

func newLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

// run validates the storage config, opens the S3 client, and ensures every
// configured bucket exists. The returned error maps to a non-zero exit code.
func run(ctx context.Context, cfg *storage.Config, log *slog.Logger) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	if !cfg.Enabled() {
		return fmt.Errorf("storage is not configured: set STORAGE_ENDPOINT, STORAGE_ACCESS_KEY and STORAGE_SECRET_KEY")
	}

	svc, err := storage.NewService(cfg, log)
	if err != nil {
		return fmt.Errorf("init storage client: %w", err)
	}

	buckets := selectBuckets(cfg)
	if len(buckets) == 0 {
		return fmt.Errorf("no buckets configured: set STORAGE_BUCKET_DOCUMENTS and/or STORAGE_BUCKET_TEMP")
	}

	if err := ensureBucketsWithRetry(ctx, buckets, svc.EnsureBucket,
		ensureAttempts, ensureInitial, ensureMaxBackoff, log); err != nil {
		return err
	}

	log.Info("storage-init complete", slog.Int("buckets", len(buckets)))
	return nil
}

// selectBuckets returns the distinct buckets to provision, in a deterministic
// order: documents first, then temp. Env overrides (STORAGE_BUCKET_DOCUMENTS,
// STORAGE_BUCKET_TEMP) flow through storage.Config.
func selectBuckets(cfg *storage.Config) []string {
	var buckets []string
	seen := make(map[string]bool)
	for _, b := range []string{cfg.BucketDocuments, cfg.BucketTemp} {
		if b == "" || seen[b] {
			continue
		}
		seen[b] = true
		buckets = append(buckets, b)
	}
	return buckets
}

// ensureBuckets creates each bucket, stopping at the first failure so the
// caller can decide whether to retry or exit non-zero.
func ensureBuckets(ctx context.Context, buckets []string, ensure func(context.Context, string) error) error {
	for _, bucket := range buckets {
		if err := ensure(ctx, bucket); err != nil {
			return fmt.Errorf("ensure bucket %q: %w", bucket, err)
		}
	}
	return nil
}

// ensureBucketsWithRetry runs ensureBuckets up to attempts times, sleeping with
// exponential backoff (initial, doubling, capped at maxBackoff) between
// transient failures, and returns nil on the first success. It gives up
// immediately on a permanent failure (bad credentials, missing bucket that
// cannot be created) because retrying those only delays the error, and it
// returns the context error if ctx is cancelled while waiting.
//
// attempts < 1 is treated as 1; a non-positive initial or maxBackoff means no
// delay between attempts (which is what the unit tests use).
//
// Extracted from run so the retry policy is unit-testable with an injected
// ensure function and zero backoff (issue #2004).
func ensureBucketsWithRetry(
	ctx context.Context,
	buckets []string,
	ensure func(context.Context, string) error,
	attempts int,
	initial, maxBackoff time.Duration,
	log *slog.Logger,
) error {
	if attempts < 1 {
		attempts = 1
	}

	var lastErr error
	backoff := initial
	attemptsMade := 0
	for attempt := 1; attempt <= attempts; attempt++ {
		attemptsMade = attempt
		lastErr = ensureBuckets(ctx, buckets, ensure)
		if lastErr == nil {
			return nil
		}
		if attempt == attempts || !isRetryableBucketErr(lastErr) {
			break
		}

		log.Warn("bucket bootstrap failed, retrying",
			slog.Int("attempt", attempt),
			slog.Int("max_attempts", attempts),
			slog.Duration("backoff", backoff),
			slog.String("error", lastErr.Error()))

		if backoff > 0 {
			select {
			case <-ctx.Done():
				return fmt.Errorf("bucket bootstrap retry aborted: %w (last error: %v)", ctx.Err(), lastErr)
			case <-time.After(backoff):
			}
			next := backoff * 2
			if maxBackoff > 0 && next > maxBackoff {
				next = maxBackoff
			}
			backoff = next
		}
	}

	return fmt.Errorf("bucket bootstrap failed after %d attempt(s): %w", attemptsMade, lastErr)
}

// isRetryableBucketErr reports whether err is worth retrying. Connection-level
// failures (dial refused/reset, DNS, timeouts) and 5xx responses mean the
// object store is not ready yet; 4xx responses mean the request itself is wrong
// (bad credentials, denied) and will not improve by waiting.
func isRetryableBucketErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	for _, target := range []error{syscall.ECONNREFUSED, syscall.ECONNRESET, syscall.EPIPE, syscall.EHOSTUNREACH, syscall.ENETUNREACH} {
		if errors.Is(err, target) {
			return true
		}
	}

	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}

	// 5xx means "not ready yet" (SeaweedFS answers 503 while volumes are being
	// created); 4xx means the request is wrong and retrying will not help.
	var statusCoder interface{ HTTPStatusCode() int }
	if errors.As(err, &statusCoder) {
		return statusCoder.HTTPStatusCode() >= 500
	}

	return false
}
