package sources

import (
	"errors"
	"io"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

const maxSourceWebhookBody = 5 << 20

// SourceEventHandler exposes the session-less App-scoped webhook endpoint.
type SourceEventHandler struct {
	dispatcher *SourceEventDispatcher
}

// NewSourceEventHandler builds the handler.
func NewSourceEventHandler(dispatcher *SourceEventDispatcher) *SourceEventHandler {
	return &SourceEventHandler{dispatcher: dispatcher}
}

// Webhook handles POST /api/webhooks/source-apps/:webhookKey. The route is
// outside session auth: the provider signature is the trust anchor.
//
// Errors are *apperror.Error, so apperror.HTTPErrorHandler is the single writer
// of the body and a sender gets the same {"error":{"code","message"}} envelope
// as every other endpoint (dev-rule-go-apperror-only, issue #863). This handler
// used to answer with seven plain-text c.String bodies, which was invisible to
// the echo.NewHTTPError ratchet and gave the sender nothing to branch on: the
// delivery log still shows a readable message, and the code now distinguishes
// the six causes instead of a string match — 400 unreadable_body (a body read
// that failed), 400 malformed_payload, 401 invalid_signature,
// 404 source_app_not_found, 503 source_app_unconfigured and 500 internal_error.
// The ratchet was widened in the same change (scripts/lint-ratchet.sh counts
// c.String error bodies), so the explicit exception this handler used to be is
// not silently re-created.
func (h *SourceEventHandler) Webhook(c echo.Context) error {
	key, err := uuid.Parse(c.Param("webhookKey"))
	if err != nil {
		return apperror.New(http.StatusNotFound, "source_app_not_found", "unknown app")
	}
	body, err := io.ReadAll(io.LimitReader(c.Request().Body, maxSourceWebhookBody))
	if err != nil {
		return apperror.New(http.StatusBadRequest, "unreadable_body", "cannot read request body")
	}
	verified, err := h.dispatcher.Verify(c.Request().Context(), key,
		c.Request().Header.Get("X-GitHub-Event"),
		c.Request().Header.Get("X-GitHub-Delivery"),
		c.Request().Header.Get("X-Hub-Signature-256"),
		body,
	)
	switch {
	case errors.Is(err, ErrSourceEventMalformed):
		return apperror.New(http.StatusBadRequest, "malformed_payload", "malformed payload")
	case errors.Is(err, ErrSourceEventInvalidSignature):
		return apperror.New(http.StatusUnauthorized, "invalid_signature", "invalid signature")
	case errors.Is(err, ErrSourceAppNotFound):
		return apperror.New(http.StatusNotFound, "source_app_not_found", "unknown app")
	case errors.Is(err, ErrSourceEventNoCredentials):
		return apperror.New(http.StatusServiceUnavailable, "source_app_unconfigured", "source app is not configured")
	case err != nil:
		// The cause is a repository or transport failure, not something the
		// sender can act on: keep it in Internal for the 5xx log line rather
		// than putting it in the body.
		return apperror.NewInternal("cannot accept delivery", err)
	}
	if verified != nil {
		go h.dispatcher.ProcessAsync(verified)
	}
	return c.NoContent(http.StatusAccepted)
}

// RegisterSourceEventRoutes wires the App-scoped webhook endpoint.
func RegisterSourceEventRoutes(e *echo.Echo, h *SourceEventHandler) {
	e.POST("/api/webhooks/source-apps/:webhookKey", h.Webhook)
}
