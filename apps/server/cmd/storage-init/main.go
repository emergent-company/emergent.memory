// Command storage-init is a one-shot bucket bootstrap for the self-hosted
// deployment. It ensures the documents and document-temp buckets exist in the
// configured S3-compatible object store (SeaweedFS by default) using the same
// backend-agnostic S3 client as the server.
//
// It runs as a compose service gated by
// `depends_on: condition: service_completed_successfully`, mirroring the
// emergent-migrate pattern, so the server starts only after bucket
// provisioning succeeds. Any failure exits non-zero.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/emergent-company/emergent.memory/internal/storage"
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

	if err := ensureBuckets(ctx, buckets, svc.EnsureBucket); err != nil {
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

// ensureBuckets creates each bucket, aborting on the first failure so the
// non-zero exit blocks server start.
func ensureBuckets(ctx context.Context, buckets []string, ensure func(context.Context, string) error) error {
	for _, bucket := range buckets {
		if err := ensure(ctx, bucket); err != nil {
			return fmt.Errorf("ensure bucket %q: %w", bucket, err)
		}
	}
	return nil
}
