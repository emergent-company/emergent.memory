package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"time"
)

// Capture timeouts. A text capture is a write that answers 202 as soon as the
// run exists (memory's async remember mode), so the default client deadline is
// generous enough. The file capture is not: memory's remember/file polls
// document conversion for up to 120 s before it answers at all, on top of the
// upload and the agent run, so it gets its own longer-bounded client. Sharing a
// photo must not look like a timeout halfway through the conversion.
const (
	captureTextTimeout = 60 * time.Second
	captureFileTimeout = 5 * time.Minute
)

// captureRequest is memory's remember body for a text capture. The gateway
// composes message/guide — a client never invents the prose the agent reads.
type captureRequest struct {
	Message      string `json:"message"`
	Guide        string `json:"guide,omitempty"`
	SourceURL    string `json:"source_url,omitempty"`
	Mode         string `json:"mode"`
	SchemaPolicy string `json:"schema_policy,omitempty"`
}

// RememberCapture posts a capture to memory's project remember endpoint and
// returns the upstream status and raw JSON body. Async mode is used so the
// caller gets the run id immediately instead of an SSE stream it would have to
// drain.
func (m *MemoryClient) RememberCapture(ctx context.Context, projectID string, body captureRequest) (int, []byte, error) {
	body.Mode = "async"
	payload, err := json.Marshal(body)
	if err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.captureURL(projectID, ""), bytes.NewReader(payload))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	m.authorizeCapture(req, ctx)
	return m.doCapture(req, captureTextTimeout)
}

// RememberCaptureFile posts a shared file (a photo, a scanned PDF, an audio
// memo) to memory's remember/file endpoint. filename names the part so memory's
// MIME/extension sniffing sees what the user actually shared, and comment
// becomes the remember guide — which is also the fallback content when the
// parser finds no text (a photo with no OCR text).
func (m *MemoryClient) RememberCaptureFile(
	ctx context.Context,
	projectID string,
	filename string,
	r io.Reader,
	comment string,
) (int, []byte, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if filename == "" {
		filename = "shared-capture"
	}
	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		return 0, nil, err
	}
	if _, err := io.Copy(part, r); err != nil {
		return 0, nil, err
	}
	if err := w.WriteField("mode", "async"); err != nil {
		return 0, nil, err
	}
	if comment != "" {
		if err := w.WriteField("guide", comment); err != nil {
			return 0, nil, err
		}
	}
	if err := w.Close(); err != nil {
		return 0, nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.captureURL(projectID, "/file"), &buf)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	m.authorizeCapture(req, ctx)
	return m.doCapture(req, captureFileTimeout)
}

func (m *MemoryClient) captureURL(projectID, suffix string) string {
	return m.baseURL + "/api/projects/" + url.PathEscape(projectID) + "/remember" + suffix
}

// authorizeCapture carries the caller's credential verbatim (a device
// credential is forwarded as-is; memory re-derives the project from it) plus
// the session's project headers when a session context exists.
func (m *MemoryClient) authorizeCapture(req *http.Request, ctx context.Context) {
	req.Header.Set("Authorization", "Bearer "+m.tokenFor(ctx))
	if projectID := m.projectIDFor(ctx); projectID != "" {
		req.Header.Set("X-Project-ID", projectID)
	}
	for k, v := range sessionHeaders(ctx) {
		req.Header.Set(k, v)
	}
}

// doCapture performs one capture request and returns the upstream status with
// its raw body, so the handler can hand memory's JSON straight back (it already
// carries {run_id, status} or a structured error the iOS client can show).
// Non-2xx bodies are returned to the caller rather than raised: the client must
// see memory's reason (422 no_content, 415 unsupported_media_type, …).
func (m *MemoryClient) doCapture(req *http.Request, timeout time.Duration) (int, []byte, error) {
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("capture request to memory failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("capture response from memory could not be read: %w", err)
	}
	return resp.StatusCode, raw, nil
}
