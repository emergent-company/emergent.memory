package useraccess_test

import (
	"context"
	"testing"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/domain/useraccess"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

func TestRegisterRoutes(t *testing.T) {
	e := echo.New()
	h := useraccess.NewHandler(useraccess.NewService(nil))
	useraccess.RegisterRoutes(e, h, &auth.Middleware{})

	got := map[string]bool{}
	for _, r := range e.Routes() {
		got[r.Method+" "+r.Path] = true
	}

	if !got["GET /api/user/orgs-and-projects"] {
		t.Fatalf("route %q not registered; have %v", "GET /api/user/orgs-and-projects", got)
	}
}

func TestGetAccessTreeEmptyUser(t *testing.T) {
	svc := useraccess.NewService(nil)
	tree, err := svc.GetAccessTree(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tree == nil {
		t.Fatal("expected non-nil empty slice")
	}
	if len(tree) != 0 {
		t.Fatalf("expected empty tree for empty user, got %d entries", len(tree))
	}
}
