package sources

import (
	"errors"
	"io"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
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
func (h *SourceEventHandler) Webhook(c echo.Context) error {
	key, err := uuid.Parse(c.Param("webhookKey"))
	if err != nil {
		return c.String(http.StatusNotFound, "unknown app")
	}
	body, err := io.ReadAll(io.LimitReader(c.Request().Body, maxSourceWebhookBody))
	if err != nil {
		return c.String(http.StatusBadRequest, "cannot read request body")
	}
	verified, err := h.dispatcher.Verify(c.Request().Context(), key,
		c.Request().Header.Get("X-GitHub-Event"),
		c.Request().Header.Get("X-GitHub-Delivery"),
		c.Request().Header.Get("X-Hub-Signature-256"),
		body,
	)
	switch {
	case errors.Is(err, ErrSourceEventMalformed):
		return c.String(http.StatusBadRequest, "malformed payload")
	case errors.Is(err, ErrSourceEventInvalidSignature):
		return c.String(http.StatusUnauthorized, "invalid signature")
	case errors.Is(err, ErrSourceAppNotFound):
		return c.String(http.StatusNotFound, "unknown app")
	case errors.Is(err, ErrSourceEventNoCredentials):
		return c.String(http.StatusServiceUnavailable, "source app is not configured")
	case err != nil:
		return c.String(http.StatusInternalServerError, "cannot accept delivery")
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
