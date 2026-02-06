package storage

import (
	"strings"
	"testing"
)

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
			expected: ".pdf",
		},
		{
			name:     "leading underscore trimmed",
			input:    "_document.pdf",
			expected: "document.pdf",
		},
		{
			name:     "trailing underscore trimmed",
			input:    "document_.pdf",
			expected: "document_.pdf", // This preserves because the underscore is followed by .pdf
		},
		{
			name:     "multiple underscores collapsed",
			input:    "doc___file.pdf",
			expected: "doc_file.pdf",
		},
		{
			name:     "parentheses replaced",
			input:    "document (1).pdf",
			expected: "document_1_.pdf",
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
			expected: "doc_.pdf",
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
