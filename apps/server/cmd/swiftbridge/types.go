// types.go defines the JSON request/response types for all bridge functions.
// No CGO dependency — safe to compile on any platform.
package main

// ---------------------------------------------------------------------------
// CreateClient
// ---------------------------------------------------------------------------

// CreateClientRequest is the JSON payload expected by CreateClient.
type CreateClientRequest struct {
	ServerURL string `json:"server_url"`
	AuthMode  string `json:"auth_mode"` // "apikey" | "apitoken"
	APIKey    string `json:"api_key"`
	OrgID     string `json:"org_id,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
}

// CreateClientResponse is the JSON payload returned by CreateClient on success.
type CreateClientResponse struct {
	Handle uint32 `json:"handle"`
}

// ---------------------------------------------------------------------------
// Ping
// ---------------------------------------------------------------------------

// PingRequest is the JSON payload for the Ping call.
type PingRequest struct {
	Message string `json:"message"`
}

// PingResponse is the JSON payload returned on success.
type PingResponse struct {
	Echo      string `json:"echo"`
	Timestamp string `json:"timestamp"`
}
