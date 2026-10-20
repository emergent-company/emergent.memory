package sources

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/internal/config"
)

func describeEntry(t *testing.T, items []*OAuthCredentialDescription, providerType string) *OAuthCredentialDescription {
	t.Helper()
	for _, item := range items {
		if item.ProviderType == providerType {
			return item
		}
	}
	t.Fatalf("no credential description for provider %q", providerType)
	return nil
}

func TestDescribeOAuthCredentials_ProjectOverridesOrgAndDeployment(t *testing.T) {
	projectID := uuid.New()
	orgID := uuid.New()
	orgStore := &fakeOrgOAuthStore{orgID: orgID, clients: map[string]*OAuthClientConfig{}}
	projStore := &fakeProjectOAuthStore{orgID: orgID, clients: map[string]*OAuthClientConfig{}}
	svc := newOrgAndProjectClientService(orgStore, projStore, config.SourceOAuthConfig{
		GoogleClientID: "deploy-id", GoogleClientSecret: "deploy-secret",
	})
	orgStore.clients[orgClientKey(orgID, ProviderTypeGoogleDrive)] = mustEncryptOrgClient(t, svc, orgID, ProviderTypeGoogleDrive, Credentials{
		oauthClientIDKey: "org-id", oauthClientSecretKey: "org-secret",
	})
	projStore.clients[projectClientKey(projectID, ProviderTypeGoogleDrive)] = mustEncryptProjectClient(t, svc, projectID, ProviderTypeGoogleDrive, Credentials{
		oauthClientIDKey: "project-id", oauthClientSecretKey: "project-secret",
	})

	items, err := svc.DescribeOAuthCredentials(context.Background(), projectID)
	if err != nil {
		t.Fatalf("DescribeOAuthCredentials() error = %v", err)
	}
	drive := describeEntry(t, items, ProviderTypeGoogleDrive)
	if drive.ResolvedScope != CredentialScopeProject {
		t.Errorf("resolved scope = %q, want %q", drive.ResolvedScope, CredentialScopeProject)
	}
	if !drive.Project.Configured || drive.Project.ClientID != "project-id" {
		t.Errorf("project source = %+v, want configured with client id project-id", drive.Project)
	}
	if !drive.Organization.Configured || drive.Organization.ClientID != "org-id" {
		t.Errorf("organization source = %+v, want configured with client id org-id", drive.Organization)
	}
	if !drive.Deployment.Configured || drive.Deployment.ClientID != "deploy-id" {
		t.Errorf("deployment source = %+v, want configured with client id deploy-id", drive.Deployment)
	}
}

func TestDescribeOAuthCredentials_FallsBackToOrganization(t *testing.T) {
	projectID := uuid.New()
	orgID := uuid.New()
	orgStore := &fakeOrgOAuthStore{orgID: orgID, clients: map[string]*OAuthClientConfig{}}
	projStore := &fakeProjectOAuthStore{orgID: orgID, clients: map[string]*OAuthClientConfig{}}
	svc := newOrgAndProjectClientService(orgStore, projStore, config.SourceOAuthConfig{})
	orgStore.clients[orgClientKey(orgID, ProviderTypeGoogleDrive)] = mustEncryptOrgClient(t, svc, orgID, ProviderTypeGoogleDrive, Credentials{
		oauthClientIDKey: "org-id", oauthClientSecretKey: "org-secret",
	})

	items, err := svc.DescribeOAuthCredentials(context.Background(), projectID)
	if err != nil {
		t.Fatalf("DescribeOAuthCredentials() error = %v", err)
	}
	drive := describeEntry(t, items, ProviderTypeGoogleDrive)
	if drive.ResolvedScope != CredentialScopeOrganization {
		t.Errorf("resolved scope = %q, want %q", drive.ResolvedScope, CredentialScopeOrganization)
	}
	if drive.Project.Configured {
		t.Error("project source marked configured, want unconfigured")
	}
	if !drive.Organization.Configured {
		t.Error("organization source not configured, want configured")
	}
}

