package apitokens_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/apitokens"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/testutil"
)

func TestAPITokensCreate(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	fixtureResponse := testutil.FixtureCreateTokenResponse()
	mock.OnJSON("POST", "/api/projects/proj_test123/tokens", http.StatusCreated, fixtureResponse)

	client, err := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth: sdk.AuthConfig{
			Mode:   "apikey",
			APIKey: "test_key",
		},
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	req := &apitokens.CreateTokenRequest{
		Name:   "Test Token",
		Scopes: []string{"documents:read", "documents:write"},
	}

	result, err := client.APITokens.Create(context.Background(), "proj_test123", req)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if result.Name != fixtureResponse.Name {
		t.Errorf("expected token name %s, got %s", fixtureResponse.Name, result.Name)
	}
	if result.Token == "" {
		t.Error("expected full token value, got empty string")
	}
}

func TestAPITokensList(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	fixtureToken := testutil.FixtureAPIToken()
	fixtureResponse := &apitokens.ListResponse{
		Tokens: []apitokens.APIToken{*fixtureToken},
	}
	mock.OnJSON("GET", "/api/projects/proj_test123/tokens", http.StatusOK, fixtureResponse)

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	result, err := client.APITokens.List(context.Background(), "proj_test123")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(result.Tokens) != 1 {
		t.Errorf("expected 1 token, got %d", len(result.Tokens))
	}

	if result.Tokens[0].ID != fixtureToken.ID {
		t.Errorf("expected token ID %s, got %s", fixtureToken.ID, result.Tokens[0].ID)
	}
}

func TestAPITokensGet(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	fixtureToken := testutil.FixtureAPIToken()
	mock.OnJSON("GET", "/api/projects/proj_test123/tokens/token_test123", http.StatusOK, fixtureToken)

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	result, err := client.APITokens.Get(context.Background(), "proj_test123", "token_test123")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if result.ID != fixtureToken.ID {
		t.Errorf("expected token ID %s, got %s", fixtureToken.ID, result.ID)
	}
	if result.Name != fixtureToken.Name {
		t.Errorf("expected token name %s, got %s", fixtureToken.Name, result.Name)
	}
	if result.Token != fixtureToken.Token {
		t.Errorf("expected token value %s, got %s", fixtureToken.Token, result.Token)
	}
}

func TestAPITokensRevoke(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.OnJSON("DELETE", "/api/projects/proj_test123/tokens/token_test123", http.StatusOK, map[string]string{"status": "revoked"})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	err := client.APITokens.Revoke(context.Background(), "proj_test123", "token_test123")
	if err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}
}
