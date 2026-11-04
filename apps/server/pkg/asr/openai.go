package asr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
)

const openAIDefaultBase = "https://api.openai.com/v1"
const openAIDefaultModel = "whisper-1"

type openAITranscriber struct {
	baseURL string
	apiKey  string
	model   string
	headers map[string]string
	client  *http.Client
}

func (t *openAITranscriber) endpoint() string {
	trimmed := strings.TrimRight(t.baseURL, "/")
	if strings.HasSuffix(trimmed, "/audio/transcriptions") {
		return trimmed
	}
	return trimmed + "/audio/transcriptions"
}

func (t *openAITranscriber) Transcribe(ctx context.Context, audio []byte, opts Options) (Result, error) {
	if len(audio) == 0 {
		return Result{}, fmt.Errorf("asr: empty audio")
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	filename := opts.Filename
	if filename == "" {
		filename = "audio"
	}
	if opts.MIMEType == "" {
		part, err := writer.CreateFormFile("file", filename)
		if err != nil {
			return Result{}, fmt.Errorf("asr: create form file: %w", err)
		}
		if _, err := part.Write(audio); err != nil {
			return Result{}, fmt.Errorf("asr: write audio: %w", err)
		}
	} else {
		// CreateFormFile hardcodes application/octet-stream; when the caller
		// declares the audio's content type, carry it on the part so vendors
		// that sniff the MIME type (rather than the extension) can accept it.
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{
			"name":     "file",
			"filename": filename,
		}))
		header.Set("Content-Type", opts.MIMEType)
		part, err := writer.CreatePart(header)
		if err != nil {
			return Result{}, fmt.Errorf("asr: create form file: %w", err)
		}
		if _, err := part.Write(audio); err != nil {
			return Result{}, fmt.Errorf("asr: write audio: %w", err)
		}
	}

	if err := writer.WriteField("model", t.model); err != nil {
		return Result{}, fmt.Errorf("asr: write model field: %w", err)
	}
	if opts.Language != "" {
		if err := writer.WriteField("language", opts.Language); err != nil {
			return Result{}, fmt.Errorf("asr: write language field: %w", err)
		}
	}
	if opts.ResponseFormat != "" {
		if err := writer.WriteField("response_format", opts.ResponseFormat); err != nil {
			return Result{}, fmt.Errorf("asr: write response_format field: %w", err)
		}
	}
	if opts.Prompt != "" {
		if err := writer.WriteField("prompt", opts.Prompt); err != nil {
			return Result{}, fmt.Errorf("asr: write prompt field: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		return Result{}, fmt.Errorf("asr: close multipart writer: %w", err)
	}

	url := t.endpoint()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &body)
	if err != nil {
		return Result{}, fmt.Errorf("asr: build request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if t.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+t.apiKey)
	}
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("asr: %s: %w", url, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return Result{}, fmt.Errorf("asr: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		excerpt := string(data)
		if len(excerpt) > 200 {
			excerpt = excerpt[:200]
		}
		return Result{}, fmt.Errorf("asr: %s: status %d: %s", url, resp.StatusCode, excerpt)
	}

	switch opts.ResponseFormat {
	case "text", "srt", "vtt":
		return Result{Text: strings.TrimSpace(string(data))}, nil
	default:
		var parsed struct {
			Text     string  `json:"text"`
			Language string  `json:"language"`
			Duration float64 `json:"duration"`
		}
		if err := json.Unmarshal(data, &parsed); err != nil {
			return Result{}, fmt.Errorf("asr: decode response: %w", err)
		}
		return Result{
			Text:     strings.TrimSpace(parsed.Text),
			Language: parsed.Language,
			Duration: parsed.Duration,
		}, nil
	}
}
