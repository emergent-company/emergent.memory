package users_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/testutil"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/users"
)

func TestUsersGetProfile(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	fixtureProfile := testutil.FixtureUserProfile()
	mock.OnJSON("GET", "/api/v2/user/profile", http.StatusOK, fixtureProfile)

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

	result, err := client.Users.GetProfile(context.Background())
	if err != nil {
		t.Fatalf("GetProfile() error = %v", err)
	}

	if result.ID != fixtureProfile.ID {
		t.Errorf("expected user ID %s, got %s", fixtureProfile.ID, result.ID)
	}
	if result.Email != fixtureProfile.Email {
		t.Errorf("expected email %s, got %s", fixtureProfile.Email, result.Email)
	}
}

func TestUsersUpdateProfile(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	fixtureProfile := testutil.FixtureUserProfile()
	updatedFirstName := "Updated"
	fixtureProfile.FirstName = &updatedFirstName

	mock.OnJSON("PUT", "/api/v2/user/profile", http.StatusOK, fixtureProfile)

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	req := &users.UpdateProfileRequest{
		FirstName: &updatedFirstName,
	}

	result, err := client.Users.UpdateProfile(context.Background(), req)
	if err != nil {
		t.Fatalf("UpdateProfile() error = %v", err)
	}

	if result.FirstName == nil || *result.FirstName != updatedFirstName {
		t.Errorf("expected first name %s, got %v", updatedFirstName, result.FirstName)
	}
}
