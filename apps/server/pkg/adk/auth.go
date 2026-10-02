package adk

import (
	"fmt"
	"net/http"
)

// AuthStyle names how a provider expects credentials to be injected into an
// HTTP request. These mirror the domain/provider vocabulary as plain strings so
// pkg/adk can reference them without importing domain/provider (which would
// create an import cycle).
type AuthStyle string

const (
	// AuthBearer sends Authorization: Bearer <key>.
	AuthBearer AuthStyle = "bearer"
	// AuthAPIKeyHeader sends api-key: <key> (Azure OpenAI).
	AuthAPIKeyHeader AuthStyle = "api-key"
	// AuthXAPIKey sends x-api-key: <key> (Anthropic).
	AuthXAPIKey AuthStyle = "x-api-key"
	// AuthGoogleAPIKey sends x-goog-api-key: <key> (Gemini native).
	AuthGoogleAPIKey AuthStyle = "x-goog-api-key"
	// AuthNone sends no credential header (local deployments).
	AuthNone AuthStyle = "none"
	// AuthSigned delegates to a registered signer hook (WeKnora Cloud).
	AuthSigned AuthStyle = "signed"
)

// ApplyAuth sets the credential header on req for the given auth style.
//
// - bearer        -> Authorization: Bearer <apiKey>
// - api-key       -> api-key: <apiKey>
// - x-api-key     -> x-api-key: <apiKey>
// - x-goog-api-key-> x-goog-api-key: <apiKey>
// - none          -> no-op
// - signed        -> returns an error (signer hook is not supported in pkg/adk)
//
// An empty style defaults to bearer when apiKey is non-empty, otherwise no-op.
func ApplyAuth(req *http.Request, style AuthStyle, apiKey string) error {
	if style == "" {
		style = AuthBearer
	}
	switch style {
	case AuthBearer:
		if apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}
		return nil
	case AuthAPIKeyHeader:
		if apiKey != "" {
			req.Header.Set("api-key", apiKey)
		}
		return nil
	case AuthXAPIKey:
		if apiKey != "" {
			req.Header.Set("x-api-key", apiKey)
		}
		return nil
	case AuthGoogleAPIKey:
		if apiKey != "" {
			req.Header.Set("x-goog-api-key", apiKey)
		}
		return nil
	case AuthNone:
		return nil
	case AuthSigned:
		return fmt.Errorf("signed auth is not supported")
	default:
		return fmt.Errorf("unknown auth style %q", style)
	}
}
