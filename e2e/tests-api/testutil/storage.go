package testutil

import (
	"context"
	"fmt"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// StorageConfig holds the S3-compatible (MinIO) connection settings needed to
// remove backup archive objects that the API's soft-delete backup DELETE
// endpoint leaves behind. The backup service stores archives in the documents
// bucket under keys of the form `backups/{orgID}/{backupID}/backup.zip`.
type StorageConfig struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Bucket    string
}

// LoadStorageConfig reads storage settings from the environment. It mirrors the
// server's internal storage configuration (internal/storage) so the test can
// target the same MinIO instance.
func LoadStorageConfig() StorageConfig {
	return StorageConfig{
		Endpoint:  getEnv("STORAGE_ENDPOINT", ""),
		AccessKey: getEnv("STORAGE_ACCESS_KEY", ""),
		SecretKey: getEnv("STORAGE_SECRET_KEY", ""),
		Bucket:    getEnv("STORAGE_BUCKET_DOCUMENTS", "documents"),
	}
}

// Enabled reports whether storage settings are present.
func (c StorageConfig) Enabled() bool {
	return c.Endpoint != "" && c.AccessKey != "" && c.SecretKey != ""
}

// Storage is a best-effort object-storage client used only for test cleanup.
type Storage struct {
	client *minio.Client
	bucket string
}

// NewStorage connects to the S3-compatible object store. It returns a nil
// Storage (and nil error) when storage is not configured, so callers can treat
// cleanup as a no-op in harnesses without MinIO.
func NewStorage(cfg StorageConfig) (*Storage, error) {
	if !cfg.Enabled() {
		return nil, nil
	}
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: strings.HasPrefix(cfg.Endpoint, "https://"),
	})
	if err != nil {
		return nil, fmt.Errorf("connect to object storage: %w", err)
	}
	return &Storage{client: client, bucket: cfg.Bucket}, nil
}

// DeleteBackup removes the archive object for the given backup. It is a no-op
// when storage is not configured. Errors are returned so the caller can decide
// whether to surface them; teardown treats them as best-effort.
func (s *Storage) DeleteBackup(ctx context.Context, orgID, backupID string) error {
	if s == nil {
		return nil
	}
	key := fmt.Sprintf("backups/%s/%s/backup.zip", orgID, backupID)
	return s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
}
