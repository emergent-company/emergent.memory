package kreuzberg

import (
	"testing"
)

func TestError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      *Error
		expected string
	}{
		{
			name: "message only",
			err: &Error{
				Message: "Something went wrong",
			},
			expected: "Something went wrong",
		},
		{
			name: "message with detail",
			err: &Error{
				Message: "Parse error",
				Detail:  "invalid JSON at line 5",
			},
			expected: "Parse error: invalid JSON at line 5",
		},
		{
			name: "empty message",
			err: &Error{
				Message: "",
			},
			expected: "",
		},
		{
			name: "empty detail is ignored",
			err: &Error{
				Message: "Error occurred",
				Detail:  "",
			},
			expected: "Error occurred",
		},
		{
			name: "full error with status code",
			err: &Error{
				Message:    "Not found",
				Detail:     "file does not exist",
				StatusCode: 404,
			},
			expected: "Not found: file does not exist",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.err.Error()
			if result != tt.expected {
				t.Errorf("Error() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestGetHumanFriendlyMessage(t *testing.T) {
	tests := []struct {
		name      string
		technical string
		detail    string
		expected  string
	}{
		{
			name:      "no txBody found",
			technical: "Parse error",
			detail:    "No txBody found in slide",
			expected:  "This PowerPoint file contains shapes without text content that cannot be parsed.",
		},
		{
			name:      "unsupported file format",
			technical: "Unsupported file format",
			detail:    "",
			expected:  "This file format is not supported for text extraction.",
		},
		{
			name:      "invalid PDF",
			technical: "Invalid PDF structure",
			detail:    "",
			expected:  "This PDF file appears to be corrupted or invalid.",
		},
		{
			name:      "invalid file",
			technical: "Invalid file header",
			detail:    "",
			expected:  "This file appears to be corrupted or in an unrecognized format.",
		},
		{
			name:      "empty content",
			technical: "Empty content returned",
			detail:    "",
			expected:  "No text content could be extracted from this file.",
		},
		{
			name:      "file too large",
			technical: "File too large to process",
			detail:    "",
			expected:  "This file exceeds the maximum size limit for processing.",
		},
		{
			name:      "processing timeout",
			technical: "Processing timeout exceeded",
			detail:    "",
			expected:  "The file took too long to process.",
		},
		{
			name:      "LibreOffice required",
			technical: "LibreOffice conversion failed",
			detail:    "",
			expected:  "This file format requires LibreOffice for conversion, which is not available.",
		},
		{
			name:      "libreoffice lowercase",
			technical: "libreoffice not available",
			detail:    "",
			expected:  "This file format requires LibreOffice for conversion, which is not available.",
		},
		{
			name:      "soffice not found",
			technical: "Command failed",
			detail:    "soffice not found in PATH",
			expected:  "LibreOffice is not installed. Legacy Office formats require LibreOffice.",
		},
		{
			name:      "unknown error with detail",
			technical: "Unknown error",
			detail:    "something specific happened",
			expected:  "Unknown error (something specific happened)",
		},
		{
			name:      "unknown error without detail",
			technical: "Some technical error",
			detail:    "",
			expected:  "Some technical error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getHumanFriendlyMessage(tt.technical, tt.detail)
			if result != tt.expected {
				t.Errorf("getHumanFriendlyMessage(%q, %q) = %q, want %q",
					tt.technical, tt.detail, result, tt.expected)
			}
		})
	}
}

func TestShouldUseKreuzberg(t *testing.T) {
	tests := []struct {
		name     string
		mimeType string
		filename string
		expected bool
	}{
		// Plain text MIME types - should NOT use Kreuzberg
		{
			name:     "plain text mime type",
			mimeType: "text/plain",
			filename: "document.txt",
			expected: false,
		},
		{
			name:     "markdown mime type",
			mimeType: "text/markdown",
			filename: "README.md",
			expected: false,
		},
		{
			name:     "JSON mime type",
			mimeType: "application/json",
			filename: "data.json",
			expected: false,
		},
		{
			name:     "XML mime type",
			mimeType: "application/xml",
			filename: "config.xml",
			expected: false,
		},
		{
			name:     "YAML mime type",
			mimeType: "application/x-yaml",
			filename: "config.yaml",
			expected: false,
		},
		{
			name:     "CSV mime type",
			mimeType: "text/csv",
			filename: "data.csv",
			expected: false,
		},

		// Plain text extensions (no mime type) - should NOT use Kreuzberg
		{
			name:     "txt extension only",
			mimeType: "",
			filename: "document.txt",
			expected: false,
		},
		{
			name:     "md extension only",
			mimeType: "",
			filename: "README.md",
			expected: false,
		},
		{
			name:     "markdown extension",
			mimeType: "",
			filename: "notes.markdown",
			expected: false,
		},
		{
			name:     "json extension only",
			mimeType: "",
			filename: "config.json",
			expected: false,
		},
		{
			name:     "yaml extension",
			mimeType: "",
			filename: "config.yaml",
			expected: false,
		},
		{
			name:     "yml extension",
			mimeType: "",
			filename: "config.yml",
			expected: false,
		},
		{
			name:     "toml extension",
			mimeType: "",
			filename: "settings.toml",
			expected: false,
		},
		{
			name:     "csv extension only",
			mimeType: "",
			filename: "data.csv",
			expected: false,
		},
		{
			name:     "tsv extension",
			mimeType: "",
			filename: "data.tsv",
			expected: false,
		},
		{
			name:     "xml extension only",
			mimeType: "",
			filename: "config.xml",
			expected: false,
		},

		// Non-plain text types - SHOULD use Kreuzberg
		{
			name:     "PDF file",
			mimeType: "application/pdf",
			filename: "document.pdf",
			expected: true,
		},
		{
			name:     "Word document",
			mimeType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
			filename: "document.docx",
			expected: true,
		},
		{
			name:     "PowerPoint file",
			mimeType: "application/vnd.openxmlformats-officedocument.presentationml.presentation",
			filename: "slides.pptx",
			expected: true,
		},
		{
			name:     "unknown file type",
			mimeType: "",
			filename: "unknown.xyz",
			expected: true,
		},
		{
			name:     "no mime or filename",
			mimeType: "",
			filename: "",
			expected: true,
		},

		// Case insensitivity for extensions
		{
			name:     "uppercase TXT extension",
			mimeType: "",
			filename: "DOCUMENT.TXT",
			expected: false,
		},
		{
			name:     "mixed case MD extension",
			mimeType: "",
			filename: "README.Md",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ShouldUseKreuzberg(tt.mimeType, tt.filename)
			if result != tt.expected {
				t.Errorf("ShouldUseKreuzberg(%q, %q) = %v, want %v",
					tt.mimeType, tt.filename, result, tt.expected)
			}
		})
	}
}

func TestIsEmailFile(t *testing.T) {
	tests := []struct {
		name     string
		mimeType string
		filename string
		expected bool
	}{
		// Email MIME types
		{
			name:     "RFC822 mime type",
			mimeType: "message/rfc822",
			filename: "email.eml",
			expected: true,
		},
		{
			name:     "MS Outlook mime type",
			mimeType: "application/vnd.ms-outlook",
			filename: "message.msg",
			expected: true,
		},

		// Email file extensions (no mime type)
		{
			name:     "eml extension only",
			mimeType: "",
			filename: "message.eml",
			expected: true,
		},
		{
			name:     "msg extension only",
			mimeType: "",
			filename: "outlook.msg",
			expected: true,
		},

		// Case insensitivity
		{
			name:     "uppercase EML extension",
			mimeType: "",
			filename: "MESSAGE.EML",
			expected: true,
		},
		{
			name:     "mixed case MSG extension",
			mimeType: "",
			filename: "Email.Msg",
			expected: true,
		},

		// Non-email files
		{
			name:     "PDF file",
			mimeType: "application/pdf",
			filename: "document.pdf",
			expected: false,
		},
		{
			name:     "text file",
			mimeType: "text/plain",
			filename: "notes.txt",
			expected: false,
		},
		{
			name:     "empty inputs",
			mimeType: "",
			filename: "",
			expected: false,
		},
		{
			name:     "unknown extension",
			mimeType: "",
			filename: "file.xyz",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsEmailFile(tt.mimeType, tt.filename)
			if result != tt.expected {
				t.Errorf("IsEmailFile(%q, %q) = %v, want %v",
					tt.mimeType, tt.filename, result, tt.expected)
			}
		})
	}
}

