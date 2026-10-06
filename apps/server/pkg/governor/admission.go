package governor

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// Middleware returns an echo middleware that admits each request through the
// governor. It must run AFTER authentication so the project id can be resolved.
// On ErrShed / ErrNoCapacity it returns HTTP 429 with a Retry-After header and
// an apperror-shaped JSON body.
func (g *Governor) Middleware(class Class) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if !g.cfg.Enabled {
				return next(c)
			}

			projectID := resolveProjectID(c)
			release, err := g.Admit(projectID, class)
			if err != nil {
				governorAdmissionRejected.WithLabelValues(class.String(), c.Path()).Inc()
				return g.reject(c, err)
			}
			defer release()

			return next(c)
		}
	}
}

// resolveProjectID derives a stable admission key for the request. It prefers
// the authenticated project id (header/token), then the project id stamped on
// the request context, and finally falls back to the user id or remote IP so
// unprojected callers still share a keyed limiter.
func resolveProjectID(c echo.Context) string {
	if id, err := auth.GetProjectID(c); err == nil && id != "" {
		return id
	}
	if id := auth.ProjectIDFromContext(c.Request().Context()); id != "" {
		return id
	}
	if user := auth.GetUser(c); user != nil && user.ID != "" {
		return "user:" + user.ID
	}
	if ip := c.RealIP(); ip != "" {
		return "ip:" + ip
	}
	return "unknown"
}

// reject writes a 429 response with a Retry-After header and an apperror-shaped
// JSON body consistent with pkg/apperror.
func (g *Governor) reject(c echo.Context, err error) error {
	retryAfter := g.cfg.RetryAfterSeconds
	if retryAfter < 1 {
		retryAfter = 1
	}
	c.Response().Header().Set("Retry-After", strconv.Itoa(retryAfter))

	code := "no_capacity"
	message := "admission limit reached"
	if errors.Is(err, ErrShed) {
		code = "shed"
		message = "system under load, request shed"
	}

	return c.JSON(http.StatusTooManyRequests, map[string]any{
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	})
}
