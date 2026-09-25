package users_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/domain/users"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

func TestRegisterRoutes(t *testing.T) {
	e := echo.New()
	h := users.NewHandler(&users.Service{})
	users.RegisterRoutes(e, h, &auth.Middleware{})

	got := map[string]bool{}
	for _, r := range e.Routes() {
		got[r.Method+" "+r.Path] = true
	}

	if !got["GET /api/users/search"] {
		t.Fatalf("route %q not registered; have %v", "GET /api/users/search", got)
	}
}

func TestSearchHandlerMissingEmail(t *testing.T) {
	e := echo.New()
	h := users.NewHandler(&users.Service{})

	req := httptest.NewRequest(http.MethodGet, "/api/users/search", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set(string(auth.UserContextKey), &auth.AuthUser{ID: "u1"})

	err := h.Search(c)
	if err == nil {
		t.Fatal("expected error for missing email query")
	}

	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("expected apperror.Error, got %T: %v", err, err)
	}
	if appErr.HTTPStatus != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", appErr.HTTPStatus)
	}
}

func TestSearchByEmailTooShort(t *testing.T) {
	svc := &users.Service{}
	if _, err := svc.SearchByEmail(context.Background(), "a", nil); err == nil {
		t.Fatal("expected error for 1-char query")
	}
}

func TestRepositorySearchByEmailTooShort(t *testing.T) {
	repo := &users.Repository{}
	got, err := repo.SearchByEmail(context.Background(), " a ", nil)
	if err != nil {
		t.Fatalf("short query must return empty result without error, got %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("short query must return empty results, got %d", len(got))
	}
}
