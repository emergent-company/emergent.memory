package sse

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mockFlusher is an http.ResponseWriter that also implements http.Flusher
type mockFlusher struct {
	*httptest.ResponseRecorder
	flushCalled int
}

func (m *mockFlusher) Flush() {
	m.flushCalled++
}

func newMockFlusher() *mockFlusher {
	return &mockFlusher{
		ResponseRecorder: httptest.NewRecorder(),
	}
}

func TestNewWriter(t *testing.T) {
	w := httptest.NewRecorder()
	sseWriter := NewWriter(w)

	if sseWriter == nil {
		t.Fatal("NewWriter returned nil")
	}
	if sseWriter.w != w {
		t.Error("NewWriter didn't set the ResponseWriter")
	}
	if sseWriter.started {
		t.Error("NewWriter should not start the writer")
	}
	if sseWriter.closed {
		t.Error("NewWriter should not close the writer")
	}
}

func TestWriterStart(t *testing.T) {
	w := newMockFlusher()
	sseWriter := NewWriter(w)

	// First start should set headers
	err := sseWriter.Start()
	if err != nil {
		t.Errorf("Start() returned error: %v", err)
	}

	// Check headers
	if w.Header().Get("Content-Type") != "text/event-stream" {
		t.Errorf("Content-Type = %q, want %q", w.Header().Get("Content-Type"), "text/event-stream")
	}
	if w.Header().Get("Cache-Control") != "no-cache" {
		t.Errorf("Cache-Control = %q, want %q", w.Header().Get("Cache-Control"), "no-cache")
	}
	if w.Header().Get("Connection") != "keep-alive" {
		t.Errorf("Connection = %q, want %q", w.Header().Get("Connection"), "keep-alive")
	}
	if w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want %q", w.Header().Get("X-Content-Type-Options"), "nosniff")
	}

	// Verify flush was called
	if w.flushCalled != 1 {
		t.Errorf("Flush called %d times, want 1", w.flushCalled)
	}

	// Second start should be a no-op
	err = sseWriter.Start()
	if err != nil {
		t.Errorf("Second Start() returned error: %v", err)
	}
	if w.flushCalled != 1 {
		t.Errorf("Second Start() should not flush again, got %d flushes", w.flushCalled)
	}
}

func TestWriterWriteEvent(t *testing.T) {
	tests := []struct {
		name       string
		eventName  string
		data       any
		wantOutput string
	}{
		{
			name:       "named event with string",
			eventName:  "message",
			data:       "hello",
			wantOutput: "event: message\ndata: \"hello\"\n\n",
		},
		{
			name:       "named event with object",
			eventName:  "update",
			data:       map[string]string{"key": "value"},
			wantOutput: "event: update\ndata: {\"key\":\"value\"}\n\n",
		},
		{
			name:       "empty event name (data only)",
			eventName:  "",
			data:       map[string]int{"count": 42},
			wantOutput: "data: {\"count\":42}\n\n",
		},
		{
			name:       "event with array data",
			eventName:  "items",
			data:       []int{1, 2, 3},
			wantOutput: "event: items\ndata: [1,2,3]\n\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newMockFlusher()
			sseWriter := NewWriter(w)
			sseWriter.started = true // Skip Start() for testing

			err := sseWriter.WriteEvent(tt.eventName, tt.data)
			if err != nil {
				t.Errorf("WriteEvent() returned error: %v", err)
			}

			got := w.Body.String()
			if got != tt.wantOutput {
				t.Errorf("WriteEvent() output = %q, want %q", got, tt.wantOutput)
			}
		})
	}
}

func TestWriterWriteEventClosed(t *testing.T) {
	w := newMockFlusher()
	sseWriter := NewWriter(w)
	sseWriter.Close()

	err := sseWriter.WriteEvent("test", "data")
	if err == nil {
		t.Error("WriteEvent() on closed writer should return error")
	}
	if !strings.Contains(err.Error(), "closed") {
		t.Errorf("WriteEvent() error = %q, want error containing 'closed'", err.Error())
	}
}

