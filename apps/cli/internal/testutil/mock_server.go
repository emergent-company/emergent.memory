package testutil

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"time"
)

// NewMockServer creates a test HTTP server with specified handlers.
// Handlers are registered for exact path matches.
// Any unmatched paths return 404.
func NewMockServer(handlers map[string]http.HandlerFunc) *httptest.Server {
	mux := http.NewServeMux()

	for path, handler := range handlers {
		mux.HandleFunc(path, handler)
	}

	return httptest.NewServer(mux)
}

// WithJSONResponse creates an HTTP handler that returns a JSON response.
func WithJSONResponse(statusCode int, body interface{}) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)

		if body != nil {
			_ = json.NewEncoder(w).Encode(body)
		}
	}
}

// WithDelayedResponse wraps a handler to add artificial latency.
// Useful for testing timeout behavior.
func WithDelayedResponse(delay time.Duration, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(delay)
		handler(w, r)
	}
}
