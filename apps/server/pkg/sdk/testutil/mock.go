// Package testutil provides testing utilities for the Emergent SDK.
package testutil

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// MockServer provides a mock HTTP server for testing SDK clients.
type MockServer struct {
	*httptest.Server
	t        *testing.T
	handlers map[string]http.HandlerFunc
}

// NewMockServer creates a new mock server for testing.
func NewMockServer(t *testing.T) *MockServer {
	ms := &MockServer{
		t:        t,
		handlers: make(map[string]http.HandlerFunc),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", ms.handleRequest)

	ms.Server = httptest.NewServer(mux)
	return ms
}

// On registers a handler for a specific method and path.
func (ms *MockServer) On(method, path string, handler http.HandlerFunc) {
	key := method + " " + path
	ms.handlers[key] = handler
}

// OnJSON registers a handler that returns JSON for a specific method and path.
func (ms *MockServer) OnJSON(method, path string, statusCode int, response interface{}) {
	ms.On(method, path, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		if response != nil {
			if err := json.NewEncoder(w).Encode(response); err != nil {
				ms.t.Fatalf("failed to encode response: %v", err)
			}
		}
	})
}

// handleRequest routes requests to registered handlers.
func (ms *MockServer) handleRequest(w http.ResponseWriter, r *http.Request) {
	key := r.Method + " " + r.URL.Path
	handler, ok := ms.handlers[key]
	if !ok {
		ms.t.Logf("no handler registered for %s", key)
		http.NotFound(w, r)
		return
	}
	handler(w, r)
}

// Close closes the mock server.
func (ms *MockServer) Close() {
	ms.Server.Close()
}

// AssertHeader asserts that a request header has the expected value.
func AssertHeader(t *testing.T, r *http.Request, key, expected string) {
	t.Helper()
	actual := r.Header.Get(key)
	if actual != expected {
		t.Errorf("expected header %s=%q, got %q", key, expected, actual)
	}
}

// AssertMethod asserts that the request method matches expected.
func AssertMethod(t *testing.T, r *http.Request, expected string) {
	t.Helper()
	if r.Method != expected {
		t.Errorf("expected method %s, got %s", expected, r.Method)
	}
}

// AssertJSONBody decodes the request body and compares it to expected.
func AssertJSONBody(t *testing.T, r *http.Request, expected interface{}) {
	t.Helper()
	var actual interface{}
	if err := json.NewDecoder(r.Body).Decode(&actual); err != nil {
		t.Fatalf("failed to decode request body: %v", err)
	}

	expectedJSON, _ := json.Marshal(expected)
	actualJSON, _ := json.Marshal(actual)

	if string(expectedJSON) != string(actualJSON) {
		t.Errorf("expected body %s, got %s", string(expectedJSON), string(actualJSON))
	}
}

// JSONResponse writes a JSON response to the response writer.
func JSONResponse(t *testing.T, w http.ResponseWriter, data interface{}) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(data); err != nil {
		t.Fatalf("failed to encode JSON response: %v", err)
	}
}
