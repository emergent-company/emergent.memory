package sources

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// listingProvider is a stub that records the credentials/config it was handed
// and returns canned resources and refs.
type listingProvider struct {
	stubProvider

	extractErr error
	listErr    error
	refsErr    error
	resources  []Resource
	refs       []Resource
	truncated  bool

	gotCreds Credentials
	gotCfg   map[string]any
	listed   bool
	refsRead bool
}

func (p *listingProvider) ExtractCredentials(map[string]any) (Credentials, error) {
	if p.extractErr != nil {
		return nil, p.extractErr
	}
	return Credentials{"access_token": "at"}, nil
}

func (p *listingProvider) ListResources(_ context.Context, creds Credentials, cfg map[string]any, _ string) ([]Resource, bool, error) {
	p.listed = true
	p.gotCreds = creds
	p.gotCfg = cfg
	if p.listErr != nil {
		return nil, false, p.listErr
	}
	return p.resources, p.truncated, nil
}

func (p *listingProvider) ListRefs(_ context.Context, creds Credentials, cfg map[string]any) ([]Resource, bool, error) {
	p.refsRead = true
	p.gotCreds = creds
	p.gotCfg = cfg
	if p.refsErr != nil {
		return nil, false, p.refsErr
	}
	return p.refs, p.truncated, nil
}

// githubLister returns a listing provider registered as the github type.
func githubLister() *listingProvider {
	return &listingProvider{stubProvider: stubProvider{catalog: Provider{Type: "github"}}}
}

func newListingService(provider SourceProvider) *Service {
	return NewService(nil, &config.Config{}, discardLog(), NewRegistry(provider))
}

func TestService_ListProviderResources_UsesUnsavedCredentials(t *testing.T) {
	p := githubLister()
	p.resources = []Resource{{ID: "acme/docs", Name: "acme/docs", Type: "repo"}}
	svc := newListingService(p)

	resources, _, err := svc.ListProviderResources(context.Background(), &ProviderListRequest{
		ProviderType: "github",
		Config:       map[string]any{"access_token": "at"},
	})
	if err != nil {
		t.Fatalf("ListProviderResources() error = %v", err)
	}
	if !p.listed {
		t.Fatal("ListProviderResources() did not call the provider")
	}
	if p.gotCreds["access_token"] != "at" {
		t.Errorf("credentials = %v, want the unsaved token", p.gotCreds)
	}
	if len(resources) != 1 || resources[0].ID != "acme/docs" {
		t.Fatalf("ListProviderResources() = %+v, want the provider's resources", resources)
	}
}

func TestService_ListProviderResources_EmptyIsNotNil(t *testing.T) {
	svc := newListingService(githubLister())
	resources, _, err := svc.ListProviderResources(context.Background(), &ProviderListRequest{ProviderType: "github"})
	if err != nil {
		t.Fatalf("ListProviderResources() error = %v", err)
	}
	if resources == nil {
		t.Fatal("ListProviderResources() = nil, want an empty slice (JSON [])")
	}
}

func TestService_ListProviderResources_RejectsUnknownProvider(t *testing.T) {
	svc := newListingService(githubLister())
	if _, _, err := svc.ListProviderResources(context.Background(), &ProviderListRequest{ProviderType: "imap"}); err == nil {
		t.Fatal("ListProviderResources(imap) = nil error, want validation error")
	}
}

func TestService_ListProviderResources_SurfacesCredentialError(t *testing.T) {
	p := githubLister()
	p.extractErr = errors.New("config.access_token is required")
	svc := newListingService(p)
	_, _, err := svc.ListProviderResources(context.Background(), &ProviderListRequest{ProviderType: "github"})
	if err == nil || !strings.Contains(err.Error(), "access_token") {
		t.Fatalf("ListProviderResources() error = %v, want the credential error", err)
	}
	if p.listed {
		t.Fatal("ListProviderResources() called the provider despite invalid credentials")
	}
}

func TestService_ListProviderResources_WrapsProviderError(t *testing.T) {
	p := githubLister()
	p.listErr = errors.New("rate limited")
	svc := newListingService(p)
	_, _, err := svc.ListProviderResources(context.Background(), &ProviderListRequest{ProviderType: "github"})
	if err == nil || !strings.Contains(err.Error(), "list resources failed") {
		t.Fatalf("ListProviderResources() error = %v, want a wrapped error", err)
	}
}

func TestService_ListProviderRefs_Passthrough(t *testing.T) {
	p := githubLister()
	p.refs = []Resource{{ID: "main", Name: "main", Type: "branch"}}
	p.truncated = true
	svc := newListingService(p)

	refs, truncated, err := svc.ListProviderRefs(context.Background(), &ProviderListRequest{
		ProviderType: "github",
		Config:       map[string]any{"access_token": "at", "repo": "acme/docs"},
	})
	if err != nil {
		t.Fatalf("ListProviderRefs() error = %v", err)
	}
	if !p.refsRead {
		t.Fatal("ListProviderRefs() did not call the provider")
	}
	if !truncated {
		t.Error("ListProviderRefs() truncated = false, want true")
	}
	if len(refs) != 1 || refs[0].ID != "main" || refs[0].Type != "branch" {
		t.Fatalf("ListProviderRefs() = %+v, want the provider's refs", refs)
	}
}

func TestService_ListProviderRefs_RejectsProviderWithoutRefSupport(t *testing.T) {
	svc := newListingService(&stubProvider{catalog: Provider{Type: "google_drive"}})
	_, _, err := svc.ListProviderRefs(context.Background(), &ProviderListRequest{ProviderType: "google_drive"})
	if err == nil || !strings.Contains(err.Error(), "does not support ref listing") {
		t.Fatalf("ListProviderRefs(drive) error = %v, want unsupported-provider error", err)
	}
}

func TestService_ListProviderRefs_WrapsProviderError(t *testing.T) {
	p := githubLister()
	p.refsErr = errors.New("boom")
	svc := newListingService(p)
	_, _, err := svc.ListProviderRefs(context.Background(), &ProviderListRequest{ProviderType: "github"})
	if err == nil || !strings.Contains(err.Error(), "list refs failed") {
		t.Fatalf("ListProviderRefs() error = %v, want a wrapped error", err)
	}
}

func TestService_ListProviderRefs_PreservesProviderValidationMessage(t *testing.T) {
	p := githubLister()
	p.refsErr = apperror.NewValidation("config.repo is required to list branches")
	svc := newListingService(p)

	_, _, err := svc.ListProviderRefs(context.Background(), &ProviderListRequest{ProviderType: "github"})
	if err == nil {
		t.Fatal("ListProviderRefs() = nil error, want the provider's validation error")
	}
	if !strings.Contains(err.Error(), "config.repo is required to list branches") {
		t.Fatalf("error = %q, want the provider's message", err)
	}
	if strings.Contains(err.Error(), "list refs failed") {
		t.Errorf("error = %q, want no double prefix on a provider validation error", err)
	}
}

func TestService_ListProviderResources_ReportsTruncation(t *testing.T) {
	p := githubLister()
	p.resources = []Resource{{ID: "folder-1", Name: "One", Type: "folder"}}
	p.truncated = true
	svc := newListingService(p)

	_, truncated, err := svc.ListProviderResources(context.Background(), &ProviderListRequest{ProviderType: "github"})
	if err != nil {
		t.Fatalf("ListProviderResources() error = %v", err)
	}
	if !truncated {
		t.Error("ListProviderResources() truncated = false, want the provider's truncation flag")
	}
}
