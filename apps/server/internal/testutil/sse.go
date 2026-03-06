package testutil

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
)

// SSEEvent represents a Server-Sent Event
type SSEEvent struct {
	Event string // The event type (from "event:" line)
	Data  string // The data payload (from "data:" line)
	ID    string // The event ID (from "id:" line)
	Retry int    // Retry interval in milliseconds (from "retry:" line)
}

// SSEResponse wraps an HTTP response with parsed SSE events
type SSEResponse struct {
	StatusCode  int
	ContentType string
	Events      []SSEEvent
	RawBody     string
}

// ParseSSEResponse parses a Server-Sent Events response body into individual events.
// SSE format: each event is separated by double newlines (\n\n)
// Each event can have multiple lines: event:, data:, id:, retry:
func ParseSSEResponse(body io.Reader) ([]SSEEvent, error) {
	var events []SSEEvent
	scanner := bufio.NewScanner(body)

	var currentEvent SSEEvent
	var dataLines []string

	for scanner.Scan() {
		line := scanner.Text()

		// Empty line marks end of event
		if line == "" {
			if len(dataLines) > 0 || currentEvent.Event != "" || currentEvent.ID != "" {
				currentEvent.Data = strings.Join(dataLines, "\n")
				events = append(events, currentEvent)
				currentEvent = SSEEvent{}
				dataLines = nil
			}
			continue
		}

		// Parse SSE fields
		if strings.HasPrefix(line, "event:") {
			currentEvent.Event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		} else if strings.HasPrefix(line, "id:") {
			currentEvent.ID = strings.TrimSpace(strings.TrimPrefix(line, "id:"))
		} else if strings.HasPrefix(line, "retry:") {
			// Parse retry as int (ignore errors for simplicity)
			var retry int
			_, _ = strings.NewReader(strings.TrimSpace(strings.TrimPrefix(line, "retry:"))).Read([]byte{})
			currentEvent.Retry = retry
		}
		// Lines starting with : are comments, ignore them
	}

	// Handle last event if no trailing newline
	if len(dataLines) > 0 || currentEvent.Event != "" || currentEvent.ID != "" {
		currentEvent.Data = strings.Join(dataLines, "\n")
		events = append(events, currentEvent)
	}

	return events, scanner.Err()
}

// ParseSSEResponseFromBytes parses SSE events from a byte slice
func ParseSSEResponseFromBytes(data []byte) ([]SSEEvent, error) {
	return ParseSSEResponse(bytes.NewReader(data))
}

// ParseSSEJSON attempts to parse the Data field of an SSE event as JSON
func (e *SSEEvent) ParseSSEJSON(v any) error {
	return json.Unmarshal([]byte(e.Data), v)
}

// GetSSE performs a GET request expecting an SSE response and parses the events
func (s *TestServer) GetSSE(path string, opts ...RequestOption) *SSEResponse {
	// Add Accept header for SSE
	opts = append(opts, WithHeader("Accept", "text/event-stream"))

	req := httptest.NewRequest(http.MethodGet, path, nil)
	for _, opt := range opts {
		opt(req)
	}

	rec := httptest.NewRecorder()
	s.Echo.ServeHTTP(rec, req)

	events, _ := ParseSSEResponseFromBytes(rec.Body.Bytes())

	return &SSEResponse{
		StatusCode:  rec.Code,
		ContentType: rec.Header().Get("Content-Type"),
		Events:      events,
		RawBody:     rec.Body.String(),
	}
}

// PostSSE performs a POST request expecting an SSE response and parses the events
func (s *TestServer) PostSSE(path string, opts ...RequestOption) *SSEResponse {
	// Add Accept header for SSE
	opts = append(opts, WithHeader("Accept", "text/event-stream"))

	req := httptest.NewRequest(http.MethodPost, path, nil)
	for _, opt := range opts {
		opt(req)
	}

	rec := httptest.NewRecorder()
	s.Echo.ServeHTTP(rec, req)

	events, _ := ParseSSEResponseFromBytes(rec.Body.Bytes())

	return &SSEResponse{
		StatusCode:  rec.Code,
		ContentType: rec.Header().Get("Content-Type"),
		Events:      events,
		RawBody:     rec.Body.String(),
	}
}

// HasEvent checks if the SSE response contains an event with the given type
func (r *SSEResponse) HasEvent(eventType string) bool {
	for _, e := range r.Events {
		if e.Event == eventType {
			return true
		}
	}
	return false
}

// GetEventsByType returns all events with the given type
func (r *SSEResponse) GetEventsByType(eventType string) []SSEEvent {
	var result []SSEEvent
	for _, e := range r.Events {
		if e.Event == eventType {
			result = append(result, e)
		}
	}
	return result
}

// GetLastEvent returns the last event in the response
func (r *SSEResponse) GetLastEvent() *SSEEvent {
	if len(r.Events) == 0 {
		return nil
	}
	return &r.Events[len(r.Events)-1]
}

// FindEventWithData finds the first event where the data contains the given substring
func (r *SSEResponse) FindEventWithData(substr string) *SSEEvent {
	for i := range r.Events {
		if strings.Contains(r.Events[i].Data, substr) {
			return &r.Events[i]
		}
	}
	return nil
}

// ParseAllDataAsJSON parses all event Data fields as JSON into a slice of the given type
func ParseAllDataAsJSON[T any](events []SSEEvent) ([]T, error) {
	var results []T
	for _, e := range events {
		if e.Data == "" {
			continue
		}
		var v T
		if err := json.Unmarshal([]byte(e.Data), &v); err != nil {
			return nil, err
		}
		results = append(results, v)
	}
	return results, nil
}

// IsSSEContentType checks if the content type is valid for SSE
func IsSSEContentType(contentType string) bool {
	return strings.Contains(contentType, "text/event-stream")
}