func TestDescribeOAuthCredentials_FallsBackToDeployment(t *testing.T) {
	projectID := uuid.New()
	orgID := uuid.New()
	orgStore := &fakeOrgOAuthStore{orgID: orgID, clients: map[string]*OAuthClientConfig{}}
	projStore := &fakeProjectOAuthStore{orgID: orgID, clients: map[string]*OAuthClientConfig{}}
	svc := newOrgAndProjectClientService(orgStore, projStore, config.SourceOAuthConfig{
		GoogleClientID: "deploy-id", GoogleClientSecret: "deploy-secret",
	})

	items, err := svc.DescribeOAuthCredentials(context.Background(), projectID)
	if err != nil {
		t.Fatalf("DescribeOAuthCredentials() error = %v", err)
	}
	drive := describeEntry(t, items, ProviderTypeGoogleDrive)
	if drive.ResolvedScope != CredentialScopeDeployment {
		t.Errorf("resolved scope = %q, want %q", drive.ResolvedScope, CredentialScopeDeployment)
	}
	if !drive.Deployment.Configured || drive.Deployment.ClientID != "deploy-id" {
		t.Errorf("deployment source = %+v, want configured with client id deploy-id", drive.Deployment)
	}
}

func TestDescribeOAuthCredentials_NoClientAnywhere(t *testing.T) {
	projectID := uuid.New()
	orgStore := &fakeOrgOAuthStore{orgID: uuid.New(), clients: map[string]*OAuthClientConfig{}}
	projStore := &fakeProjectOAuthStore{orgID: orgStore.orgID, clients: map[string]*OAuthClientConfig{}}
	svc := newOrgAndProjectClientService(orgStore, projStore, config.SourceOAuthConfig{})

	items, err := svc.DescribeOAuthCredentials(context.Background(), projectID)
	if err != nil {
		t.Fatalf("DescribeOAuthCredentials() error = %v", err)
	}
	for _, item := range items {
		if item.ResolvedScope != CredentialScopeNone {
			t.Errorf("%s resolved scope = %q, want %q", item.ProviderType, item.ResolvedScope, CredentialScopeNone)
		}
	}
}

func TestDescribeOAuthCredentials_OneEntryPerOAuthProvider(t *testing.T) {
	projectID := uuid.New()
	orgStore := &fakeOrgOAuthStore{orgID: uuid.New(), clients: map[string]*OAuthClientConfig{}}
	projStore := &fakeProjectOAuthStore{orgID: orgStore.orgID, clients: map[string]*OAuthClientConfig{}}
	svc := newOrgAndProjectClientService(orgStore, projStore, config.SourceOAuthConfig{})

	items, err := svc.DescribeOAuthCredentials(context.Background(), projectID)
	if err != nil {
		t.Fatalf("DescribeOAuthCredentials() error = %v", err)
	}
	seen := map[string]bool{}
	for _, item := range items {
		seen[item.ProviderType] = true
	}
	for _, providerType := range []string{ProviderTypeGoogleDrive, ProviderTypeGitHub} {
		if !seen[providerType] {
			t.Errorf("missing credential description for OAuth provider %q", providerType)
		}
	}
}

func TestDescribeOAuthCredentials_NeverSerializesSecrets(t *testing.T) {
	projectID := uuid.New()
	orgID := uuid.New()
	orgStore := &fakeOrgOAuthStore{orgID: orgID, clients: map[string]*OAuthClientConfig{}}
	projStore := &fakeProjectOAuthStore{orgID: orgID, clients: map[string]*OAuthClientConfig{}}
	svc := newOrgAndProjectClientService(orgStore, projStore, config.SourceOAuthConfig{})
	projStore.clients[projectClientKey(projectID, ProviderTypeGoogleDrive)] = mustEncryptProjectClient(t, svc, projectID, ProviderTypeGoogleDrive, Credentials{
		oauthClientIDKey: "project-id", oauthClientSecretKey: "top-secret-project",
	})

	items, err := svc.DescribeOAuthCredentials(context.Background(), projectID)
	if err != nil {
		t.Fatalf("DescribeOAuthCredentials() error = %v", err)
	}
	raw, err := json.Marshal(items)
	if err != nil {
		t.Fatalf("marshal descriptions: %v", err)
	}
	body := string(raw)
	if strings.Contains(body, "top-secret-project") {
		t.Fatalf("serialized descriptions leaked the client secret: %s", body)
	}
	if strings.Contains(body, "clientSecret") {
		t.Fatalf("serialized descriptions carry a clientSecret field: %s", body)
	}
}
