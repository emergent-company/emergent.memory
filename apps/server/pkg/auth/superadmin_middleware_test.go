package auth

import (
	"context"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

// TestRequireSuperadminAndFullAdmission pins the transport-level superadmin
// gates: RequireSuperadmin admits any active superadmin grant (full or
// readonly) while RequireSuperadminFull admits full only. Both fail closed for
// an unauthenticated caller, a caller without a real user id, and a caller with
// no grant. This is the middleware contract the /api/superadmin group relies on
// (#1086): removing the group middleware drops the gate to RequireAuth and this
// test — plus the routeguard tier assertions — is what fails.
func TestRequireSuperadminAndFullAdmission(t *testing.T) {
	tests := []struct {
		name       string
		mw         func(m *Middleware) echo.MiddlewareFunc
		user       *AuthUser
		lookupRole string
		wantErr    bool
	}{
		{name: "RequireSuperadmin no user refused", mw: func(m *Middleware) echo.MiddlewareFunc { return m.RequireSuperadmin() }, user: nil, wantErr: true},
		{name: "RequireSuperadmin empty id refused", mw: func(m *Middleware) echo.MiddlewareFunc { return m.RequireSuperadmin() }, user: &AuthUser{ID: ""}, lookupRole: RoleSuperadminFull, wantErr: true},
		{name: "RequireSuperadmin full admitted", mw: func(m *Middleware) echo.MiddlewareFunc { return m.RequireSuperadmin() }, user: &AuthUser{ID: "u1"}, lookupRole: RoleSuperadminFull, wantErr: false},
		{name: "RequireSuperadmin readonly admitted", mw: func(m *Middleware) echo.MiddlewareFunc { return m.RequireSuperadmin() }, user: &AuthUser{ID: "u1"}, lookupRole: RoleSuperadminReadonly, wantErr: false},
		{name: "RequireSuperadmin non-superadmin refused", mw: func(m *Middleware) echo.MiddlewareFunc { return m.RequireSuperadmin() }, user: &AuthUser{ID: "u1"}, lookupRole: "", wantErr: true},
		{name: "RequireSuperadminFull readonly refused", mw: func(m *Middleware) echo.MiddlewareFunc { return m.RequireSuperadminFull() }, user: &AuthUser{ID: "u1"}, lookupRole: RoleSuperadminReadonly, wantErr: true},
		{name: "RequireSuperadminFull full admitted", mw: func(m *Middleware) echo.MiddlewareFunc { return m.RequireSuperadminFull() }, user: &AuthUser{ID: "u1"}, lookupRole: RoleSuperadminFull, wantErr: false},
		{name: "RequireSuperadminFull non-superadmin refused", mw: func(m *Middleware) echo.MiddlewareFunc { return m.RequireSuperadminFull() }, user: &AuthUser{ID: "u1"}, lookupRole: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestMiddleware(t)
			m.superadminLookup = func(ctx context.Context, u string) (string, error) {
				return tt.lookupRole, nil
			}

			called := false
			handler := func(c echo.Context) error {
				called = true
				return nil
			}

			c := makeEchoCtx("", tt.user)
			err := tt.mw(m)(handler)(c)
			if tt.wantErr {
				require.Error(t, err, "middleware must reject")
				require.False(t, called, "handler must not run on rejection")
			} else {
				require.NoError(t, err, "middleware must admit")
				require.True(t, called, "handler must run on admission")
			}
		})
	}
}
