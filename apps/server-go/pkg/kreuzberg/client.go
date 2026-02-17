// Package kreuzberg provides an HTTP client for the Kreuzberg document extraction service.
//
// Kreuzberg is a document parsing service that extracts text, tables, and images
// from various document formats (PDF, DOCX, images with OCR, etc.).
// See: https://github.com/Goldziher/kreuzberg
package kreuzberg

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/emergent-company/emergent/internal/config"
	"github.com/emergent-company/emergent/pkg/logger"
	"go.uber.org/fx"
)

// Module provides the Kreuzberg client as an fx module
var Module = fx.Module("kreuzberg",
	fx.Provide(NewClient),
)

// Client is an HTTP client for the Kreuzberg document extraction service
type Client struct {
	httpClient *http.Client
	baseURL    string
	timeout    time.Duration
	enabled    bool
	log        *slog.Logger
}

// NewClient creates a new Kreuzberg client
func NewClient(cfg *config.Config, log *slog.Logger) *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: cfg.Kreuzberg.Timeout(),
		},
		baseURL: cfg.Kreuzberg.ServiceURL,
		timeout: cfg.Kreuzberg.Timeout(),
		enabled: cfg.Kreuzberg.Enabled,
		log:     log.With(logger.Scope("kreuzberg")),
	}
}

// IsEnabled returns true if Kreuzberg service is enabled
func (c *Client) IsEnabled() bool {
	return c.enabled
}

// ExtractResult is the response from Kreuzberg document extraction
type ExtractResult struct {
	// Content is the extracted text content from the document
	Content string `json:"content"`

	// Metadata contains document metadata extracted during parsing
	Metadata *ExtractMetadata `json:"metadata,omitempty"`

	// Tables extracted from the document
	Tables []ExtractedTable `json:"tables,omitempty"`

	// Images extracted from the document
	Images []ExtractedImage `json:"images,omitempty"`
}

// ExtractMetadata contains document metadata
type ExtractMetadata struct {
	PageCount        *int   `json:"page_count,omitempty"`
	Title            string `json:"title,omitempty"`
	Author           string `json:"author,omitempty"`
	CreationDate     string `json:"creation_date,omitempty"`
	ModificationDate string `json:"modification_date,omitempty"`
	Producer         string `json:"producer,omitempty"`
}

// ExtractedTable represents a table extracted from a document
type ExtractedTable struct {
	Page int        `json:"page,omitempty"`
	Data [][]string `json:"data"`
}

// ExtractedImage represents an image extracted from a document
type ExtractedImage struct {
	Page     int    `json:"page,omitempty"`
	Data     string `json:"data"`      // Base64-encoded image data
	MimeType string `json:"mime_type"` // e.g., 'image/png', 'image/jpeg'
}

// OCRConfig contains OCR-specific configuration for Kreuzberg
type OCRConfig struct {
	Backend  string `json:"backend,omitempty"`  // "tesseract", "easyocr", "paddleocr"
	Language string `json:"language,omitempty"` // "eng", "deu", "eng+deu"
}

// ExtractConfig is the JSON configuration sent to Kreuzberg's /extract endpoint
type ExtractConfig struct {
	OCR      *OCRConfig `json:"ocr,omitempty"`
	ForceOCR bool       `json:"force_ocr,omitempty"`
}

// ExtractOptions contains options for extraction requests
type ExtractOptions struct {
	// TimeoutMs overrides the default timeout for this request
	TimeoutMs int
	// ExtractTables enables table extraction
	ExtractTables bool
	// ExtractImages enables image extraction
	ExtractImages bool
	// OCRLanguage is the language hint for OCR (e.g., "eng", "deu")
	OCRLanguage string
	// OCRBackend specifies the OCR backend to use (e.g., "tesseract", "easyocr")
	OCRBackend string
	// ForceOCR forces OCR on all pages, even if text layer exists
	ForceOCR bool
}

// HealthResponse is the health check response from Kreuzberg
type HealthResponse struct {
	Status  string                 `json:"status"` // "healthy" or "unhealthy"
	Version string                 `json:"version,omitempty"`
	Details map[string]interface{} `json:"details,omitempty"`
}

// Error represents a Kreuzberg service error
type Error struct {
	// Message is the human-friendly error message
	Message string
	// Detail is the technical error detail
	Detail string
	// StatusCode is the HTTP status code
	StatusCode int
}

func (e *Error) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("%s: %s", e.Message, e.Detail)
	}
	return e.Message
}

