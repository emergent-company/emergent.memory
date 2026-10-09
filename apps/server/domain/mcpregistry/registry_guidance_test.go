package mcpregistry

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestRegistryServerToDTOExposesRemoteCredentials asserts the browse DTO carries
// each remote's own declared credentials, so the catalog card can preview what an
// install will require before the user installs.
func TestRegistryServerToDTOExposesRemoteCredentials(t *testing.T) {
	dto := registryServerToDTO(RegistryServer{
		Name: "io.github.github/github-mcp-server",
		Repository: &RegistryRepo{
			URL:    "https://github.com/github/github-mcp-server",
			Source: "github",
		},
		Remotes: []RegistryRemote{
			{
				Type: "streamable-http",
				URL:  "https://api.githubcopilot.com/mcp/",
				Headers: []RegistryHeader{
					{
						Name:        "Authorization",
						IsSecret:    true,
						Description: "Authorization header with authentication token (PAT or App token)",
					},
				},
			},
		},
	})

	if len(dto.Remotes) != 1 {
		t.Fatalf("remotes = %+v, want 1", dto.Remotes)
	}
	remote := dto.Remotes[0]
	if len(remote.Headers) != 1 {
		t.Fatalf("remote headers = %+v, want the declared Authorization header", remote.Headers)
	}
	if remote.Headers[0].Name != "Authorization" || !remote.Headers[0].IsSecret {
		t.Fatalf("header = %+v, want secret Authorization", remote.Headers[0])
	}
	if remote.Headers[0].Description != "Authorization header with authentication token (PAT or App token)" {
		t.Fatalf("header description = %q", remote.Headers[0].Description)
	}
	if dto.Repository == nil || dto.Repository.URL != "https://github.com/github/github-mcp-server" {
		t.Fatalf("repository = %+v", dto.Repository)
	}
}

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
