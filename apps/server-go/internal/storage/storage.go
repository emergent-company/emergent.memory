package storage

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
	"go.uber.org/fx"

	"github.com/emergent-company/emergent/pkg/logger"
)

var Module = fx.Module("storage",
	fx.Provide(NewConfig),
	fx.Provide(NewService),
)

// Config holds storage configuration
type Config struct {
	Endpoint        string
	AccessKey       string
	SecretKey       string
	Region          string
	BucketDocuments string
	BucketTemp      string
}

// Enabled returns true if storage is properly configured
func (c *Config) Enabled() bool {
	return c.Endpoint != "" && c.AccessKey != "" && c.SecretKey != ""
}

// NewConfig creates storage config from environment variables
func NewConfig() *Config {
	region := os.Getenv("STORAGE_REGION")
	if region == "" {
		region = "us-east-1"
	}

	bucketDocs := os.Getenv("STORAGE_BUCKET_DOCUMENTS")
	if bucketDocs == "" {
		bucketDocs = "documents"
	}

	bucketTemp := os.Getenv("STORAGE_BUCKET_TEMP")
	if bucketTemp == "" {
		bucketTemp = "document-temp"
	}

	return &Config{
		Endpoint:        os.Getenv("STORAGE_ENDPOINT"),
		AccessKey:       os.Getenv("STORAGE_ACCESS_KEY"),
		SecretKey:       os.Getenv("STORAGE_SECRET_KEY"),
		Region:          region,
		BucketDocuments: bucketDocs,
		BucketTemp:      bucketTemp,
	}
}

// Service provides S3-compatible storage operations
type Service struct {
	client          *s3.Client
	presignClient   *s3.PresignClient
	cfg             *Config
	log             *slog.Logger
	bucketDocuments string
}

// UploadOptions configures an upload operation
type UploadOptions struct {
	ContentType        string
	ContentDisposition string
	Metadata           map[string]string
}

// UploadResult contains information about an uploaded object
type UploadResult struct {
	Key         string
	Bucket      string
	ETag        string
	Size        int64
	ContentType string
	StorageURL  string
}

// DocumentUploadOptions extends UploadOptions with document-specific fields
type DocumentUploadOptions struct {
	OrgID     string
	ProjectID string
	Filename  string
	UploadOptions
}

// NewService creates a new storage service
func NewService(cfg *Config, log *slog.Logger) (*Service, error) {
	if !cfg.Enabled() {
		log.Warn("storage service disabled - no configuration provided")
		return &Service{
			cfg: cfg,
			log: log.With(logger.Scope("storage")),
		}, nil
	}

	// Create custom endpoint resolver for MinIO
	customResolver := aws.EndpointResolverWithOptionsFunc(
		func(service, region string, options ...interface{}) (aws.Endpoint, error) {
			return aws.Endpoint{
				URL:               cfg.Endpoint,
				HostnameImmutable: true,
				SigningRegion:     cfg.Region,
			}, nil
		},
	)

	// Load AWS config with custom credentials and endpoint
	awsCfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion(cfg.Region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.AccessKey,
			cfg.SecretKey,
			"",
		)),
		config.WithEndpointResolverWithOptions(customResolver),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Create S3 client with path-style addressing (required for MinIO)
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.UsePathStyle = true
	})

	// Create presign client for signed URLs
	presignClient := s3.NewPresignClient(client)

	log.Info("storage service initialized",
		slog.String("endpoint", cfg.Endpoint),
		slog.String("bucket", cfg.BucketDocuments),
	)

	return &Service{
		client:          client,
		presignClient:   presignClient,
		cfg:             cfg,
		log:             log.With(logger.Scope("storage")),
		bucketDocuments: cfg.BucketDocuments,
	}, nil
}

// Enabled returns true if the storage service is properly configured
func (s *Service) Enabled() bool {
	return s.client != nil
}

// Upload uploads data to the specified key in the documents bucket
func (s *Service) Upload(ctx context.Context, key string, data io.Reader, size int64, opts UploadOptions) (*UploadResult, error) {
	if !s.Enabled() {
		return nil, fmt.Errorf("storage service not enabled")
	}

	input := &s3.PutObjectInput{
		Bucket:        aws.String(s.bucketDocuments),
		Key:           aws.String(key),
		Body:          data,
		ContentLength: aws.Int64(size),
	}

	if opts.ContentType != "" {
		input.ContentType = aws.String(opts.ContentType)
	}
	if opts.ContentDisposition != "" {
		input.ContentDisposition = aws.String(opts.ContentDisposition)
	}
	if len(opts.Metadata) > 0 {
		input.Metadata = opts.Metadata
	}

	result, err := s.client.PutObject(ctx, input)
	if err != nil {
		s.log.Error("failed to upload object",
			slog.String("key", key),
			logger.Error(err),
		)
		return nil, fmt.Errorf("upload failed: %w", err)
	}

	etag := ""
	if result.ETag != nil {
		etag = strings.Trim(*result.ETag, "\"")
	}

	s.log.Debug("object uploaded",
		slog.String("key", key),
		slog.String("bucket", s.bucketDocuments),
		slog.Int64("size", size),
	)

	return &UploadResult{
		Key:         key,
		Bucket:      s.bucketDocuments,
		ETag:        etag,
		Size:        size,
		ContentType: opts.ContentType,
		StorageURL:  fmt.Sprintf("%s/%s", s.bucketDocuments, key),
	}, nil
}