func TestIsKreuzbergSupported(t *testing.T) {
	tests := []struct {
		name     string
		mimeType string
		expected bool
	}{
		// Supported MIME types
		{
			name:     "PDF",
			mimeType: "application/pdf",
			expected: true,
		},
		{
			name:     "Word docx",
			mimeType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
			expected: true,
		},
		{
			name:     "PowerPoint pptx",
			mimeType: "application/vnd.openxmlformats-officedocument.presentationml.presentation",
			expected: true,
		},
		{
			name:     "Excel xlsx",
			mimeType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
			expected: true,
		},
		{
			name:     "legacy Word doc",
			mimeType: "application/msword",
			expected: true,
		},
		{
			name:     "legacy Excel xls",
			mimeType: "application/vnd.ms-excel",
			expected: true,
		},
		{
			name:     "legacy PowerPoint ppt",
			mimeType: "application/vnd.ms-powerpoint",
			expected: true,
		},
		{
			name:     "HTML",
			mimeType: "text/html",
			expected: true,
		},
		{
			name:     "SVG",
			mimeType: "image/svg+xml",
			expected: true,
		},
		{
			name:     "RTF",
			mimeType: "application/rtf",
			expected: true,
		},
		{
			name:     "ZIP",
			mimeType: "application/zip",
			expected: true,
		},

		// Unsupported MIME types
		{
			name:     "plain text",
			mimeType: "text/plain",
			expected: false,
		},
		{
			name:     "JSON",
			mimeType: "application/json",
			expected: false,
		},
		{
			name:     "unknown type",
			mimeType: "application/unknown",
			expected: false,
		},
		{
			name:     "empty string",
			mimeType: "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsKreuzbergSupported(tt.mimeType)
			if result != tt.expected {
				t.Errorf("IsKreuzbergSupported(%q) = %v, want %v",
					tt.mimeType, result, tt.expected)
			}
		})
	}
}

