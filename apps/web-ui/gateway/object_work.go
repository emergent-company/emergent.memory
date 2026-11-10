package main

import (
	"context"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

// Device object-card work writes (iOS): the editable half of the workflow
// strip, the JSON twin of the drawer's status and assignee controls.
//
//	POST /api/objects/:id/status    {"status":"done"}    ↔  POST /objects/:id/status
//	POST /api/objects/:id/assignee  {"assignee":"Alice"} ↔  POST /objects/:id/assignee
//
// Both mirror uiObjectStatus / uiObjectAssignee exactly: an object with a
// work-item key goes through the work-item status/reassign routes, so
// status_changed events and the board's live refresh are preserved; an object
// without a key falls back to the generic graph-object update. They answer with
// the refreshed object card (objectCardFor), so the client re-renders from the
// server rather than from its own guess — the reason a failure never leaves an
// optimistically flipped value on screen.
//
// The device boundary is two-fold. The gateway admits these shapes via
// deviceObjectWritePath (auth.go); memory admits, for a device credential, only
// the two work-item paths the keyed branch calls (pkg/auth
// deviceWorkItemWritePath). A keyless object's generic update is therefore not
// device-reachable: the card only offers the pickers when it reports a key.

// objectCardStatusRequest is the POST /api/objects/:id/status body.
type objectCardStatusRequest struct {
	Status string `json:"status"`
}

// objectCardAssigneeRequest is the POST /api/objects/:id/assignee body. An
// empty assignee is meaningful: it clears the assignment.
type objectCardAssigneeRequest struct {
	Assignee string `json:"assignee"`
}

// postObjectStatus implements POST /api/objects/:id/status.
func (s *Server) postObjectStatus(c echo.Context) error {
	ctx, cancel := context.WithTimeout(c.Request().Context(), optionalFetchTimeout)
	defer cancel()

	var body objectCardStatusRequest
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	value := strings.TrimSpace(body.Status)
	if value == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "choose a status"})
	}

	obj, status, msg := s.resolveCardObject(ctx, c.Param("id"))
	if obj == nil {
		return c.JSON(status, map[string]string{"error": msg})
	}
	if err := s.writeObjectStatus(ctx, obj, value); err != nil {
		return objectWorkWriteError(c, err)
	}
	return s.respondObjectCard(c, ctx, obj.ID)
}

// postObjectAssignee implements POST /api/objects/:id/assignee.
func (s *Server) postObjectAssignee(c echo.Context) error {
	ctx, cancel := context.WithTimeout(c.Request().Context(), optionalFetchTimeout)
	defer cancel()

	var body objectCardAssigneeRequest
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	value := strings.TrimSpace(body.Assignee)

	obj, status, msg := s.resolveCardObject(ctx, c.Param("id"))
	if obj == nil {
		return c.JSON(status, map[string]string{"error": msg})
	}
	if err := s.writeObjectAssignee(ctx, obj, value); err != nil {
		return objectWorkWriteError(c, err)
	}
	return s.respondObjectCard(c, ctx, obj.ID)
}

// writeObjectStatus applies one status to the object the way the drawer does:
// the work-item status route when the object has a key, the generic graph
// update otherwise.
func (s *Server) writeObjectStatus(ctx context.Context, obj *GraphObject, status string) error {
	if obj.Key != "" {
		_, err := s.memory.SetWorkItemStatus(ctx, firstNonEmpty(obj.CanonicalID, obj.ID), status)
		return err
	}
	_, err := s.memory.UpdateObject(ctx, obj.ID, &UpdateObjectRequest{Status: &status})
	return err
}

// writeObjectAssignee applies one assignee (or clears it) the way the drawer
// does: the work-item reassign route when the object has a key, the generic
// graph update otherwise.
func (s *Server) writeObjectAssignee(ctx context.Context, obj *GraphObject, assignee string) error {
	if obj.Key != "" {
		_, err := s.memory.ReassignWorkItem(ctx, firstNonEmpty(obj.CanonicalID, obj.ID), assignee)
		return err
	}
	_, err := s.memory.UpdateObject(ctx, obj.ID, &UpdateObjectRequest{Assignee: &assignee})
	return err
}

// respondObjectCard re-reads the object after a write and answers with its
// refreshed card, so the client renders exactly what the server stored.
func (s *Server) respondObjectCard(c echo.Context, ctx context.Context, id string) error {
	obj, status, msg := s.resolveCardObject(ctx, id)
	if obj == nil {
		return c.JSON(status, map[string]string{"error": msg})
	}
	return c.JSON(http.StatusOK, s.objectCardFor(ctx, obj, ""))
}

// objectWorkWriteError maps a memory write failure onto the JSON error the card
// shows inline. A client-class status is passed through — a status outside the
// type's allowed set, a non-member assignee, or a credential not allowed on the
// object are all actionable — while anything else is reported as an upstream
// outage without echoing transport detail.
func objectWorkWriteError(c echo.Context, err error) error {
	captureError(err)
	status := memoryStatus(err)
	if status >= http.StatusBadRequest && status < http.StatusInternalServerError {
		reason := memoryHTTPErrorMessage(err)
		if reason == "" {
			reason = err.Error()
		}
		return c.JSON(status, map[string]string{"error": reason})
	}
	return c.JSON(http.StatusBadGateway, map[string]string{"error": "memory service unavailable"})
}
