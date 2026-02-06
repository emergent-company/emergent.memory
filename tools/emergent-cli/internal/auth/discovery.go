package auth

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type OIDCConfig struct {
	Issuer                      string `json:"issuer"`
	DeviceAuthorizationEndpoint string `json:"device_authorization_endpoint"`
	TokenEndpoint               string `json:"token_endpoint"`
	UserinfoEndpoint            string `json:"userinfo_endpoint"`
	AuthorizationEndpoint       string `json:"authorization_endpoint,omitempty"`
	JwksURI                     string `json:"jwks_uri,omitempty"`
}

func DiscoverOIDC(issuerURL string) (*OIDCConfig, error) {
	discoveryURL := strings.TrimSuffix(issuerURL, "/") + "/.well-known/openid-configuration"

	resp, err := http.Get(discoveryURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch discovery document: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("discovery endpoint returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var config OIDCConfig
	if err := json.Unmarshal(body, &config); err != nil {
		return nil, fmt.Errorf("failed to parse discovery document: %w", err)
	}

	if err := validateOIDCConfig(&config); err != nil {
		return nil, err
	}

	return &config, nil
}

func validateOIDCConfig(config *OIDCConfig) error {
	if config.Issuer == "" {
		return fmt.Errorf("missing required field: issuer")
	}
	if config.DeviceAuthorizationEndpoint == "" {
		return fmt.Errorf("missing required field: device_authorization_endpoint")
	}
	if config.TokenEndpoint == "" {
		return fmt.Errorf("missing required field: token_endpoint")
	}
	if config.UserinfoEndpoint == "" {
		return fmt.Errorf("missing required field: userinfo_endpoint")
	}
	return nil
}