func TestPlainTextMIMETypes(t *testing.T) {
	// Verify expected MIME types are in the map
	expectedTypes := []string{
		"text/plain",
		"text/markdown",
		"text/csv",
		"text/tab-separated-values",
		"text/xml",
		"application/json",
		"application/xml",
		"application/x-yaml",
		"text/yaml",
		"application/toml",
	}

	for _, mimeType := range expectedTypes {
		if !PlainTextMIMETypes[mimeType] {
			t.Errorf("PlainTextMIMETypes missing expected type: %q", mimeType)
		}
	}
}

func TestPlainTextExtensions(t *testing.T) {
	// Verify expected extensions are in the map
	expectedExts := []string{
		".txt",
		".md",
		".markdown",
		".csv",
		".tsv",
		".json",
		".xml",
		".yaml",
		".yml",
		".toml",
	}

	for _, ext := range expectedExts {
		if !PlainTextExtensions[ext] {
			t.Errorf("PlainTextExtensions missing expected extension: %q", ext)
		}
	}
}

func TestEmailMIMETypes(t *testing.T) {
	// Verify expected MIME types are in the map
	expectedTypes := []string{
		"message/rfc822",
		"application/vnd.ms-outlook",
	}

	for _, mimeType := range expectedTypes {
		if !EmailMIMETypes[mimeType] {
			t.Errorf("EmailMIMETypes missing expected type: %q", mimeType)
		}
	}
}

func TestEmailExtensions(t *testing.T) {
	// Verify expected extensions are in the map
	expectedExts := []string{
		".eml",
		".msg",
	}

	for _, ext := range expectedExts {
		if !EmailExtensions[ext] {
			t.Errorf("EmailExtensions missing expected extension: %q", ext)
		}
	}
}
