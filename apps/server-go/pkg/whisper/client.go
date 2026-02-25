// Package whisper provides an HTTP client for the Whisper audio transcription service.
//
// Whisper is a self-hosted audio transcription service based on OpenAI's Whisper model.
// It accepts audio file uploads via HTTP multipart form and returns plaintext transcripts.
// See: https://github.com/onerahmet/openai-whisper-asr-webservice
package whisper

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/emergent-company/emergent/internal/config"
	"github.com/emergent-company/emergent/pkg/logger"
	"go.uber.org/fx"
)

// Module provides the Whisper client as an fx module
var Module = fx.Module("whisper",
	fx.Provide(NewClient),
)

// Client is an HTTP client for the Whisper audio transcription service
type Client struct {
	httpClient    *http.Client
	baseURL       string
	timeout       time.Duration
	enabled       bool
	language      string
	maxFileSizeMB int
	log           *slog.Logger
}

// NewClient creates a new Whisper client
func NewClient(cfg *config.Config, log *slog.Logger) *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: cfg.Whisper.Timeout(),
		},
		baseURL:       cfg.Whisper.ServiceURL,
		timeout:       cfg.Whisper.Timeout(),
		enabled:       cfg.Whisper.Enabled,
		language:      cfg.Whisper.Language,
		maxFileSizeMB: cfg.Whisper.MaxFileSizeMB,
		log:           log.With(logger.Scope("whisper")),
	}
}

// IsEnabled returns true if the Whisper service is enabled
func (c *Client) IsEnabled() bool {
	return c.enabled
}

// MaxFileSizeBytes returns the maximum allowed audio file size in bytes
func (c *Client) MaxFileSizeBytes() int64 {
	return int64(c.maxFileSizeMB) * 1024 * 1024
}

// Transcribe sends an audio file to the Whisper service and returns the plaintext transcript.
// The audio data is sent as multipart/form-data with field name "audio_file".
// The service endpoint is POST /asr?output=txt&task=transcribe.
// initialPrompt optionally seeds the decoder with context (names, vocabulary, style hints).
func (c *Client) Transcribe(ctx context.Context, data []byte, filename, mimeType, initialPrompt string) (string, error) {
	if !c.enabled {
		return "", fmt.Errorf("whisper transcription service is disabled")
	}

	startTime := time.Now()
	c.log.Debug("transcribing audio file",
		slog.String("filename", filename),
		slog.String("mime_type", mimeType),
		slog.Int("size_bytes", len(data)),
	)

	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	// Build multipart form body
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	part, err := writer.CreateFormFile("audio_file", filename)
	if err != nil {
		return "", fmt.Errorf("create form file: %w", err)
	}
	if _, err := part.Write(data); err != nil {
		return "", fmt.Errorf("write audio content: %w", err)
	}
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("close multipart writer: %w", err)
	}

	// Build URL with query parameters
	endpoint, err := url.Parse(c.baseURL + "/asr")
	if err != nil {
		return "", fmt.Errorf("parse service URL: %w", err)
	}
	q := endpoint.Query()
	q.Set("output", "txt")
	q.Set("task", "transcribe")
	if c.language != "" {
		q.Set("language", c.language)
	}
	if initialPrompt != "" {
		q.Set("initial_prompt", initialPrompt)
	}
	endpoint.RawQuery = q.Encode()

	// Create request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), &buf)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Accept", "text/plain")

	// Send request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("whisper transcription timed out for %s after %s", filename, c.timeout)
		}
		return "", fmt.Errorf("whisper service unavailable at %s: %w", c.baseURL, err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response body: %w", err)
	}

	// Handle non-200 responses
	if resp.StatusCode >= 400 {
		excerpt := string(body)
		if len(excerpt) > 200 {
			excerpt = excerpt[:200] + "..."
		}
		return "", fmt.Errorf("whisper service returned %d for %s: %s", resp.StatusCode, filename, strings.TrimSpace(excerpt))
	}

	transcript := strings.TrimSpace(string(body))
	duration := time.Since(startTime)

	c.log.Info("transcription completed",
		slog.String("filename", filename),
		slog.Int("transcript_length", len(transcript)),
		slog.Duration("duration", duration),
	)

	return transcript, nil
}