func TestWriterWriteData(t *testing.T) {
	w := newMockFlusher()
	sseWriter := NewWriter(w)
	sseWriter.started = true

	err := sseWriter.WriteData(map[string]string{"message": "hello"})
	if err != nil {
		t.Errorf("WriteData() returned error: %v", err)
	}

	got := w.Body.String()
	want := "data: {\"message\":\"hello\"}\n\n"
	if got != want {
		t.Errorf("WriteData() output = %q, want %q", got, want)
	}
}

func TestWriterWriteRaw(t *testing.T) {
	tests := []struct {
		name       string
		data       string
		wantOutput string
	}{
		{
			name:       "simple string",
			data:       "hello world",
			wantOutput: "data: hello world\n\n",
		},
		{
			name:       "JSON string (not re-encoded)",
			data:       `{"key":"value"}`,
			wantOutput: "data: {\"key\":\"value\"}\n\n",
		},
		{
			name:       "empty string",
			data:       "",
			wantOutput: "data: \n\n",
		},
		{
			name:       "multiline string",
			data:       "line1\nline2",
			wantOutput: "data: line1\nline2\n\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newMockFlusher()
			sseWriter := NewWriter(w)
			sseWriter.started = true

			err := sseWriter.WriteRaw(tt.data)
			if err != nil {
				t.Errorf("WriteRaw() returned error: %v", err)
			}

			got := w.Body.String()
			if got != tt.wantOutput {
				t.Errorf("WriteRaw() output = %q, want %q", got, tt.wantOutput)
			}
		})
	}
}

func TestWriterWriteRawClosed(t *testing.T) {
	w := newMockFlusher()
	sseWriter := NewWriter(w)
	sseWriter.Close()

	err := sseWriter.WriteRaw("test")
	if err == nil {
		t.Error("WriteRaw() on closed writer should return error")
	}
	if !strings.Contains(err.Error(), "closed") {
		t.Errorf("WriteRaw() error = %q, want error containing 'closed'", err.Error())
	}
}

func TestWriterWriteComment(t *testing.T) {
	tests := []struct {
		name       string
		comment    string
		wantOutput string
	}{
		{
			name:       "keepalive comment",
			comment:    "keepalive",
			wantOutput: ": keepalive\n\n",
		},
		{
			name:       "empty comment",
			comment:    "",
			wantOutput: ": \n\n",
		},
		{
			name:       "ping comment",
			comment:    "ping",
			wantOutput: ": ping\n\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newMockFlusher()
			sseWriter := NewWriter(w)
			sseWriter.started = true

			err := sseWriter.WriteComment(tt.comment)
			if err != nil {
				t.Errorf("WriteComment() returned error: %v", err)
			}

			got := w.Body.String()
			if got != tt.wantOutput {
				t.Errorf("WriteComment() output = %q, want %q", got, tt.wantOutput)
			}
		})
	}
}

func TestWriterWriteCommentClosed(t *testing.T) {
	w := newMockFlusher()
	sseWriter := NewWriter(w)
	sseWriter.Close()

	err := sseWriter.WriteComment("test")
	if err == nil {
		t.Error("WriteComment() on closed writer should return error")
	}
	if !strings.Contains(err.Error(), "closed") {
		t.Errorf("WriteComment() error = %q, want error containing 'closed'", err.Error())
	}
}

func TestWriterClose(t *testing.T) {
	w := newMockFlusher()
	sseWriter := NewWriter(w)

	if sseWriter.IsClosed() {
		t.Error("New writer should not be closed")
	}

	sseWriter.Close()

	if !sseWriter.IsClosed() {
		t.Error("Writer should be closed after Close()")
	}
}

func TestWriterIsClosed(t *testing.T) {
	w := newMockFlusher()
	sseWriter := NewWriter(w)

	if sseWriter.IsClosed() {
		t.Error("New writer should not be closed")
	}

	sseWriter.Close()

	if !sseWriter.IsClosed() {
		t.Error("Writer should report closed after Close()")
	}
}

