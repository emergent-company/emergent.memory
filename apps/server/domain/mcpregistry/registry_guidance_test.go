package mcpregistry

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// stubTransport returns a fixed status with an empty body.
type stubTransport struct{ status int }

func (s stubTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: s.status,
		Body:       io.NopCloser(strings.NewReader("")),
	}, nil
}

// TestAuthStatusTransportClassifiesRejection asserts a 401/403 surfaces as a
// typed ErrMCPAuthRejected (so the failure is attributable to the credential),
// while other statuses pass through untouched.
func TestAuthStatusTransportClassifiesRejection(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "https://api.githubcopilot.com/mcp/", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}

	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			_, err := authStatusTransport{base: stubTransport{status: status}}.RoundTrip(req)
			var authErr *ErrMCPAuthRejected
			if !errors.As(err, &authErr) {
				t.Fatalf("err = %v, want *ErrMCPAuthRejected", err)
			}
			if authErr.Status != status {
				t.Fatalf("status = %d, want %d", authErr.Status, status)
			}
			if !strings.Contains(authErr.Error(), "Bearer") {
				t.Fatalf("message %q should hint at the Bearer form", authErr.Error())
			}
		})
	}

	resp, err := authStatusTransport{base: stubTransport{status: http.StatusOK}}.RoundTrip(req)
	if err != nil {
		t.Fatalf("200 should pass through, got %v", err)
	}
	_ = resp.Body.Close()
}
