package storage

import (
	"io"
	"log/slog"
	"strings"
	"testing"
)

func clearStorageEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"STORAGE_ENDPOINT",
		"STORAGE_PROVIDER",
		"STORAGE_ACCESS_KEY",
		"STORAGE_SECRET_KEY",
		"STORAGE_REGION",
		"STORAGE_BUCKET_DOCUMENTS",
		"STORAGE_BUCKET_TEMP",
	} {
		t.Setenv(k, "")
	}
}

func TestNewConfigDefaults(t *testing.T) {
	clearStorageEnv(t)

	cfg := NewConfig()
	if cfg.Region != "us-east-1" {
		t.Errorf("Region = %q, want us-east-1", cfg.Region)
	}
	if cfg.Provider != "" {
		t.Errorf("Provider = %q, want empty default", cfg.Provider)
	}
	if cfg.BucketDocuments != "documents" {
		t.Errorf("BucketDocuments = %q, want documents", cfg.BucketDocuments)
	}
	if cfg.BucketTemp != "document-temp" {
		t.Errorf("BucketTemp = %q, want document-temp", cfg.BucketTemp)
	}
	if cfg.Enabled() {
		t.Error("Enabled() = true with no endpoint/credentials, want false")
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() with empty provider returned error: %v", err)
	}
}

func TestNewConfigOverrides(t *testing.T) {
	clearStorageEnv(t)
	t.Setenv("STORAGE_ENDPOINT", "http://seaweedfs:8333")
	t.Setenv("STORAGE_PROVIDER", "seaweedfs")
	t.Setenv("STORAGE_ACCESS_KEY", "emergent")
	t.Setenv("STORAGE_SECRET_KEY", "secret")
	t.Setenv("STORAGE_REGION", "eu-west-2")
	t.Setenv("STORAGE_BUCKET_DOCUMENTS", "docs-bucket")
	t.Setenv("STORAGE_BUCKET_TEMP", "temp-bucket")

	cfg := NewConfig()
	if cfg.Region != "eu-west-2" {
		t.Errorf("Region = %q, want eu-west-2", cfg.Region)
	}
	if cfg.Provider != "seaweedfs" {
		t.Errorf("Provider = %q, want seaweedfs", cfg.Provider)
	}
	if cfg.Endpoint != "http://seaweedfs:8333" {
		t.Errorf("Endpoint = %q, want http://seaweedfs:8333", cfg.Endpoint)
	}
	if cfg.BucketDocuments != "docs-bucket" || cfg.BucketTemp != "temp-bucket" {
		t.Errorf("buckets = %q/%q, want docs-bucket/temp-bucket", cfg.BucketDocuments, cfg.BucketTemp)
	}
	if !cfg.Enabled() {
		t.Error("Enabled() = false with full config, want true")
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() with seaweedfs returned error: %v", err)
	}
}

func TestConfigValidateRejectsUnknownProvider(t *testing.T) {
	cfg := &Config{Provider: "ceph"}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() with unknown provider returned nil, want error")
	}
	if !strings.Contains(err.Error(), "ceph") {
		t.Errorf("error %q does not name the rejected provider", err)
	}
	if !strings.Contains(err.Error(), "seaweedfs") {
		t.Errorf("error %q does not list accepted providers", err)
	}
}

