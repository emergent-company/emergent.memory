package xberg

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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

func TestShouldUseXberg(t *testing.T) {
	tests := []struct {
		name     string
		mimeType string
		filename string
		expected bool
	}{
		// Plain text MIME types - should NOT use Xberg
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

		// Plain text extensions (no mime type) - should NOT use Xberg
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

		// Non-plain text types - SHOULD use Xberg
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
			result := ShouldUseXberg(tt.mimeType, tt.filename)
			if result != tt.expected {
				t.Errorf("ShouldUseXberg(%q, %q) = %v, want %v",
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

func TestIsXbergSupported(t *testing.T) {
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
			result := IsXbergSupported(tt.mimeType)
			if result != tt.expected {
				t.Errorf("IsXbergSupported(%q) = %v, want %v",
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

// newExtractTestClient returns a Client wired to an httptest server running
// handler, with the same unexported configuration NewClient would produce.
// It lives in the same package so it can set the internal fields.
func newExtractTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &Client{
		httpClient: srv.Client(),
		baseURL:    srv.URL,
		timeout:    5 * time.Second,
		enabled:    true,
		log:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

// TestExtractText_DecodesSingleResultEnvelope pins the /extract envelope
// contract: the response is {"results":[...],"errors":[...],"summary":{}} (not
// a bare array), and the sole result is decoded into the returned ExtractResult.
func TestExtractText_DecodesSingleResultEnvelope(t *testing.T) {
	var gotPath string
	c := newExtractTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"results": [{
				"content": "hello world",
				"metadata": {"page_count": 2, "title": "Doc"},
				"tables": [],
				"images": []
			}],
			"errors": [],
			"summary": {"num_files": 1}
		}`)
	})

	got, err := c.ExtractText(context.Background(), []byte("pdf-bytes"), "doc.pdf", "application/pdf", nil)
	if err != nil {
		t.Fatalf("ExtractText() error = %v, want nil", err)
	}
	if gotPath != "/extract" {
		t.Errorf("request path = %q, want %q", gotPath, "/extract")
	}
	if got.Content != "hello world" {
		t.Errorf("Content = %q, want %q", got.Content, "hello world")
	}
	if got.Metadata == nil {
		t.Fatal("Metadata = nil, want decoded metadata")
	}
	if got.Metadata.PageCount == nil || *got.Metadata.PageCount != 2 {
		t.Errorf("Metadata.PageCount = %v, want 2", got.Metadata.PageCount)
	}
	if got.Metadata.Title != "Doc" {
		t.Errorf("Metadata.Title = %q, want %q", got.Metadata.Title, "Doc")
	}
}

// TestExtractText_SelectsFirstResultFromMultiResultEnvelope pins which result is
// selected when the envelope carries more than one: the client returns
// results[0]. With the current single-file upload there should only ever be one
// result, but the selection rule is deliberate and must not silently change.
func TestExtractText_SelectsFirstResultFromMultiResultEnvelope(t *testing.T) {
	c := newExtractTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"results":[{"content":"first"},{"content":"second"}],"errors":[],"summary":{}}`)
	})

	got, err := c.ExtractText(context.Background(), []byte("pdf-bytes"), "doc.pdf", "application/pdf", nil)
	if err != nil {
		t.Fatalf("ExtractText() error = %v, want nil", err)
	}
	if got.Content != "first" {
		t.Errorf("Content = %q, want %q (results[0] is selected)", got.Content, "first")
	}
}

// TestExtractText_SurfacesPerInputErrors pins how a per-input failure surfaces:
// when the envelope has no results but carries errors, the first error's `error`
// (falling back to `message`) is run through the friendly-message mapping while
// the joined "file: message" list is preserved as Error.Detail.
func TestExtractText_SurfacesPerInputErrors(t *testing.T) {
	c := newExtractTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"results": [],
			"errors": [{"message": "could not parse", "error": "Invalid PDF", "file": "broken.pdf"}],
			"summary": {"num_files": 1}
		}`)
	})

	_, err := c.ExtractText(context.Background(), []byte("bad-pdf"), "broken.pdf", "application/pdf", nil)
	if err == nil {
		t.Fatal("ExtractText() error = nil, want an error for a per-input failure")
	}

	var xe *Error
	if !errors.As(err, &xe) {
		t.Fatalf("error = %T (%v), want *xberg.Error", err, err)
	}
	if xe.Message != "This PDF file appears to be corrupted or invalid." {
		t.Errorf("Message = %q, want the friendly Invalid PDF message", xe.Message)
	}
	if xe.Detail != "broken.pdf: could not parse" {
		t.Errorf("Detail = %q, want %q", xe.Detail, "broken.pdf: could not parse")
	}
	// The envelope arrived on a 2xx response, so the synthesized Error carries
	// the HTTP status of the response rather than a dedicated error code.
	if xe.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want %d", xe.StatusCode, http.StatusOK)
	}
}

// TestExtractText_JoinsMultiplePerInputErrors pins the detail join for an
// envelope with several failed inputs.
func TestExtractText_JoinsMultiplePerInputErrors(t *testing.T) {
	c := newExtractTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"results": [],
			"errors": [
				{"error": "Invalid PDF", "file": "a.pdf"},
				{"error": "Unsupported file format", "file": "b.docx"}
			],
			"summary": {"num_files": 2}
		}`)
	})

	_, err := c.ExtractText(context.Background(), []byte("bytes"), "a.pdf", "application/pdf", nil)
	var xe *Error
	if !errors.As(err, &xe) {
		t.Fatalf("error = %T (%v), want *xberg.Error", err, err)
	}
	wantDetail := "a.pdf: Invalid PDF; b.docx: Unsupported file format"
	if xe.Detail != wantDetail {
		t.Errorf("Detail = %q, want %q", xe.Detail, wantDetail)
	}
}
