package auth

import (
	"context"
	"database/sql"
	"errors"

	"github.com/labstack/echo/v4"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// superadminRole reads the active superadmin role from core.superadmins. A
// missing or revoked row returns ("", nil) — "no superadmin grant". It is the
// canonical superadmin_full boundary shared by the middleware and handler-layer
// gates, and is the same query superadmin.Repository.IsSuperadminFull relies on
// (issue #940 review: the platform-admin authority must be the core.superadmins
// role, not a scope that an org_admin-minted admin:all token implies).
func superadminRole(ctx context.Context, db bun.IDB, userID string) (string, error) {
	if db == nil {
		return "", errors.New("auth: no database available for superadmin role lookup")
	}
	var role string
	err := db.NewSelect().
		TableExpr("core.superadmins").
		Column("role").
		Where("user_id = ?", userID).
		Where("revoked_at IS NULL").
		Limit(1).
		Scan(ctx, &role)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return role, nil
}

// IsSuperadminFull reports whether the authenticated user holds an active
// superadmin_full grant. It is the handler-layer counterpart to the
// RequireSuperadminFull middleware for authorization decisions that happen after
// the route middleware (e.g. a project-scoped endpoint's deployment-wide
// fallback). The caller identity comes from the authenticated context and the
// role from core.superadmins, so no caller-supplied value can self-satisfy it.
func IsSuperadminFull(ctx context.Context, db bun.IDB) (bool, error) {
	user := UserFromContext(ctx)
	if user == nil || user.ID == "" {
		return false, nil
	}
	role, err := superadminRole(ctx, db, user.ID)
	if err != nil {
		return false, err
	}
	return role == RoleSuperadminFull, nil
}

// RequireSuperadminFull returns middleware that admits only an active
// superadmin_full principal. It is the platform-admin gate for deployment-wide
// operator controls: unlike a scope gate, it is NOT satisfiable by an admin:all
// token (which an org_admin can mint) or a bare admin token.
func (m *Middleware) RequireSuperadminFull() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			user := GetUser(c)
			if user == nil {
				return apperror.ErrUnauthorized
			}
			if user.ID == "" {
				return apperror.NewForbidden("superadmin privilege required")
			}
			role, err := m.lookupSuperadminRole(c.Request().Context(), user.ID)
			if err != nil {
				m.log.Warn("failed to resolve superadmin role; failing closed", logger.Error(err))
				return apperror.ErrInternal
			}
			if role != RoleSuperadminFull {
				return apperror.NewForbidden("superadmin privilege required")
			}
			return next(c)
		}
	}
}
