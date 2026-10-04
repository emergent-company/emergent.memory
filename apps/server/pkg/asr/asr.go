// Package asr provides vendor speech-to-text (transcription) adapters.
package asr

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// API identifies a transcription vendor wire shape.
type API string

const (
	// APIOpenAITranscriptions is the OpenAI-compatible multipart
	// POST {base}/audio/transcriptions protocol.
	APIOpenAITranscriptions API = "openai-transcriptions"
)

// Options carries per-request transcription parameters.
type Options struct {
	Filename       string // original filename (used as the multipart filename)
	MIMEType       string // informational; content type of the part
	Language       string // optional ISO-639-1 language hint
	ResponseFormat string // optional: "json" (default), "text", "verbose_json"
	Prompt         string // optional decoder prompt
}

// Result is a transcription result.
type Result struct {
	Text     string
	Language string
	Duration float64 // seconds; only populated for verbose_json
}

// Transcriber transcribes encoded audio bytes to text.
type Transcriber interface {
	Transcribe(ctx context.Context, audio []byte, opts Options) (Result, error)
}

// Config configures an ASR adapter.
type Config struct {
	API        API
	BaseURL    string            // vendor base URL; empty -> adapter default
	APIKey     string            // bearer credential
	Model      string            // transcription model name; empty -> adapter default
	Headers    map[string]string // extra headers (from provider ExtraHeaders)
	HTTPClient *http.Client      // optional; default http.Client{Timeout: 120s}
}

// New dispatches on cfg.API and returns the adapter. Unknown API -> error.
func New(cfg Config) (Transcriber, error) {
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 120 * time.Second}
	}
	switch cfg.API {
	case APIOpenAITranscriptions:
		base := cfg.BaseURL
		if base == "" {
			base = openAIDefaultBase
		}
		model := cfg.Model
		if model == "" {
			model = openAIDefaultModel
		}
		return &openAITranscriber{
			baseURL: base,
			apiKey:  cfg.APIKey,
			model:   model,
			headers: cfg.Headers,
			client:  client,
		}, nil
	default:
		return nil, fmt.Errorf("asr: unknown API %q", cfg.API)
	}
}
