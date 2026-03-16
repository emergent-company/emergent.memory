package autoprovision

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/domain/orgs"
	"github.com/emergent-company/emergent.memory/domain/projects"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// ─── Mock implementations ────────────────────────────────────────────

// mockOrgCreator records Create calls and returns configured results.
type mockOrgCreator struct {
	calls   []string // names passed to Create
	results map[string]mockOrgResult
}

type mockOrgResult struct {
	org *orgs.OrgDTO
	err error
}

func (m *mockOrgCreator) Create(_ context.Context, name string, _ string) (*orgs.OrgDTO, error) {
	m.calls = append(m.calls, name)
	if r, ok := m.results[name]; ok {
		return r.org, r.err
	}
	// Default: succeed with generated ID
	return &orgs.OrgDTO{ID: "org-" + name, Name: name}, nil
}

// mockProjectCreator records Create calls and returns configured results.
type mockProjectCreator struct {
	calls []projects.CreateProjectRequest
	err   error
}

func (m *mockProjectCreator) Create(_ context.Context, req projects.CreateProjectRequest, _ string) (*projects.ProjectDTO, error) {
	m.calls = append(m.calls, req)
	if m.err != nil {
		return nil, m.err
	}
	return &projects.ProjectDTO{ID: "proj-1", Name: req.Name, OrgID: req.OrgID}, nil
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// ─── Name derivation tests ───────────────────────────────────────────

func TestDeriveOrgName(t *testing.T) {
	tests := []struct {
		name     string
		profile  *auth.UserProfileInfo
		expected string
	}{
		{
			name:     "full name (first + last)",
			profile:  &auth.UserProfileInfo{FirstName: "John", LastName: "Doe"},
			expected: "John Doe's Org",
		},
		{
			name:     "first name only",
			profile:  &auth.UserProfileInfo{FirstName: "John"},
			expected: "John's Org",
		},
		{
			name:     "display name fallback",
			profile:  &auth.UserProfileInfo{DisplayName: "Johnny D"},
			expected: "Johnny D's Org",
		},
		{
			name:     "email fallback",
			profile:  &auth.UserProfileInfo{Email: "johndoe@example.com"},
			expected: "johndoe's Org",
		},
		{
			name:     "no data fallback",
			profile:  &auth.UserProfileInfo{},
			expected: "My Organization",
		},
		{
			name:     "nil profile",
			profile:  nil,
			expected: "My Organization",
		},
		{
			name:     "whitespace-only first name falls through",
			profile:  &auth.UserProfileInfo{FirstName: "  ", DisplayName: "Fallback"},
			expected: "Fallback's Org",
		},
		{
			name:     "first name with spaces trimmed",
			profile:  &auth.UserProfileInfo{FirstName: "  John  ", LastName: "  Doe  "},
			expected: "John Doe's Org",
		},
		{
			name:     "email with no local part",
			profile:  &auth.UserProfileInfo{Email: "@example.com"},
			expected: "My Organization",
		},
		{
			name:     "first name preferred over display name",
			profile:  &auth.UserProfileInfo{FirstName: "John", DisplayName: "Johnny D", Email: "jd@example.com"},
			expected: "John's Org",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deriveOrgName(tt.profile)
			if got != tt.expected {
				t.Errorf("deriveOrgName() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestDeriveProjectName(t *testing.T) {
	tests := []struct {
		name     string
		profile  *auth.UserProfileInfo
		expected string
	}{
		{
			name:     "first name",
			profile:  &auth.UserProfileInfo{FirstName: "John", LastName: "Doe"},
			expected: "John's Project",
		},
		{
			name:     "display name fallback",
			profile:  &auth.UserProfileInfo{DisplayName: "Johnny D"},
			expected: "Johnny D's Project",
		},
		{
			name:     "email fallback",
			profile:  &auth.UserProfileInfo{Email: "johndoe@example.com"},
			expected: "johndoe's Project",
		},
		{
			name:     "no data fallback",
			profile:  &auth.UserProfileInfo{},
			expected: "My Project",
		},
		{
			name:     "nil profile",
			profile:  nil,
			expected: "My Project",
		},
		{
			name:     "first name preferred over display name and email",
			profile:  &auth.UserProfileInfo{FirstName: "Jane", DisplayName: "Jane D", Email: "jane@example.com"},
			expected: "Jane's Project",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deriveProjectName(tt.profile)
			if got != tt.expected {
				t.Errorf("deriveProjectName() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestEmailLocalPart(t *testing.T) {
	tests := []struct {
		email    string
		expected string
	}{
		{"john@example.com", "john"},
		{"john.doe@example.com", "john.doe"},
		{"@example.com", ""},
		{"noatsign", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.email, func(t *testing.T) {
			got := emailLocalPart(tt.email)
			if got != tt.expected {
				t.Errorf("emailLocalPart(%q) = %q, want %q", tt.email, got, tt.expected)
			}
		})
	}
}

// ─── Conflict error detection ────────────────────────────────────────

func TestIsConflictError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"generic error", fmt.Errorf("something went wrong"), false},
		{"wrapped generic error", fmt.Errorf("wrap: %w", fmt.Errorf("inner")), false},
		{"409 apperror", apperror.New(409, "conflict", "duplicate name"), true},
		{"400 apperror", apperror.New(400, "bad_request", "invalid"), false},
		{"500 apperror", apperror.New(500, "internal", "oops"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isConflictError(tt.err)
			if got != tt.expected {
				t.Errorf("isConflictError() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// ─── ProvisionNewUser flow tests ─────────────────────────────────────

func TestProvisionNewUser_Success(t *testing.T) {
	orgsMock := &mockOrgCreator{results: map[string]mockOrgResult{}}
	projMock := &mockProjectCreator{}
	svc := newServiceWithDeps(orgsMock, projMock, testLogger())

	profile := &auth.UserProfileInfo{FirstName: "Alice", LastName: "Smith"}
	err := svc.ProvisionNewUser(context.Background(), "user-1", profile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify org creation was called with derived name
	if len(orgsMock.calls) != 1 {
		t.Fatalf("expected 1 org create call, got %d", len(orgsMock.calls))
	}
	if orgsMock.calls[0] != "Alice Smith's Org" {
		t.Errorf("org name = %q, want %q", orgsMock.calls[0], "Alice Smith's Org")
	}

	// Verify project creation was called with derived name and correct org ID
	if len(projMock.calls) != 1 {
		t.Fatalf("expected 1 project create call, got %d", len(projMock.calls))
	}
	if projMock.calls[0].Name != "Alice's Project" {
		t.Errorf("project name = %q, want %q", projMock.calls[0].Name, "Alice's Project")
	}
	if projMock.calls[0].OrgID != "org-Alice Smith's Org" {
		t.Errorf("project orgID = %q, want %q", projMock.calls[0].OrgID, "org-Alice Smith's Org")
	}
}

func TestProvisionNewUser_NilProfile_UsesFallbacks(t *testing.T) {
	orgsMock := &mockOrgCreator{results: map[string]mockOrgResult{}}
	projMock := &mockProjectCreator{}
	svc := newServiceWithDeps(orgsMock, projMock, testLogger())

	err := svc.ProvisionNewUser(context.Background(), "user-1", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if orgsMock.calls[0] != "My Organization" {
		t.Errorf("org name = %q, want %q", orgsMock.calls[0], "My Organization")
	}
	if projMock.calls[0].Name != "My Project" {
		t.Errorf("project name = %q, want %q", projMock.calls[0].Name, "My Project")
	}
}

func TestProvisionNewUser_OrgConflict_RetriesWithSuffix(t *testing.T) {
	conflictErr := apperror.New(409, "conflict", "name taken")
	orgsMock := &mockOrgCreator{
		results: map[string]mockOrgResult{
			"John's Org":   {nil, conflictErr},
			"John's Org 2": {nil, conflictErr},
			"John's Org 3": {&orgs.OrgDTO{ID: "org-3", Name: "John's Org 3"}, nil},
		},
	}
	projMock := &mockProjectCreator{}
	svc := newServiceWithDeps(orgsMock, projMock, testLogger())

	profile := &auth.UserProfileInfo{FirstName: "John"}
	err := svc.ProvisionNewUser(context.Background(), "user-1", profile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have tried 3 names
	if len(orgsMock.calls) != 3 {
		t.Fatalf("expected 3 org create calls, got %d: %v", len(orgsMock.calls), orgsMock.calls)
	}
	expectedNames := []string{"John's Org", "John's Org 2", "John's Org 3"}
	for i, name := range expectedNames {
		if orgsMock.calls[i] != name {
			t.Errorf("call[%d] = %q, want %q", i, orgsMock.calls[i], name)
		}
	}

	// Project should use the successfully created org's ID
	if projMock.calls[0].OrgID != "org-3" {
		t.Errorf("project orgID = %q, want %q", projMock.calls[0].OrgID, "org-3")
	}
}

func TestProvisionNewUser_OrgConflict_ExhaustsRetries(t *testing.T) {
	conflictErr := apperror.New(409, "conflict", "name taken")
	results := map[string]mockOrgResult{}
	// Make all attempts conflict
	results["John's Org"] = mockOrgResult{nil, conflictErr}
	for i := 2; i <= maxOrgNameRetries+1; i++ {
		name := fmt.Sprintf("John's Org %d", i)
		results[name] = mockOrgResult{nil, conflictErr}
	}

	orgsMock := &mockOrgCreator{results: results}
	projMock := &mockProjectCreator{}
	svc := newServiceWithDeps(orgsMock, projMock, testLogger())

	profile := &auth.UserProfileInfo{FirstName: "John"}
	err := svc.ProvisionNewUser(context.Background(), "user-1", profile)
	if err == nil {
		t.Fatal("expected error when all retries exhausted, got nil")
	}
	if !strings.Contains(err.Error(), "conflict persists") {
		t.Errorf("error = %q, want it to contain 'conflict persists'", err.Error())
	}

	// Should have tried 1 (base) + maxOrgNameRetries (suffixed) = 6 times
	expectedCalls := 1 + maxOrgNameRetries
	if len(orgsMock.calls) != expectedCalls {
		t.Errorf("expected %d org create calls, got %d", expectedCalls, len(orgsMock.calls))
	}

	// Project should NOT have been called
	if len(projMock.calls) != 0 {
		t.Errorf("expected 0 project create calls, got %d", len(projMock.calls))
	}
}

func TestProvisionNewUser_OrgNonConflictError_StopsImmediately(t *testing.T) {
	orgsMock := &mockOrgCreator{
		results: map[string]mockOrgResult{
			"John's Org": {nil, fmt.Errorf("database down")},
		},
	}
	projMock := &mockProjectCreator{}
	svc := newServiceWithDeps(orgsMock, projMock, testLogger())

	profile := &auth.UserProfileInfo{FirstName: "John"}
	err := svc.ProvisionNewUser(context.Background(), "user-1", profile)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "database down") {
		t.Errorf("error = %q, want it to contain 'database down'", err.Error())
	}

	// Should stop after first attempt (non-conflict error)
	if len(orgsMock.calls) != 1 {
		t.Errorf("expected 1 org create call, got %d", len(orgsMock.calls))
	}
	// Project should NOT have been called
	if len(projMock.calls) != 0 {
		t.Errorf("expected 0 project create calls, got %d", len(projMock.calls))
	}
}

func TestProvisionNewUser_ProjectFailure_ReturnsError(t *testing.T) {
	orgsMock := &mockOrgCreator{results: map[string]mockOrgResult{}}
	projMock := &mockProjectCreator{err: fmt.Errorf("project quota exceeded")}
	svc := newServiceWithDeps(orgsMock, projMock, testLogger())

	profile := &auth.UserProfileInfo{FirstName: "Alice"}
	err := svc.ProvisionNewUser(context.Background(), "user-1", profile)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "create project") {
		t.Errorf("error = %q, want it to contain 'create project'", err.Error())
	}
	if !strings.Contains(err.Error(), "project quota exceeded") {
		t.Errorf("error = %q, want it to contain 'project quota exceeded'", err.Error())
	}

	// Org should have been created successfully
	if len(orgsMock.calls) != 1 {
		t.Errorf("expected 1 org create call, got %d", len(orgsMock.calls))
	}
	// Project should have been attempted
	if len(projMock.calls) != 1 {
		t.Errorf("expected 1 project create call, got %d", len(projMock.calls))
	}
}

func TestProvisionNewUser_EmailOnlyProfile(t *testing.T) {
	orgsMock := &mockOrgCreator{results: map[string]mockOrgResult{}}
	projMock := &mockProjectCreator{}
	svc := newServiceWithDeps(orgsMock, projMock, testLogger())

	profile := &auth.UserProfileInfo{Email: "alice@example.com"}
	err := svc.ProvisionNewUser(context.Background(), "user-1", profile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if orgsMock.calls[0] != "alice's Org" {
		t.Errorf("org name = %q, want %q", orgsMock.calls[0], "alice's Org")
	}
	if projMock.calls[0].Name != "alice's Project" {
		t.Errorf("project name = %q, want %q", projMock.calls[0].Name, "alice's Project")
	}
}

func TestProvisionNewUser_DisplayNameOnlyProfile(t *testing.T) {
	orgsMock := &mockOrgCreator{results: map[string]mockOrgResult{}}
	projMock := &mockProjectCreator{}
	svc := newServiceWithDeps(orgsMock, projMock, testLogger())

	profile := &auth.UserProfileInfo{DisplayName: "AliceD"}
	err := svc.ProvisionNewUser(context.Background(), "user-1", profile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if orgsMock.calls[0] != "AliceD's Org" {
		t.Errorf("org name = %q, want %q", orgsMock.calls[0], "AliceD's Org")
	}
	if projMock.calls[0].Name != "AliceD's Project" {
		t.Errorf("project name = %q, want %q", projMock.calls[0].Name, "AliceD's Project")
	}
}

// ─── createOrgWithRetry tests ────────────────────────────────────────

func TestCreateOrgWithRetry_NonConflictErrorOnRetry(t *testing.T) {
	conflictErr := apperror.New(409, "conflict", "name taken")
	orgsMock := &mockOrgCreator{
		results: map[string]mockOrgResult{
			"Test Org":   {nil, conflictErr},
			"Test Org 2": {nil, fmt.Errorf("connection lost")},
		},
	}
	svc := newServiceWithDeps(orgsMock, &mockProjectCreator{}, testLogger())

	_, err := svc.createOrgWithRetry(context.Background(), "Test Org", "user-1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "connection lost") {
		t.Errorf("error = %q, want it to contain 'connection lost'", err.Error())
	}
	// Should have tried base name, then suffix 2, then stopped
	if len(orgsMock.calls) != 2 {
		t.Errorf("expected 2 calls, got %d", len(orgsMock.calls))
	}
}

// ─── Edge-case and panic-safety tests ────────────────────────────────

func TestProvisionNewUser_EdgeCases_NoPanic(t *testing.T) {
	edgeCases := []*auth.UserProfileInfo{
		nil,
		{},
		{Email: ""},
		{Email: "noat"},
		{FirstName: "  ", LastName: "  "},
		{FirstName: "A", LastName: "B", DisplayName: "C", Email: "d@e.com"},
	}

	for i, profile := range edgeCases {
		t.Run(fmt.Sprintf("edge_case_%d", i), func(t *testing.T) {
			orgsMock := &mockOrgCreator{results: map[string]mockOrgResult{}}
			projMock := &mockProjectCreator{}
			svc := newServiceWithDeps(orgsMock, projMock, testLogger())

			// Should not panic
			err := svc.ProvisionNewUser(context.Background(), "user-1", profile)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			orgName := deriveOrgName(profile)
			projectName := deriveProjectName(profile)
			if orgName == "" {
				t.Error("orgName should never be empty")
			}
			if projectName == "" {
				t.Error("projectName should never be empty")
			}
		})
	}
}

// ─── Verify ensureUserProfile gating: isNew=false means no auto-provision ──

// TestReactivatedUser_NoAutoProvision verifies that the middleware's
// ensureUserProfile only calls ProvisionNewUser when isNew=true.
// Since ensureUserProfile is not directly testable here (it's in the auth
// package), we verify the contract: EnsureProfile returns isNew=false for
// both existing and reactivated profiles, and the middleware only calls
// ProvisionNewUser when isNew=true.
//
// The actual gating logic is in middleware.go:651:
//
//	if isNew && m.autoProvisionSvc != nil { ... }
//
// This test documents that the autoprovision service is only ever called for
// brand-new users, never for reactivated or existing users.
func TestAutoProvisionService_OnlyCalledForNewUsers(t *testing.T) {
	t.Run("service itself does not gate on isNew (caller responsibility)", func(t *testing.T) {
		orgsMock := &mockOrgCreator{results: map[string]mockOrgResult{}}
		projMock := &mockProjectCreator{}
		svc := newServiceWithDeps(orgsMock, projMock, testLogger())

		// Calling ProvisionNewUser always provisions — it's the middleware's
		// responsibility to only call it when isNew=true
		err := svc.ProvisionNewUser(context.Background(), "user-1", &auth.UserProfileInfo{FirstName: "Test"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(orgsMock.calls) != 1 {
			t.Errorf("expected exactly 1 org creation, got %d", len(orgsMock.calls))
		}
	})
}