// humanFriendlyMessages maps technical errors to user-friendly messages
var humanFriendlyMessages = map[string]string{
	"No txBody found":         "This PowerPoint file contains shapes without text content that cannot be parsed.",
	"Unsupported file format": "This file format is not supported for text extraction.",
	"Invalid PDF":             "This PDF file appears to be corrupted or invalid.",
	"Invalid file":            "This file appears to be corrupted or in an unrecognized format.",
	"Empty content":           "No text content could be extracted from this file.",
	"File too large":          "This file exceeds the maximum size limit for processing.",
	"Processing timeout":      "The file took too long to process.",
	"LibreOffice":             "This file format requires LibreOffice for conversion, which is not available.",
	"libreoffice":             "This file format requires LibreOffice for conversion, which is not available.",
	"soffice not found":       "LibreOffice is not installed. Legacy Office formats require LibreOffice.",
}

// getHumanFriendlyMessage converts technical errors to user-friendly messages
func getHumanFriendlyMessage(technical string, detail string) string {
	for pattern, friendly := range humanFriendlyMessages {
		if strings.Contains(technical, pattern) || strings.Contains(detail, pattern) {
			return friendly
		}
	}
	if detail != "" {
		return fmt.Sprintf("%s (%s)", technical, detail)
	}
	return technical
}

// ExtractText extracts text and content from a document
func (c *Client) ExtractText(ctx context.Context, content []byte, filename, mimeType string, opts *ExtractOptions) (*ExtractResult, error) {
	if !c.enabled {
		return nil, &Error{
			Message:    "Kreuzberg document parsing is not enabled",
			StatusCode: http.StatusServiceUnavailable,
		}
	}

	startTime := time.Now()
	c.log.Debug("extracting text from document",
		slog.String("filename", filename),
		slog.String("mime_type", mimeType),
		slog.Int("size_bytes", len(content)),
	)

	// Determine timeout
	timeout := c.timeout
	if opts != nil && opts.TimeoutMs > 0 {
		timeout = time.Duration(opts.TimeoutMs) * time.Millisecond
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Create multipart form data
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Kreuzberg expects the field name to be 'file' (singular)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return nil, fmt.Errorf("create form file: %w", err)
	}
	if _, err := part.Write(content); err != nil {
		return nil, fmt.Errorf("write file content: %w", err)
	}

	if opts != nil && (opts.OCRLanguage != "" || opts.OCRBackend != "" || opts.ForceOCR) {
		config := ExtractConfig{
			ForceOCR: opts.ForceOCR,
		}
		if opts.OCRLanguage != "" || opts.OCRBackend != "" {
			backend := opts.OCRBackend
			if backend == "" {
				backend = "tesseract"
			}
			config.OCR = &OCRConfig{
				Backend:  backend,
				Language: opts.OCRLanguage,
			}
		}
		configJSON, err := json.Marshal(config)
		if err != nil {
			return nil, fmt.Errorf("marshal OCR config: %w", err)
		}
		if err := writer.WriteField("config", string(configJSON)); err != nil {
			return nil, fmt.Errorf("write config field: %w", err)
		}
		c.log.Debug("OCR config added to request",
			slog.String("config", string(configJSON)),
		)
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close multipart writer: %w", err)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/extract", &buf)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Accept", "application/json")

	// Send request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, &Error{
				Message:    fmt.Sprintf("Kreuzberg request timed out for %s", filename),
				StatusCode: http.StatusRequestTimeout,
			}
		}
		return nil, &Error{
			Message:    fmt.Sprintf("Kreuzberg service unavailable at %s", c.baseURL),
			Detail:     err.Error(),
			StatusCode: http.StatusServiceUnavailable,
		}
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	// Handle error responses
	if resp.StatusCode >= 400 {
		return nil, c.handleErrorResponse(resp.StatusCode, body, filename)
	}

	// Kreuzberg returns a single result object (not an array)
	var result ExtractResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	duration := time.Since(startTime)

	c.log.Info("extraction completed",
		slog.String("filename", filename),
		slog.Int("content_length", len(result.Content)),
		slog.Duration("duration", duration),
	)

	return &result, nil
}

// handleErrorResponse converts HTTP error responses to Error
func (c *Client) handleErrorResponse(statusCode int, body []byte, filename string) *Error {
	// Try to parse error response
	var errResp struct {
		Error   string                 `json:"error"`
		Detail  string                 `json:"detail"`
		Message string                 `json:"message"`
		Type    string                 `json:"type"`
		Context map[string]interface{} `json:"context"`
	}

	var message, detail string

	if err := json.Unmarshal(body, &errResp); err == nil {
		message = errResp.Error
		if message == "" {
			message = errResp.Message
		}
		detail = errResp.Detail
		if errResp.Type != "" && detail == "" {
			detail = fmt.Sprintf("Error type: %s", errResp.Type)
		}
	} else {
		// Plain text error
		message = string(body)
	}

	if message == "" {
		message = fmt.Sprintf("Kreuzberg error for %s", filename)
	}

	c.log.Warn("kreuzberg error",
		slog.String("filename", filename),
		slog.Int("status_code", statusCode),
		slog.String("message", message),
		slog.String("detail", detail),
	)

	return &Error{
		Message:    getHumanFriendlyMessage(message, detail),
		Detail:     detail,
		StatusCode: statusCode,
	}
}