func TestWriterWithoutFlusher(t *testing.T) {
	// Use a basic ResponseWriter that doesn't implement Flusher
	w := httptest.NewRecorder()
	sseWriter := NewWriter(w)

	// Start should still work without panic
	err := sseWriter.Start()
	if err != nil {
		t.Errorf("Start() without Flusher returned error: %v", err)
	}

	// WriteEvent should still work
	err = sseWriter.WriteEvent("test", "data")
	if err != nil {
		t.Errorf("WriteEvent() without Flusher returned error: %v", err)
	}

	// WriteRaw should still work
	err = sseWriter.WriteRaw("raw data")
	if err != nil {
		t.Errorf("WriteRaw() without Flusher returned error: %v", err)
	}

	// WriteComment should still work
	err = sseWriter.WriteComment("comment")
	if err != nil {
		t.Errorf("WriteComment() without Flusher returned error: %v", err)
	}
}

func TestWriterStatusCode(t *testing.T) {
	w := newMockFlusher()
	sseWriter := NewWriter(w)

	err := sseWriter.Start()
	if err != nil {
		t.Errorf("Start() returned error: %v", err)
	}

	// Check that status code is 200 OK
	if w.Code != http.StatusOK {
		t.Errorf("Status code = %d, want %d", w.Code, http.StatusOK)
	}
}

// unmarshallableValue is a type that cannot be JSON marshalled
type unmarshallableValue struct {
	Ch chan int
}

func TestWriterWriteEventMarshalError(t *testing.T) {
	w := newMockFlusher()
	sseWriter := NewWriter(w)
	sseWriter.started = true

	// Channels cannot be marshalled to JSON
	err := sseWriter.WriteEvent("test", unmarshallableValue{Ch: make(chan int)})
	if err == nil {
		t.Error("WriteEvent() with unmarshallable data should return error")
	}
	if !strings.Contains(err.Error(), "marshal") {
		t.Errorf("WriteEvent() error = %q, want error containing 'marshal'", err.Error())
	}
}

// errWriter is a writer that always returns an error
type errWriter struct {
	err error
}

func (e *errWriter) Header() http.Header {
	return http.Header{}
}

func (e *errWriter) Write([]byte) (int, error) {
	return 0, e.err
}

func (e *errWriter) WriteHeader(int) {}

func TestWriterWriteEventWriteError(t *testing.T) {
	w := &errWriter{err: http.ErrBodyNotAllowed}
	sseWriter := NewWriter(w)
	sseWriter.started = true

	err := sseWriter.WriteEvent("test", "data")
	if err == nil {
		t.Error("WriteEvent() with failing writer should return error")
	}
}

func TestWriterWriteRawWriteError(t *testing.T) {
	w := &errWriter{err: http.ErrBodyNotAllowed}
	sseWriter := NewWriter(w)
	sseWriter.started = true

	err := sseWriter.WriteRaw("data")
	if err == nil {
		t.Error("WriteRaw() with failing writer should return error")
	}
}

func TestWriterWriteCommentWriteError(t *testing.T) {
	w := &errWriter{err: http.ErrBodyNotAllowed}
	sseWriter := NewWriter(w)
	sseWriter.started = true

	err := sseWriter.WriteComment("comment")
	if err == nil {
		t.Error("WriteComment() with failing writer should return error")
	}
}

// failOnNthWriter is a writer that fails on the nth write call
type failOnNthWriter struct {
	calls int
	failN int
	err   error
}

func (w *failOnNthWriter) Header() http.Header {
	return http.Header{}
}

func (w *failOnNthWriter) Write(p []byte) (int, error) {
	w.calls++
	if w.calls >= w.failN {
		return 0, w.err
	}
	return len(p), nil
}

func (w *failOnNthWriter) WriteHeader(int) {}

func TestWriterWriteEventDataLineError(t *testing.T) {
	// This writer succeeds on the first write (event line) but fails on second (data line)
	w := &failOnNthWriter{failN: 2, err: http.ErrBodyNotAllowed}
	sseWriter := NewWriter(w)
	sseWriter.started = true

	err := sseWriter.WriteEvent("test", "data") // event name will succeed, data line will fail
	if err == nil {
		t.Error("WriteEvent() with failing data line should return error")
	}
}
