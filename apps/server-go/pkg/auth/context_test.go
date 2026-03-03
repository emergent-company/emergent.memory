package auth

import (
	"context"
	"testing"
)

func TestContextWithUser(t *testing.T) {
	ctx := context.Background()
	user := &AuthUser{ID: "u1", Sub: "sub1", Email: "test@example.com"}

	ctx = ContextWithUser(ctx, user)

	got := UserFromContext(ctx)
	if got == nil {
		t.Fatal("expected user from context, got nil")
	}
	if got.ID != "u1" {
		t.Errorf("expected user ID u1, got %s", got.ID)
	}
}

func TestUserFromContext_Empty(t *testing.T) {
	ctx := context.Background()
	got := UserFromContext(ctx)
	if got != nil {
		t.Errorf("expected nil from empty context, got %v", got)
	}
}

func TestContextWithProjectID(t *testing.T) {
	ctx := context.Background()
	ctx = ContextWithProjectID(ctx, "proj-123")

	got := ProjectIDFromContext(ctx)
	if got != "proj-123" {
		t.Errorf("expected proj-123, got %s", got)
	}
}

func TestProjectIDFromContext_Empty(t *testing.T) {
	ctx := context.Background()
	got := ProjectIDFromContext(ctx)
	if got != "" {
		t.Errorf("expected empty string, got %s", got)
	}
}

func TestContextWithOrgID(t *testing.T) {
	ctx := context.Background()
	ctx = ContextWithOrgID(ctx, "org-456")

	got := OrgIDFromContext(ctx)
	if got != "org-456" {
		t.Errorf("expected org-456, got %s", got)
	}
}

func TestOrgIDFromContext_Empty(t *testing.T) {
	ctx := context.Background()
	got := OrgIDFromContext(ctx)
	if got != "" {
		t.Errorf("expected empty string, got %s", got)
	}
}

func TestInjectAuthContext(t *testing.T) {
	ctx := context.Background()
	user := &AuthUser{
		ID:        "u1",
		Sub:       "sub1",
		Email:     "test@example.com",
		ProjectID: "proj-123",
		OrgID:     "org-456",
	}

	ctx = InjectAuthContext(ctx, user)

	gotUser := UserFromContext(ctx)
	if gotUser == nil || gotUser.ID != "u1" {
		t.Errorf("expected user u1, got %v", gotUser)
	}

	gotProject := ProjectIDFromContext(ctx)
	if gotProject != "proj-123" {
		t.Errorf("expected proj-123, got %s", gotProject)
	}

	gotOrg := OrgIDFromContext(ctx)
	if gotOrg != "org-456" {
		t.Errorf("expected org-456, got %s", gotOrg)
	}
}

func TestInjectAuthContext_EmptyFields(t *testing.T) {
	ctx := context.Background()
	user := &AuthUser{
		ID:  "u1",
		Sub: "sub1",
	}

	ctx = InjectAuthContext(ctx, user)

	// User should be present
	gotUser := UserFromContext(ctx)
	if gotUser == nil || gotUser.ID != "u1" {
		t.Errorf("expected user u1, got %v", gotUser)
	}

	// ProjectID and OrgID should be empty (not injected when blank)
	if pid := ProjectIDFromContext(ctx); pid != "" {
		t.Errorf("expected empty project ID, got %s", pid)
	}
	if oid := OrgIDFromContext(ctx); oid != "" {
		t.Errorf("expected empty org ID, got %s", oid)
	}
}
