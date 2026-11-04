package auth

import (
	"context"
	"testing"

	"github.com/google/uuid"
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

// TestUserIDFromContext pins the value-only accessor added for callers that
// attribute an action to a user without caring why the user is absent (#2260).
func TestUserIDFromContext(t *testing.T) {
	cases := []struct {
		name string
		ctx  context.Context
		want string
	}{
		{name: "absent user yields empty string", ctx: context.Background(), want: ""},
		{name: "user with an id yields it", ctx: ContextWithUser(context.Background(), &AuthUser{ID: "u1"}), want: "u1"},
		{name: "user without an id yields empty string", ctx: ContextWithUser(context.Background(), &AuthUser{}), want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := UserIDFromContext(tc.ctx); got != tc.want {
				t.Errorf("UserIDFromContext() = %q, want %q", got, tc.want)
			}
		})
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

func TestWithActor(t *testing.T) {
	ctx := context.Background()
	id := uuid.New()

	ctx = WithActor(ctx, "agent", &id)

	actorType, actorID, ok := ActorFromContext(ctx)
	if !ok {
		t.Fatal("expected actor present, got ok=false")
	}
	if actorType != "agent" {
		t.Errorf("expected actor type agent, got %s", actorType)
	}
	if actorID == nil || *actorID != id {
		t.Errorf("expected actor id %s, got %v", id, actorID)
	}
}

func TestWithActor_NilID(t *testing.T) {
	ctx := context.Background()

	ctx = WithActor(ctx, "system", nil)

	actorType, actorID, ok := ActorFromContext(ctx)
	if !ok {
		t.Fatal("expected actor present, got ok=false")
	}
	if actorType != "system" {
		t.Errorf("expected actor type system, got %s", actorType)
	}
	if actorID != nil {
		t.Errorf("expected nil actor id, got %v", actorID)
	}
}

func TestActorFromContext_Empty(t *testing.T) {
	ctx := context.Background()

	actorType, actorID, ok := ActorFromContext(ctx)
	if ok {
		t.Errorf("expected ok=false from empty context, got ok=true (%s, %v)", actorType, actorID)
	}
	if actorType != "" || actorID != nil {
		t.Errorf("expected empty actor from empty context, got (%s, %v)", actorType, actorID)
	}
}

// TestWithActor_Clear pins the "clear" semantics (issue: provenance followups):
// WithActor(ctx, "", nil) shadows a parent's actor so ActorFromContext reports
// no actor — a delegated run whose definition is unresolvable falls back rather
// than inheriting the parent agent.
func TestWithActor_Clear(t *testing.T) {
	parentID := uuid.New()
	ctx := WithActor(context.Background(), "agent", &parentID)

	// Sanity: the parent actor is present before clearing.
	if _, _, ok := ActorFromContext(ctx); !ok {
		t.Fatal("expected parent actor present before clear")
	}

	// Clear via an empty actor type.
	ctx = WithActor(ctx, "", nil)
	actorType, actorID, ok := ActorFromContext(ctx)
	if ok {
		t.Errorf("expected ok=false after clear, got ok=true (%s, %v)", actorType, actorID)
	}
	if actorType != "" || actorID != nil {
		t.Errorf("expected empty actor after clear, got (%s, %v)", actorType, actorID)
	}
}

func TestReactionWorkStatusContext(t *testing.T) {
	ctx := context.Background()
	if got := AgentWorkStatusSubjectFromContext(ctx); got != "" {
		t.Fatalf("empty context subject = %q, want empty", got)
	}
	if AgentWorkStatusAllowedFromContext(ctx) {
		t.Fatal("empty context unexpectedly allows work-status writes")
	}

	ctx = WithAgentWorkStatusSubject(ctx, "canonical-1")
	if got := AgentWorkStatusSubjectFromContext(ctx); got != "canonical-1" {
		t.Fatalf("subject = %q, want canonical-1", got)
	}
	if AgentWorkStatusAllowedFromContext(ctx) {
		t.Fatal("subject alone must not allow work-status writes")
	}

	ctx = WithAgentWorkStatusAllowed(ctx)
	if !AgentWorkStatusAllowedFromContext(ctx) {
		t.Fatal("stamped exemption was not honored")
	}
}

// TestAgentWorkStatusSubjectShadowing pins the clear form used by delegated
// child runs: an empty subject must shadow an inherited parent subject so a
// child cannot assert the parent's board-status exemption.
func TestAgentWorkStatusSubjectShadowing(t *testing.T) {
	parent := WithAgentWorkStatusSubject(context.Background(), "parent-subject")
	child := WithAgentWorkStatusSubject(parent, "")
	if got := AgentWorkStatusSubjectFromContext(child); got != "" {
		t.Fatalf("child subject = %q, want empty", got)
	}
}