func TestNewServiceRejectsUnknownProvider(t *testing.T) {
	cfg := &Config{
		Endpoint:  "http://example.invalid:8333",
		Provider:  "not-a-backend",
		AccessKey: "access",
		SecretKey: "secret",
	}
	_, err := NewService(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err == nil {
		t.Fatal("NewService() with unknown provider returned nil error, want fail-fast")
	}
	if !strings.Contains(err.Error(), "STORAGE_PROVIDER") {
		t.Errorf("error %q does not mention STORAGE_PROVIDER", err)
	}
}

func TestNewServiceAllowedProviders(t *testing.T) {
	for _, provider := range []string{"", "s3", "seaweedfs", "minio"} {
		t.Run("provider="+provider, func(t *testing.T) {
			cfg := &Config{Endpoint: "http://example.invalid:8333", Provider: provider}
			// Storage disabled (no credentials) but provider validation must pass.
			if _, err := NewService(cfg, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
				t.Errorf("NewService() with provider %q returned error: %v", provider, err)
			}
		})
	}
}

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: "unnamed",
		},
		{
			name:     "simple filename",
			input:    "document.pdf",
			expected: "document.pdf",
		},
		{
			name:     "uppercase to lowercase",
			input:    "DOCUMENT.PDF",
			expected: "document.pdf",
		},
		{
			name:     "mixed case",
			input:    "MyDocument.PDF",
			expected: "mydocument.pdf",
		},
		{
			name:     "spaces replaced with underscore",
			input:    "my document.pdf",
			expected: "my_document.pdf",
		},
		{
			name:     "multiple spaces collapsed",
			input:    "my   document.pdf",
			expected: "my_document.pdf",
		},
		{
			name:     "special characters replaced",
			input:    "doc@#$%file.pdf",
			expected: "doc_file.pdf",
		},
		{
			name:     "unicode characters replaced",
			input:    "документ.pdf",
			expected: "unnamed.pdf", // all base chars are non-ASCII; extension preserved
		},
		{
			name:     "leading underscore trimmed",
			input:    "_document.pdf",
			expected: "document.pdf",
		},
		{
			name:     "trailing underscore trimmed",
			input:    "document_.pdf",
			expected: "document.pdf", // trailing underscore on base trimmed before extension added
		},
		{
			name:     "multiple underscores collapsed",
			input:    "doc___file.pdf",
			expected: "doc_file.pdf",
		},
		{
			name:     "parentheses replaced",
			input:    "document (1).pdf",
			expected: "document_1.pdf", // trailing underscore from ) before .pdf trimmed
		},
		{
			name:     "dashes preserved",
			input:    "my-document.pdf",
			expected: "my-document.pdf",
		},
		{
			name:     "numbers preserved",
			input:    "file123.pdf",
			expected: "file123.pdf",
		},
		{
			name:     "dots preserved",
			input:    "file.backup.pdf",
			expected: "file.backup.pdf",
		},
		{
			name:     "all special chars becomes unnamed",
			input:    "@#$%^&*()",
			expected: "unnamed",
		},
		{
			name:     "very long filename truncated",
			input:    strings.Repeat("a", 300),
			expected: strings.Repeat("a", 200),
		},
		{
			name:     "emojis replaced",
			input:    "doc📄.pdf",
			expected: "doc.pdf", // emoji becomes underscore, trailing underscore trimmed before extension
		},
		{
			name:     "newlines replaced",
			input:    "doc\nfile.pdf",
			expected: "doc_file.pdf",
		},
		{
			name:     "tabs replaced",
			input:    "doc\tfile.pdf",
			expected: "doc_file.pdf",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeFilename(tt.input)
			if result != tt.expected {
				t.Errorf("SanitizeFilename(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestGenerateDocumentKey(t *testing.T) {
	tests := []struct {
		name      string
		projectID string
		orgID     string
		filename  string
	}{
		{
			name:      "normal document",
			projectID: "proj-123",
			orgID:     "org-456",
			filename:  "document.pdf",
		},
		{
			name:      "document with spaces",
			projectID: "proj-123",
			orgID:     "org-456",
			filename:  "my document.pdf",
		},
		{
			name:      "empty filename",
			projectID: "proj-123",
			orgID:     "org-456",
			filename:  "",
		},
		{
			name:      "special characters in filename",
			projectID: "proj-123",
			orgID:     "org-456",
			filename:  "doc@file#2024.pdf",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GenerateDocumentKey(tt.projectID, tt.orgID, tt.filename)

			// Check format: {projectId}/{orgId}/{uuid}-{sanitized_filename}
			expectedPrefix := tt.projectID + "/" + tt.orgID + "/"
			if !strings.HasPrefix(result, expectedPrefix) {
				t.Errorf("GenerateDocumentKey() prefix = %q, want prefix %q", result, expectedPrefix)
			}

			// Check that the key ends with sanitized filename
			expectedSanitized := SanitizeFilename(tt.filename)
			if !strings.HasSuffix(result, "-"+expectedSanitized) {
				t.Errorf("GenerateDocumentKey() should end with -%q, got %q", expectedSanitized, result)
			}

			// The middle part should be a valid UUID (36 chars)
			suffix := strings.TrimPrefix(result, expectedPrefix)
			// UUID format: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx (36 chars) followed by -filename
			// Find the position after the UUID by looking for the 5th hyphen
			dashCount := 0
			uuidEnd := -1
			for i, c := range suffix {
				if c == '-' {
					dashCount++
					if dashCount == 5 {
						uuidEnd = i
						break
					}
				}
			}

			if uuidEnd != 36 {
				t.Errorf("GenerateDocumentKey() UUID length should be 36, found UUID end at %d in %q", uuidEnd, suffix)
			}
		})
	}
}

func TestGenerateDocumentKey_UniquePerCall(t *testing.T) {
	key1 := GenerateDocumentKey("proj", "org", "file.pdf")
	key2 := GenerateDocumentKey("proj", "org", "file.pdf")

	if key1 == key2 {
		t.Error("GenerateDocumentKey() should return unique keys for each call")
	}
}

func TestConfig_Enabled(t *testing.T) {
	tests := []struct {
		name     string
		config   Config
		expected bool
	}{
		{
			name:     "empty config",
			config:   Config{},
			expected: false,
		},
		{
			name: "only endpoint set",
			config: Config{
				Endpoint: "http://localhost:9000",
			},
			expected: false,
		},
		{
			name: "endpoint and access key set",
			config: Config{
				Endpoint:  "http://localhost:9000",
				AccessKey: "minioadmin",
			},
			expected: false,
		},
		{
			name: "all required fields set",
			config: Config{
				Endpoint:  "http://localhost:9000",
				AccessKey: "minioadmin",
				SecretKey: "minioadmin",
			},
			expected: true,
		},
		{
			name: "full config with all fields",
			config: Config{
				Endpoint:        "http://localhost:9000",
				AccessKey:       "minioadmin",
				SecretKey:       "minioadmin",
				Region:          "us-east-1",
				BucketDocuments: "documents",
				BucketTemp:      "temp",
			},
			expected: true,
		},
		{
			name: "missing secret key",
			config: Config{
				Endpoint:  "http://localhost:9000",
				AccessKey: "minioadmin",
				SecretKey: "",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.Enabled()
			if result != tt.expected {
				t.Errorf("Config.Enabled() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestService_Enabled(t *testing.T) {
	tests := []struct {
		name     string
		service  Service
		expected bool
	}{
		{
			name:     "nil client",
			service:  Service{client: nil},
			expected: false,
		},
		{
			name:     "empty service",
			service:  Service{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.service.Enabled()
			if result != tt.expected {
				t.Errorf("Service.Enabled() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestUploadOptions(t *testing.T) {
	opts := UploadOptions{
		ContentType:        "application/pdf",
		ContentDisposition: "attachment; filename=\"test.pdf\"",
		Metadata: map[string]string{
			"project": "test-project",
			"user":    "test-user",
		},
	}

	if opts.ContentType != "application/pdf" {
		t.Errorf("ContentType = %q, want application/pdf", opts.ContentType)
	}
	if opts.ContentDisposition != "attachment; filename=\"test.pdf\"" {
		t.Errorf("ContentDisposition = %q, want attachment; filename=\"test.pdf\"", opts.ContentDisposition)
	}
	if len(opts.Metadata) != 2 {
		t.Errorf("Metadata length = %d, want 2", len(opts.Metadata))
	}
}

func TestUploadResult(t *testing.T) {
	result := UploadResult{
		Key:         "proj/org/uuid-file.pdf",
		Bucket:      "documents",
		ETag:        "abc123",
		Size:        1024,
		ContentType: "application/pdf",
		StorageURL:  "documents/proj/org/uuid-file.pdf",
	}

	if result.Key != "proj/org/uuid-file.pdf" {
		t.Errorf("Key = %q, want proj/org/uuid-file.pdf", result.Key)
	}
	if result.Bucket != "documents" {
		t.Errorf("Bucket = %q, want documents", result.Bucket)
	}
	if result.ETag != "abc123" {
		t.Errorf("ETag = %q, want abc123", result.ETag)
	}
	if result.Size != 1024 {
		t.Errorf("Size = %d, want 1024", result.Size)
	}
	if result.ContentType != "application/pdf" {
		t.Errorf("ContentType = %q, want application/pdf", result.ContentType)
	}
}

func TestDocumentUploadOptions(t *testing.T) {
	opts := DocumentUploadOptions{
		OrgID:     "org-123",
		ProjectID: "proj-456",
		Filename:  "test.pdf",
		UploadOptions: UploadOptions{
			ContentType: "application/pdf",
		},
	}

	if opts.OrgID != "org-123" {
		t.Errorf("OrgID = %q, want org-123", opts.OrgID)
	}
	if opts.ProjectID != "proj-456" {
		t.Errorf("ProjectID = %q, want proj-456", opts.ProjectID)
	}
	if opts.Filename != "test.pdf" {
		t.Errorf("Filename = %q, want test.pdf", opts.Filename)
	}
	if opts.ContentType != "application/pdf" {
		t.Errorf("ContentType = %q, want application/pdf", opts.ContentType)
	}
}
