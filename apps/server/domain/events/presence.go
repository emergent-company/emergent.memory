package events

import (
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// PresenceWindow is how recent an SSE heartbeat must be for a member to count
// as online. It is deliberately a little above HeartbeatInterval (30s) so one
// missed tick does not flap a live connection offline.
const PresenceWindow = 90 * time.Second

// PresenceEntry is one online member as reported by the presence endpoint.
type PresenceEntry struct {
	UserID       string    `json:"userId"`
	ProjectID    string    `json:"projectId,omitempty"`
	LastActivity time.Time `json:"lastActivity"`
}

// PresenceResponse is the presence payload. OnlineNow is the distinct user
// count; Entries carries the per-user detail.
type PresenceResponse struct {
	OnlineNow int             `json:"onlineNow"`
	Entries   []PresenceEntry `json:"entries"`
}

// HandlePresence reports who is online right now, either for a project
// (?projectId=, caller must have project access) or for an organization
// (?orgId=, caller must be org_admin). Presence is read from the in-memory SSE
// registry and is never persisted: a restart simply reports nobody online.
//
// @Summary      Get live member presence
// @Description  Returns members with a recent SSE heartbeat for a project or an organization. Project scope requires project access; org scope requires org_admin.
// @Tags         events
// @Produce      json
// @Param        projectId query string false "Project ID (UUID)"
// @Param        orgId query string false "Organization ID (UUID)"
// @Success      200 {object} PresenceResponse
// @Failure      400 {object} apperror.Error "Missing scope"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      403 {object} apperror.Error "Forbidden"
// @Router       /api/events/presence [get]
// @Security     bearerAuth
func (h *Handler) HandlePresence(c echo.Context) error {
	user := auth.MustGetUser(c)
	projectID := c.QueryParam("projectId")
	orgID := c.QueryParam("orgId")

	if projectID == "" && orgID == "" {
		return apperror.NewBadRequest("projectId or orgId query param is required")
	}

	now := time.Now().UTC()
	entries := h.snapshotPresence(now)

	switch {
	case projectID != "":
		// Reuses the same project authorization the SSE stream uses: the caller
		// must belong to the project's owning org, and a project-bound token must
		// address its own project.
		if err := h.auth.AuthorizeProject(c, projectID); err != nil {
			return err
		}
		filtered := entries[:0]
		for _, e := range entries {
			if e.ProjectID == projectID {
				filtered = append(filtered, e)
			}
		}
		entries = filtered
	case orgID != "":
		ok, err := auth.CanAdministerOrg(c.Request().Context(), h.db, orgID, user.ID)
		if err != nil {
			return apperror.NewDatabase("presence query failed", err)
		}
		if !ok {
			return apperror.ErrForbidden
		}
		entries, err = h.filterOrgMembers(c, orgID, entries)
		if err != nil {
			return err
		}
	}

	return c.JSON(http.StatusOK, PresenceResponse{OnlineNow: len(entries), Entries: entries})
}

// snapshotPresence collects live connections, deduplicated per user (a user may
// hold several tabs or projects open) and keeps only fresh heartbeats.
func (h *Handler) snapshotPresence(now time.Time) []PresenceEntry {
	h.connMu.RLock()
	conns := make([]*SSEConnection, 0, len(h.connections))
	for _, conn := range h.connections {
		conns = append(conns, conn)
	}
	h.connMu.RUnlock()

	byUser := make(map[string]PresenceEntry, len(conns))
	for _, conn := range conns {
		if conn.UserID == "" || now.Sub(conn.LastHeartbeat) > PresenceWindow {
			continue
		}
		prev, ok := byUser[conn.UserID]
		if ok && prev.LastActivity.After(conn.LastHeartbeat) {
			continue
		}
		byUser[conn.UserID] = PresenceEntry{
			UserID:       conn.UserID,
			ProjectID:    conn.ProjectID,
			LastActivity: conn.LastHeartbeat,
		}
	}

	out := make([]PresenceEntry, 0, len(byUser))
	for _, e := range byUser {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UserID < out[j].UserID })
	return out
}

// filterOrgMembers keeps only entries whose user belongs to orgID.
func (h *Handler) filterOrgMembers(c echo.Context, orgID string, entries []PresenceEntry) ([]PresenceEntry, error) {
	if h.db == nil || len(entries) == 0 {
		return entries, nil
	}
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, e.UserID)
	}
	members := []string{}
	err := h.db.NewRaw(`
		SELECT user_id::text FROM kb.organization_memberships
		WHERE organization_id = ?::uuid AND user_id IN (?)
	`, orgID, bun.In(ids)).Scan(c.Request().Context(), &members)
	if err != nil {
		h.log.Error("failed to filter org presence", logger.Error(err), slog.String("org_id", orgID))
		return nil, apperror.NewDatabase("presence query failed", err)
	}
	allowed := make(map[string]bool, len(members))
	for _, id := range members {
		allowed[id] = true
	}
	out := entries[:0]
	for _, e := range entries {
		if allowed[e.UserID] {
			out = append(out, e)
		}
	}
	return out, nil
}
