package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"testing"
	"time"
)

// TestStorageCompatibilityAgainstLiveBackend exercises every S3 operation the
// application relies on (bucket creation, upload, download, presigned GET,
// HeadObject, ListObjectsV2, DeleteObject) against a real S3-compatible
// backend. It guards against backend compatibility gaps (e.g. presign or
// listing differences) when switching object stores.
//
// It is skipped unless STORAGE_COMPAT_ENDPOINT is set, so the default unit-test
// run stays hermetic. Point it at the pinned SeaweedFS image:
//
//	docker run -d --name swfs -p 19000:8333 \
//	  -e AWS_ACCESS_KEY_ID=compat -e AWS_SECRET_ACCESS_KEY=compat-secret \
//	  chrislusf/seaweedfs@sha256:<digest> server -dir=/data -s3
//	STORAGE_COMPAT_ENDPOINT=http://127.0.0.1:19000 \
//	STORAGE_COMPAT_ACCESS_KEY=compat \
//	STORAGE_COMPAT_SECRET_KEY=compat-secret \
//	go test ./internal/storage/... -run TestStorageCompatibilityAgainstLiveBackend
func TestStorageCompatibilityAgainstLiveBackend(t *testing.T) {
	endpoint := os.Getenv("STORAGE_COMPAT_ENDPOINT")
	if endpoint == "" {
		t.Skip("STORAGE_COMPAT_ENDPOINT not set; skipping live S3 compatibility test")
	}

	cfg := &Config{
		Endpoint:        endpoint,
		Provider:        os.Getenv("STORAGE_COMPAT_PROVIDER"),
		AccessKey:       os.Getenv("STORAGE_COMPAT_ACCESS_KEY"),
		SecretKey:       os.Getenv("STORAGE_COMPAT_SECRET_KEY"),
		Region:          "us-east-1",
		BucketDocuments: "compat-documents",
		BucketTemp:      "compat-temp",
	}
	if !cfg.Enabled() {
		t.Fatal("STORAGE_COMPAT_ACCESS_KEY/SECRET_KEY must be set with STORAGE_COMPAT_ENDPOINT")
	}

	svc, err := NewService(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// CreateBucket via EnsureBucket, then HeadBucket.
	for _, bucket := range []string{cfg.BucketDocuments, cfg.BucketTemp} {
		if err := svc.EnsureBucket(ctx, bucket); err != nil {
			t.Fatalf("EnsureBucket(%q): %v", bucket, err)
		}
	}

	key := fmt.Sprintf("compat/%d/hello.txt", time.Now().UnixNano())
	payload := []byte("hello from the storage compatibility suite")

	// PutObject.
	result, err := svc.Upload(ctx, key, bytes.NewReader(payload), int64(len(payload)), UploadOptions{
		ContentType: "text/plain",
	})
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if result.Bucket != cfg.BucketDocuments {
		t.Errorf("Upload bucket = %q, want %q", result.Bucket, cfg.BucketDocuments)
	}

	// HeadObject.
	exists, err := svc.Exists(ctx, key)
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !exists {
		t.Fatal("Exists = false immediately after upload, want true")
	}

	// GetObject.
	body, err := svc.Download(ctx, key)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	got, err := io.ReadAll(body)
	_ = body.Close()
	if err != nil {
		t.Fatalf("read downloaded body: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("downloaded bytes = %q, want %q", got, payload)
	}

	// ListObjectsV2.
	keys, err := svc.ListBucket(ctx, cfg.BucketDocuments, "compat/")
	if err != nil {
		t.Fatalf("ListBucket: %v", err)
	}
	found := false
	for _, k := range keys {
		if k == key {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ListBucket did not include %q (got %v)", key, keys)
	}

	// PresignGetObject + anonymous HTTP GET.
	url, err := svc.GetSignedDownloadURL(ctx, key, GetSignedDownloadURLOptions{ExpiresIn: 5 * time.Minute})
	if err != nil {
		t.Fatalf("GetSignedDownloadURL: %v", err)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url) //nolint:gosec // presigned URL, not user input
	if err != nil {
		t.Fatalf("GET presigned URL: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("presigned GET status = %d, want 200", resp.StatusCode)
	}
	signed, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read presigned body: %v", err)
	}
	if !bytes.Equal(signed, payload) {
		t.Errorf("presigned GET bytes = %q, want %q", signed, payload)
	}

	// DeleteObject.
	if err := svc.Delete(ctx, key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	exists, err = svc.Exists(ctx, key)
	if err != nil {
		t.Fatalf("Exists after delete: %v", err)
	}
	if exists {
		t.Error("Exists = true after delete, want false")
	}
}
