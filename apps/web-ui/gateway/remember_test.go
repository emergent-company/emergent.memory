package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
)

// captureCall is one RememberCapture call recorded by fakeMemory.
type captureCall struct {
	ProjectID string
	Body      captureRequest
}

// captureFileCall is one RememberCaptureFile call recorded by fakeMemory.
type captureFileCall struct {
	ProjectID string
	Filename  string
	Comment   string
	Body      []byte
}

func (f *fakeMemory) RememberCapture(_ context.Context, projectID string, body captureRequest) (int, []byte, error) {
	if f.captureErr != nil {
		return 0, nil, f.captureErr
	}
	f.captureCalls = append(f.captureCalls, captureCall{ProjectID: projectID, Body: body})
	return f.captureResult()
}

func (f *fakeMemory) RememberCaptureFile(_ context.Context, projectID, filename string, r io.Reader, comment string) (int, []byte, error) {
	if f.captureErr != nil {
		return 0, nil, f.captureErr
	}
	body, _ := io.ReadAll(r)
	f.captureFiles = append(f.captureFiles, captureFileCall{
		ProjectID: projectID,
		Filename:  filename,
		Comment:   comment,
		Body:      body,
	})
	return f.captureResult()
}

func (f *fakeMemory) captureResult() (int, []byte, error) {
	status := f.captureStatus
	if status == 0 {
		status = http.StatusAccepted
	}
	raw := f.captureRaw
	if raw == nil {
		raw = []byte(`{"run_id":"r1","status":"submitted"}`)
	}
	return status, raw, nil
}

// The message the remember agent reads is framed by the gateway, never by the
// client: the user's comment leads, the shared text follows, and the link
// closes as title-over-URL.
func TestFrameCapture(t *testing.T) {
	cases := []struct {
		name       string
		body       captureBody
		wantMsg    string
		wantSource string
		wantOK     bool
	}{
		{
			name:       "link with title and comment",
			body:       captureBody{URL: "https://example.com/a", Title: "A good article", Comment: "read this later"},
			wantMsg:    "read this later\n\nA good article\nhttps://example.com/a",
			wantSource: "https://example.com/a",
			wantOK:     true,
		},
		{
			name:       "link only, no title",
			body:       captureBody{URL: "https://example.com/a"},
			wantMsg:    "https://example.com/a",
			wantSource: "https://example.com/a",
			wantOK:     true,
		},
		{
			name:    "shared text only (no link)",
			body:    captureBody{Text: "the quick brown fox"},
			wantMsg: "the quick brown fox",
			wantOK:  true,
		},
		{
			name:       "comment plus selected text",
			body:       captureBody{Text: "quoted passage", Comment: "worth keeping"},
			wantMsg:    "worth keeping\n\nquoted passage",
			wantSource: "",
			wantOK:     true,
		},
		{
			// A comment identical to the shared text must not appear twice.
			name:    "comment equals text",
			body:    captureBody{Text: "same", Comment: "same"},
			wantMsg: "same",
			wantOK:  true,
		},
		{
			// A non-http string in the url field is not an origin memory can
			// point at, so it is not recorded as a source.
			name:       "non-http url is not a source",
			body:       captureBody{URL: "javascript:alert(1)", Comment: "note"},
			wantMsg:    "note",
			wantSource: "",
			wantOK:     true,
		},
		{
			name:   "nothing to remember",
			body:   captureBody{},
			wantOK: false,
		},
		{
			name:   "whitespace only",
			body:   captureBody{URL: "  ", Title: " ", Text: "\n", Comment: "\t"},
			wantOK: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg, source, ok := frameCapture(tc.body)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v (message %q)", ok, tc.wantOK, msg)
			}
			if msg != tc.wantMsg {
				t.Errorf("message = %q, want %q", msg, tc.wantMsg)
			}
			if source != tc.wantSource {
				t.Errorf("sourceURL = %q, want %q", source, tc.wantSource)
			}
		})
	}
}

// jsonCaptureRequest builds a capture POST with the JSON content type echo's
// binder needs (a share client always sends it).
func jsonCaptureRequest(path, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	return req
}

func captureEcho(f *fakeMemory) *echo.Echo {
	s := &Server{cfg: Config{AuthMode: "session", SessionSecret: "test-secret"}, memory: f}
	e := echo.New()
	e.POST("/api/projects/:projectId/remember", s.rememberCapture)
	e.POST("/api/projects/:projectId/remember/file", s.rememberFileCapture)
	return e
}

// A shared link is passed to memory as an async remember carrying its origin,
// and memory's own 202 body is handed back unchanged so the extension can read
// the run id.
func TestRememberCapture_LinksAndComments(t *testing.T) {
	f := &fakeMemory{}
	e := captureEcho(f)

	body := `{"url":"https://example.com/a","title":"A good article","comment":"read this later"}`
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, jsonCaptureRequest("/api/projects/p1/remember", body))

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (body %s)", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != `{"run_id":"r1","status":"submitted"}` {
		t.Errorf("body = %q, want memory's body verbatim", got)
	}
	if len(f.captureCalls) != 1 {
		t.Fatalf("capture calls = %d, want 1", len(f.captureCalls))
	}
	call := f.captureCalls[0]
	if call.ProjectID != "p1" {
		t.Errorf("project = %q, want p1", call.ProjectID)
	}
	if call.Body.SourceURL != "https://example.com/a" {
		t.Errorf("source_url = %q, want the shared link", call.Body.SourceURL)
	}
	if want := "read this later\n\nA good article\nhttps://example.com/a"; call.Body.Message != want {
		t.Errorf("message = %q, want %q", call.Body.Message, want)
	}
}