// UploadDocument uploads a document with project/org namespacing
func (s *Service) UploadDocument(ctx context.Context, data io.Reader, size int64, opts DocumentUploadOptions) (*UploadResult, error) {
	// Generate storage key: {projectId}/{orgId}/{uuid}-{sanitized_filename}
	key := GenerateDocumentKey(opts.ProjectID, opts.OrgID, opts.Filename)

	// Set content disposition for download
	uploadOpts := opts.UploadOptions
	if uploadOpts.ContentDisposition == "" && opts.Filename != "" {
		uploadOpts.ContentDisposition = fmt.Sprintf(`attachment; filename="%s"`, opts.Filename)
	}

	return s.Upload(ctx, key, data, size, uploadOpts)
}

// Download retrieves an object from storage
func (s *Service) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	if !s.Enabled() {
		return nil, fmt.Errorf("storage service not enabled")
	}

	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucketDocuments),
		Key:    aws.String(key),
	})
	if err != nil {
		s.log.Error("failed to download object",
			slog.String("key", key),
			logger.Error(err),
		)
		return nil, fmt.Errorf("download failed: %w", err)
	}

	return result.Body, nil
}

// Delete removes an object from storage
func (s *Service) Delete(ctx context.Context, key string) error {
	if !s.Enabled() {
		return fmt.Errorf("storage service not enabled")
	}

	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucketDocuments),
		Key:    aws.String(key),
	})
	if err != nil {
		s.log.Error("failed to delete object",
			slog.String("key", key),
			logger.Error(err),
		)
		return fmt.Errorf("delete failed: %w", err)
	}

	s.log.Debug("object deleted", slog.String("key", key))
	return nil
}

// Exists checks if an object exists in storage
func (s *Service) Exists(ctx context.Context, key string) (bool, error) {
	if !s.Enabled() {
		return false, fmt.Errorf("storage service not enabled")
	}

	_, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucketDocuments),
		Key:    aws.String(key),
	})
	if err != nil {
		// Check if it's a "not found" error
		errStr := err.Error()
		if strings.Contains(errStr, "NotFound") || strings.Contains(errStr, "404") || strings.Contains(errStr, "NoSuchKey") {
			return false, nil
		}
		return false, fmt.Errorf("head object failed: %w", err)
	}

	return true, nil
}

// GenerateDocumentKey creates a storage key for a document
// Format: {projectId}/{orgId}/{uuid}-{sanitized_filename}
// If orgID is empty, format: {projectId}/{uuid}-{sanitized_filename}
func GenerateDocumentKey(projectID, orgID, filename string) string {
	sanitized := SanitizeFilename(filename)
	id := uuid.New().String()
	if orgID == "" {
		return fmt.Sprintf("%s/%s-%s", projectID, id, sanitized)
	}
	return fmt.Sprintf("%s/%s/%s-%s", projectID, orgID, id, sanitized)
}

// SanitizeFilename cleans a filename for storage
func SanitizeFilename(filename string) string {
	if filename == "" {
		return "unnamed"
	}

	// Replace special characters with underscores
	re := regexp.MustCompile(`[^a-zA-Z0-9._-]`)
	sanitized := re.ReplaceAllString(filename, "_")

	// Collapse multiple underscores
	re = regexp.MustCompile(`_{2,}`)
	sanitized = re.ReplaceAllString(sanitized, "_")

	// Trim leading/trailing underscores
	sanitized = strings.Trim(sanitized, "_")

	// Lowercase
	sanitized = strings.ToLower(sanitized)

	// Limit length
	if len(sanitized) > 200 {
		sanitized = sanitized[:200]
	}

	if sanitized == "" {
		return "unnamed"
	}

	return sanitized
}

// GetSignedDownloadURLOptions configures a signed download URL
type GetSignedDownloadURLOptions struct {
	ExpiresIn                  time.Duration
	ResponseContentDisposition string
}

// GetSignedDownloadURL generates a presigned URL for downloading an object
func (s *Service) GetSignedDownloadURL(ctx context.Context, key string, opts GetSignedDownloadURLOptions) (string, error) {
	if !s.Enabled() {
		return "", fmt.Errorf("storage service not enabled")
	}

	if opts.ExpiresIn == 0 {
		opts.ExpiresIn = time.Hour // Default 1 hour expiration
	}

	input := &s3.GetObjectInput{
		Bucket: aws.String(s.bucketDocuments),
		Key:    aws.String(key),
	}

	if opts.ResponseContentDisposition != "" {
		input.ResponseContentDisposition = aws.String(opts.ResponseContentDisposition)
	}

	presignedReq, err := s.presignClient.PresignGetObject(ctx, input, func(po *s3.PresignOptions) {
		po.Expires = opts.ExpiresIn
	})
	if err != nil {
		s.log.Error("failed to generate presigned URL",
			slog.String("key", key),
			logger.Error(err),
		)
		return "", fmt.Errorf("presign failed: %w", err)
	}

	s.log.Debug("presigned URL generated",
		slog.String("key", key),
		slog.Duration("expires", opts.ExpiresIn),
	)

	return presignedReq.URL, nil
}