// HealthCheck checks the health status of the Kreuzberg service
func (c *Client) HealthCheck(ctx context.Context) (*HealthResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return nil, fmt.Errorf("create health request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.log.Warn("kreuzberg health check failed", slog.Any("error", err))
		return &HealthResponse{
			Status: "unhealthy",
			Details: map[string]interface{}{
				"error": err.Error(),
			},
		}, nil
	}
	defer resp.Body.Close()

	var health HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return &HealthResponse{
			Status: "unhealthy",
			Details: map[string]interface{}{
				"error": "failed to decode health response",
			},
		}, nil
	}

	return &health, nil
}

// MIME types supported by Kreuzberg for extraction
var KreuzbergSupportedMIMETypes = map[string]bool{
	// Documents
	"application/pdf":    true,
	"application/msword": true,
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document": true,
	"application/vnd.oasis.opendocument.text":                                 true,

	// Spreadsheets
	"application/vnd.ms-excel": true,
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet": true,
	"application/vnd.ms-excel.sheet.macroEnabled.12":                    true,
	"application/vnd.ms-excel.sheet.binary.macroEnabled.12":             true,
	"application/vnd.oasis.opendocument.spreadsheet":                    true,

	// Presentations
	"application/vnd.ms-powerpoint":                                             true,
	"application/vnd.openxmlformats-officedocument.presentationml.presentation": true,

	// Images (for OCR)
	"image/png":                true,
	"image/jpeg":               true,
	"image/gif":                true,
	"image/bmp":                true,
	"image/tiff":               true,
	"image/webp":               true,
	"image/jp2":                true,
	"image/jpx":                true,
	"image/x-portable-anymap":  true,
	"image/x-portable-bitmap":  true,
	"image/x-portable-graymap": true,
	"image/x-portable-pixmap":  true,

	// Email
	"message/rfc822":             true,
	"application/vnd.ms-outlook": true,

	// Web & Markup
	"text/html":     true,
	"image/svg+xml": true,

	// Rich Text
	"application/rtf": true,

	// Archives
	"application/zip":             true,
	"application/x-tar":           true,
	"application/gzip":            true,
	"application/x-gzip":          true,
	"application/x-7z-compressed": true,
}

// MIME types for plain text that bypass Kreuzberg
var PlainTextMIMETypes = map[string]bool{
	"text/plain":                true,
	"text/markdown":             true,
	"text/csv":                  true,
	"text/tab-separated-values": true,
	"text/xml":                  true,
	"application/json":          true,
	"application/xml":           true,
	"application/x-yaml":        true,
	"text/yaml":                 true,
	"application/toml":          true,
}

// Plain text file extensions that bypass Kreuzberg
var PlainTextExtensions = map[string]bool{
	".txt":      true,
	".md":       true,
	".markdown": true,
	".csv":      true,
	".tsv":      true,
	".json":     true,
	".xml":      true,
	".yaml":     true,
	".yml":      true,
	".toml":     true,
}

// Email MIME types that should use native parser
var EmailMIMETypes = map[string]bool{
	"message/rfc822":             true,
	"application/vnd.ms-outlook": true,
}

// Email file extensions
var EmailExtensions = map[string]bool{
	".eml": true,
	".msg": true,
}

// ShouldUseKreuzberg determines if a file should be processed by Kreuzberg
func ShouldUseKreuzberg(mimeType, filename string) bool {
	// Check MIME type first
	if mimeType != "" && PlainTextMIMETypes[mimeType] {
		return false
	}

	// Check file extension as fallback
	if filename != "" {
		ext := strings.ToLower(filepath.Ext(filename))
		if PlainTextExtensions[ext] {
			return false
		}
	}

	// Default to using Kreuzberg for unknown types
	return true
}

// IsEmailFile checks if a file is an email file that should use native parser
func IsEmailFile(mimeType, filename string) bool {
	// Check MIME type first
	if mimeType != "" && EmailMIMETypes[mimeType] {
		return true
	}

	// Check file extension as fallback
	if filename != "" {
		ext := strings.ToLower(filepath.Ext(filename))
		if EmailExtensions[ext] {
			return true
		}
	}

	return false
}

// IsKreuzbergSupported checks if a MIME type is supported by Kreuzberg
func IsKreuzbergSupported(mimeType string) bool {
	return KreuzbergSupportedMIMETypes[mimeType]
}