// The capture is async by construction: the gateway never lets a caller hold an
// SSE stream open for a share. This pins the client, which is what sets it.
func TestRememberCaptureClient_SendsAsyncMode(t *testing.T) {
	var got captureRequest
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/projects/p1/remember" {
			t.Errorf("upstream path = %q", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"run_id":"r1"}`))
	}))
	defer upstream.Close()

	client := NewMemoryClient(upstream.URL, "")
	status, raw, err := client.RememberCapture(context.Background(), "p1", captureRequest{Message: "note"})
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if status != http.StatusAccepted || string(raw) != `{"run_id":"r1"}` {
		t.Errorf("status %d body %s, want 202 verbatim body", status, raw)
	}
	if got.Mode != "async" {
		t.Errorf("mode = %q, want async", got.Mode)
	}
}

// An empty share is refused before it reaches memory, in memory's own error
// envelope.
func TestRememberCapture_EmptyShareIs400(t *testing.T) {
	f := &fakeMemory{}
	e := captureEcho(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, jsonCaptureRequest("/api/projects/p1/remember", `{"comment":"   "}`))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if len(f.captureCalls) != 0 {
		t.Errorf("memory was called %d times for an empty share", len(f.captureCalls))
	}
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("error body is not JSON: %v", err)
	}
	if envelope.Error.Code != "empty_capture" {
		t.Errorf("error code = %q, want empty_capture", envelope.Error.Code)
	}
}

// A memory refusal is forwarded with memory's status and wording — the client
// shows "no_content"/"unsupported_media_type" verbatim.
func TestRememberCapture_RefusalIsForwarded(t *testing.T) {
	f := &fakeMemory{
		captureStatus: http.StatusUnprocessableEntity,
		captureRaw:    []byte(`{"error":{"code":"no_content","message":"no text"}}`),
	}
	e := captureEcho(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, jsonCaptureRequest("/api/projects/p1/remember", `{"text":"a note"}`))

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	if got := rec.Body.String(); got != `{"error":{"code":"no_content","message":"no text"}}` {
		t.Errorf("body = %q, want memory's refusal verbatim", got)
	}
}

// A transport failure is a retryable 502 in the same envelope, not a panic and
// not memory's URL leaked to the device.
func TestRememberCapture_TransportFailureIs502(t *testing.T) {
	f := &fakeMemory{captureErr: errors.New("dial tcp 10.0.0.1: connect: refused")}
	e := captureEcho(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, jsonCaptureRequest("/api/projects/p1/remember", `{"text":"a note"}`))

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("dial tcp")) {
		t.Errorf("body leaked the transport error: %s", rec.Body.String())
	}
}

func multipartCapture(t *testing.T, filename string, content []byte, comment string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write part: %v", err)
	}
	if comment != "" {
		if err := w.WriteField("comment", comment); err != nil {
			t.Fatalf("write comment: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	return &buf, w.FormDataContentType()
}

// A shared photo travels as bytes with its filename (memory's parser sniffs the
// extension for images) and the comment rides along as the guide, which is the
// fallback content for a photo with no OCR text.
func TestRememberFileCapture_ForwardsFileAndComment(t *testing.T) {
	f := &fakeMemory{}
	e := captureEcho(f)

	body, contentType := multipartCapture(t, "IMG_0001.jpg", []byte("jpegbytes"), "receipt for lunch")
	req := httptest.NewRequest(http.MethodPost, "/api/projects/p1/remember/file", body)
	req.Header.Set(echo.HeaderContentType, contentType)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (body %s)", rec.Code, rec.Body.String())
	}
	if len(f.captureFiles) != 1 {
		t.Fatalf("file captures = %d, want 1", len(f.captureFiles))
	}
	call := f.captureFiles[0]
	if call.ProjectID != "p1" || call.Filename != "IMG_0001.jpg" || call.Comment != "receipt for lunch" {
		t.Errorf("capture = %+v, want project p1, filename IMG_0001.jpg, comment forwarded", call)
	}
	if string(call.Body) != "jpegbytes" {
		t.Errorf("file body = %q, want the shared bytes unchanged", string(call.Body))
	}
}

func TestRememberFileCapture_RequiresFile(t *testing.T) {
	f := &fakeMemory{}
	e := captureEcho(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/projects/p1/remember/file", nil))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if len(f.captureFiles) != 0 {
		t.Errorf("memory was called for a fileless capture")
	}
}

// The capture route is exempt from the gateway's 4 MB body limit and enforces
// its own ceiling instead.
func TestBodyLimitSkipper_ExemptsCaptureUpload(t *testing.T) {
	e := echo.New()
	c := e.NewContext(
		httptest.NewRequest(http.MethodPost, "/api/projects/p1/remember/file", nil),
		httptest.NewRecorder(),
	)
	if !bodyLimitSkipper(c) {
		t.Error("remember/file must be exempt from the 4 MB body limit")
	}
	if maxCaptureUploadSize <= 4*1024*1024 {
		t.Errorf("capture limit %d is not above the global body limit", maxCaptureUploadSize)
	}
}
