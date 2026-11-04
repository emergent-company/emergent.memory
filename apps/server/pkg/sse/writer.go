// Package sse provides Server-Sent Events (SSE) utilities for HTTP streaming responses.
package sse

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// Writer provides a convenient way to write SSE events to an HTTP response.
// It handles header setup, JSON serialization, and flushing.
type Writer struct {
	w       http.ResponseWriter
	flusher http.Flusher
	mu      sync.Mutex
	started bool
	closed  bool
}

// NewWriter creates a new SSE writer from an http.ResponseWriter.
// It does not set headers or flush - call Start() to begin streaming.
func NewWriter(w http.ResponseWriter) *Writer {
	flusher, _ := w.(http.Flusher)
	return &Writer{
		w:       w,
		flusher: flusher,
	}
}

// Start sets the SSE headers and flushes them to the client.
// This should be called after request validation is complete.
// Returns an error if the response doesn't support flushing.
func (s *Writer) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return nil
	}

	s.w.Header().Set("Content-Type", "text/event-stream")
	s.w.Header().Set("Cache-Control", "no-cache")
	s.w.Header().Set("Connection", "keep-alive")
	s.w.Header().Set("X-Content-Type-Options", "nosniff")
	s.w.WriteHeader(http.StatusOK)

	if s.flusher != nil {
		s.flusher.Flush()
	}

	s.started = true
	return nil
}

// WriteEvent writes a named event with JSON data.
// Format: event: {name}\ndata: {json}\n\n
func (s *Writer) WriteEvent(eventName string, data any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return fmt.Errorf("SSE writer is closed")
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal SSE data: %w", err)
	}

	if eventName != "" {
		if _, err := fmt.Fprintf(s.w, "event: %s\n", eventName); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprintf(s.w, "data: %s\n\n", jsonData); err != nil {
		return err
	}

	if s.flusher != nil {
		s.flusher.Flush()
	}

	return nil
}

// WriteData writes a data-only event (no event name).
// Format: data: {json}\n\n
func (s *Writer) WriteData(data any) error {
	return s.WriteEvent("", data)
}

// WriteRaw writes a raw string as SSE data without JSON encoding.
// Format: data: {raw}\n\n
func (s *Writer) WriteRaw(data string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return fmt.Errorf("SSE writer is closed")
	}

	if _, err := fmt.Fprintf(s.w, "data: %s\n\n", data); err != nil {
		return err
	}

	if s.flusher != nil {
		s.flusher.Flush()
	}

	return nil
}

// WriteComment writes an SSE comment (used for keep-alive).
// Format: : {comment}\n\n
func (s *Writer) WriteComment(comment string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return fmt.Errorf("SSE writer is closed")
	}

	if _, err := fmt.Fprintf(s.w, ": %s\n\n", comment); err != nil {
		return err
	}

	if s.flusher != nil {
		s.flusher.Flush()
	}

	return nil
}

// Close marks the writer as closed. No more writes will be accepted.
func (s *Writer) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
}

// IsClosed returns whether the writer has been closed.
func (s *Writer) IsClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}
