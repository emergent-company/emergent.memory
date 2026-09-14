package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/emergent-company/go-daisy/components/ui"
	"github.com/labstack/echo/v4"
)

// --- project backups (memory /api/v1 backups domain) ---
//
// The backup routes live under the /api/v1 prefix (unlike the /api routes the
// rest of this client uses) and are organization-scoped except create, which
// is project-scoped. Memory gates the whole domain behind
// Config.Features.Backups: when the feature is off the routes are not mounted
// and every call returns 404 — the UI degrades that to an "unavailable" state
// (design D6) rather than an error.

// Backup statuses (memory's BackupStatus constants).
const (
	backupStatusCreating = "creating"
	backupStatusReady    = "ready"
	backupStatusFailed   = "failed"
	backupStatusDeleted  = "deleted"
)

// BackupType constants.
const (
	backupTypeFull        = "full"
	backupTypeIncremental = "incremental"
)

// Backup mirrors memory's backups.Backup entity (kb.backups). Field names are
// camelCase on the wire; nullable fields are pointers so "absent" is
// distinguishable from a zero value. Stats/Includes are free-form JSONB maps
// (object/chunk/chat counts etc.).
type Backup struct {
	ID               string         `json:"id"`
	OrganizationID   string         `json:"organizationId"`
	ProjectID        string         `json:"projectId"`
	ProjectName      string         `json:"projectName"`
	StorageKey       string         `json:"storageKey"`
	SizeBytes        int64          `json:"sizeBytes"`
	Status           string         `json:"status"`
	Progress         int            `json:"progress"`
	ErrorMessage     *string        `json:"errorMessage"`
	BackupType       string         `json:"backupType"`
	Includes         map[string]any `json:"includes"`
	Stats            map[string]any `json:"stats"`
	CreatedAt        time.Time      `json:"createdAt"`
	CreatedBy        *string        `json:"createdBy"`
	CompletedAt      *time.Time     `json:"completedAt"`
	ExpiresAt        *time.Time     `json:"expiresAt"`
	DeletedAt        *time.Time     `json:"deletedAt"`
	ManifestChecksum *string        `json:"manifestChecksum"`
	ContentChecksum  *string        `json:"contentChecksum"`
	ParentBackupID   *string        `json:"parentBackupId"`
	BaselineBackupID *string        `json:"baselineBackupId"`
	ChangeWindow     map[string]any `json:"changeWindow"`
}

// backupCursor is memory's ListResult.NextCursor — the pagination anchor of
// the last returned row. The gateway only needs it to distinguish "no more"
// from "truncated", so it is decoded for presence but never followed.
type backupCursor struct {
	CreatedAt time.Time `json:"createdAt"`
	ID        string    `json:"id"`
}

// backupListResult mirrors memory's backups.ListResult (GET
// /api/v1/organizations/:orgId/backups).
type backupListResult struct {
	Backups    []Backup      `json:"backups"`
	Total      int           `json:"total"`
	NextCursor *backupCursor `json:"nextCursor"`
}

// CreateBackupRequest mirrors memory's CreateBackupRequestDTO (POST
// /api/v1/projects/:projectId/backups). retentionDays defaults to 30 on the
// server; valid range is 1–365.
type CreateBackupRequest struct {
	IncludeDeleted bool `json:"includeDeleted"`
	IncludeChat    bool `json:"includeChat"`
	RetentionDays  int  `json:"retentionDays"`
}

// listBackups lists the org's backups, filtered to the active project (most
// recent first). A 404/not-found means the Memory deployment has the backup
// feature gated off (design D6) — callers degrade it to an "unavailable"
// state via isMemoryNotFound.
func (m *MemoryClient) ListBackups(ctx context.Context, orgID string) ([]Backup, int, error) {
	q := url.Values{}
	q.Set("project_id", m.projectIDFor(ctx))
	q.Set("limit", "100")
	path := "/api/v1/organizations/" + url.PathEscape(orgID) + "/backups?" + q.Encode()
	var out backupListResult
	if err := m.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, 0, err
	}
	return out.Backups, out.Total, nil
}

// GetBackup fetches one backup's details (GET
// /api/v1/organizations/:orgId/backups/:backupId). The response is the bare
// Backup entity.
func (m *MemoryClient) GetBackup(ctx context.Context, orgID, backupID string) (*Backup, error) {
	path := "/api/v1/organizations/" + url.PathEscape(orgID) + "/backups/" + url.PathEscape(backupID)
	var out Backup
	if err := m.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateBackup initiates an async project backup (POST
// /api/v1/projects/:projectId/backups). Memory answers 202 with the Backup
// entity in status "creating"; progress is tracked by re-listing/polling.
// retentionDays is forwarded as-is (0 lets memory default it to 30); values
// outside 1–365 are rejected by memory and surface as errors.
func (m *MemoryClient) CreateBackup(ctx context.Context, includeDeleted, includeChat bool, retentionDays int) (*Backup, error) {
	body := CreateBackupRequest{
		IncludeDeleted: includeDeleted,
		IncludeChat:    includeChat,
		RetentionDays:  retentionDays,
	}
	path := "/api/v1/projects/" + url.PathEscape(m.projectIDFor(ctx)) + "/backups"
	var out Backup
	if err := m.do(ctx, http.MethodPost, path, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DownloadBackup returns the pre-signed download URL for a ready backup (GET
// .../download). Memory answers 302 with the signed URL in the Location
// header; the URL must reach the BROWSER, not be followed by this client, so
// the request is made with redirects disabled (http.ErrUseLastResponse) and
// the Location header is returned as-is. Memory rejects the call unless the
// backup is status "ready".
func (m *MemoryClient) DownloadBackup(ctx context.Context, orgID, backupID string) (string, error) {
	path := "/api/v1/organizations/" + url.PathEscape(orgID) + "/backups/" + url.PathEscape(backupID) + "/download"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.baseURL+path, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+m.tokenFor(ctx))
	for k, v := range sessionHeaders(ctx) {
		req.Header.Set(k, v)
	}
	// Copy the short-timeout client but never follow the redirect: the 302
	// target is a presigned object-storage URL with a 1h expiry that must be
	// handed to the browser untouched.
	client := *m.http
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusFound {
		loc := resp.Header.Get("Location")
		if loc == "" {
			return "", fmt.Errorf("memory 302: download redirect carried no Location header")
		}
		return loc, nil
	}
	raw, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return "", readErr
	}
	if resp.StatusCode >= 400 {
		var e struct {
			Error apiError `json:"error"`
		}
		if json.Unmarshal(raw, &e) == nil && e.Error.Code != "" {
			err := fmt.Errorf("memory %d %s: %s", resp.StatusCode, e.Error.Code, e.Error.Message)
			captureMemoryError(http.MethodGet, path, resp.StatusCode, err)
			return "", err
		}
		err := fmt.Errorf("memory %d: %s", resp.StatusCode, string(raw))
		captureMemoryError(http.MethodGet, path, resp.StatusCode, err)
		return "", err
	}
	return "", fmt.Errorf("memory %d: download did not redirect (expected 302)", resp.StatusCode)
}

// DeleteBackup permanently deletes a backup (DELETE
// /api/v1/organizations/:orgId/backups/:backupId → 204 no body).
func (m *MemoryClient) DeleteBackup(ctx context.Context, orgID, backupID string) error {
	path := "/api/v1/organizations/" + url.PathEscape(orgID) + "/backups/" + url.PathEscape(backupID)
	return m.do(ctx, http.MethodDelete, path, nil, nil)
}

// --- org resolution (design D3) ---

// errBackupsNoOrg is the clear-error sentinel when the active project carries
// no organization id, so the org-scoped backup routes cannot be addressed.
// It surfaces as a page error state, never a 500.
var errBackupsNoOrg = errors.New("no organization is bound to the active project — backups are org-scoped in the connected Memory service")

// backupsOrgID resolves the organization id for the org-scoped backup routes
// (design D3). Web sessions carry the org directly (or can recover it from the
// active project via the tenancy listing); the API-key path resolves it from
// the token-bound current-project lookup. Callers render a clear error state
// when no org can be resolved (never a 500).
func (s *Server) backupsOrgID(ctx context.Context) (string, error) {
	// Web session: the org is authoritative when present. When only the project
	// is known (the project picker stores just the id), recover the org from the
	// project record. A user access token is never project-bound, so the
	// current-project lookup below returns nothing for these callers.
	if sc, ok := sessionContextFrom(ctx); ok {
		if sc.OrgID != "" {
			return sc.OrgID, nil
		}
		if sc.ProjectID == "" {
			return "", errBackupsNoOrg
		}
		orgID, err := s.projectOrgID(ctx, sc.ProjectID)
		if err != nil {
			return "", err
		}
		if orgID == "" {
			return "", errBackupsNoOrg
		}
		return orgID, nil
	}
	// API-key path: the token is project-bound, so the current-project lookup
	// resolves the org authoritatively.
	project, err := s.memory.GetCurrentProject(ctx)
	if err != nil {
		return "", err
	}
	if project == nil || project.OrgID == "" {
		return "", errBackupsNoOrg
	}
	return project.OrgID, nil
}

// projectOrgID resolves a project's org id from the tenancy listing. The
// project picker stores only the project id in the session, so org-scoped
// routes must recover the org from the project record. A project that is
// missing (or carries no org) yields an empty string, which callers map to
// errBackupsNoOrg.
func (s *Server) projectOrgID(ctx context.Context, projectID string) (string, error) {
	projects, err := s.memory.ListProjects(ctx)
	if err != nil {
		return "", err
	}
	for _, p := range projects {
		if p.ID == projectID {
			return p.OrgID, nil
		}
	}
	return "", nil
}

// --- UI handlers ---

// uiBackups renders the backups page: the create form plus the active
// project's backups, most recent first. The list load is best-effort in the
// skills-page style: a 404/not-found from Memory (feature gated off, design
// D6) or an unresolved org degrades to an unavailable state; any other load
// failure (Memory unreachable, etc.) renders an error state. ?created=1 /
// ?deleted=1 / ?err=1 surface PRG feedback.
func (s *Server) uiBackups(c echo.Context) error {
	ctx := c.Request().Context()
	var flashMsg string
	switch {
	case c.QueryParam("created") != "":
		flashMsg = "Backup started — the page refreshes until it finishes."
	case c.QueryParam("deleted") != "":
		flashMsg = "Backup deleted."
	}
	flashErr := flashError(c)

	orgID, err := s.backupsOrgID(ctx)
	if err != nil {
		// Unreachable/gated Memory or a project with no org: a clear error
		// state, never a 500.
		return s.page(c, pageTitle("Backups"), BackupsPage(nil, 0, "", err, flashMsg, flashErr))
	}
	backups, total, err := s.memory.ListBackups(ctx, orgID)
	if err != nil {
		if isMemoryNotFound(err) {
			// Memory deployment has backups disabled (design D6): degrade to
			// an unavailable state instead of a hard error.
			return s.page(c, pageTitle("Backups"), BackupsPage(nil, 0, "The connected Memory service does not expose backups.", nil, flashMsg, flashErr))
		}
		return s.page(c, pageTitle("Backups"), BackupsPage(nil, 0, "", err, flashMsg, flashErr))
	}
	if backups == nil {
		backups = []Backup{}
	}
	return s.page(c, pageTitle("Backups"), BackupsPage(backups, total, "", nil, flashMsg, flashErr))
}

// uiBackupDetail renders the dedicated backup page (GET /backups/:id): the
// overview, contents statistics, archive settings, a static archive-format
// explainer, and the integrity checksums. A load failure (including a missing
// backup or an unresolved org) renders the page's error state rather than a
// 500 — matching the list page's error handling.
func (s *Server) uiBackupDetail(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	orgID, err := s.backupsOrgID(ctx)
	if err != nil {
		return s.page(c, pageTitle("Backup"), BackupDetailPage(nil, err))
	}
	b, err := s.memory.GetBackup(ctx, orgID, id)
	if err != nil {
		return s.page(c, pageTitle("Backup"), BackupDetailPage(nil, err))
	}
	if b == nil {
		return s.page(c, pageTitle("Backup"), BackupDetailPage(nil, fmt.Errorf("backup %s not found", shortID(id))))
	}
	return s.page(c, pageTitle("Backup"), BackupDetailPage(b, nil))
}

// uiBackupCreate handles the create form (PRG → POST /backups): it forwards
// includeDeleted/includeChat/retentionDays to memory and redirects back to the
// list. retentionDays defaults to 30 when blank (matching memory's default);
// out-of-range values are rejected by memory and surface via the ?err= flash.
func (s *Server) uiBackupCreate(c echo.Context) error {
	ctx := c.Request().Context()
	includeDeleted := c.FormValue("includeDeleted") != ""
	includeChat := c.FormValue("includeChat") != ""
	retentionDays := 30
	if raw := c.FormValue("retentionDays"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return redirectWithError(c, "/backups", fmt.Errorf("retentionDays must be a number of days"))
		}
		retentionDays = n
	}
	if _, err := s.memory.CreateBackup(ctx, includeDeleted, includeChat, retentionDays); err != nil {
		return redirectWithError(c, "/backups", err)
	}
	return c.Redirect(http.StatusSeeOther, "/backups?created=1")
}

// uiBackupDownload handles a download click (GET /backups/:id/download): it
// resolves the org, verifies the backup is status "ready", asks memory for the
// pre-signed URL, and 302-redirects the BROWSER to it. A backup that is not
// ready (or gone) redirects back to the list with the error surfaced — the
// download link is only offered for ready backups in the first place.
func (s *Server) uiBackupDownload(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	orgID, err := s.backupsOrgID(ctx)
	if err != nil {
		return redirectWithError(c, "/backups", err)
	}
	b, err := s.memory.GetBackup(ctx, orgID, id)
	if err != nil {
		return redirectWithError(c, "/backups", err)
	}
	if b == nil || b.Status != backupStatusReady {
		return redirectWithError(c, "/backups", fmt.Errorf("backup %s is not ready for download", shortID(id)))
	}
	downloadURL, err := s.memory.DownloadBackup(ctx, orgID, id)
	if err != nil {
		return redirectWithError(c, "/backups", err)
	}
	return c.Redirect(http.StatusFound, downloadURL)
}

// uiBackupDelete handles the delete-confirm form (PRG → POST
// /backups/:id/delete). The row action opens a confirm dialog before posting,
// so a delete only happens on explicit confirmation.
func (s *Server) uiBackupDelete(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	orgID, err := s.backupsOrgID(ctx)
	if err != nil {
		return redirectWithError(c, "/backups", err)
	}
	if err := s.memory.DeleteBackup(ctx, orgID, id); err != nil {
		return redirectWithError(c, "/backups", err)
	}
	return c.Redirect(http.StatusSeeOther, "/backups?deleted=1")
}

// --- display helpers (shared with backups.templ) ---

// backupStatusIntent maps a backup status to a badge colour.
func backupStatusIntent(status string) ui.BadgeIntent {
	switch status {
	case backupStatusReady:
		return ui.BadgeSuccess
	case backupStatusFailed:
		return ui.BadgeError
	case backupStatusCreating:
		return ui.BadgeInfo
	case backupStatusDeleted:
		return ui.BadgeGhost
	default:
		return ui.BadgeNeutral
	}
}

// backupStatusLabel renders a friendly status label. An empty status from an
// older service degrades to "creating"-adjacent "unknown".
func backupStatusLabel(status string) string {
	switch status {
	case "":
		return "unknown"
	default:
		return status
	}
}

// backupStatusAnimate returns true for in-flight statuses so the badge dot
// pulses while the backup is being created.
func backupStatusAnimate(status string) bool {
	return status == backupStatusCreating
}

// backupTypeIntent maps a backup type to a badge colour: full reads as the
// stronger intent, incremental as informational.
func backupTypeIntent(t string) ui.BadgeIntent {
	switch t {
	case backupTypeFull:
		return ui.BadgeNeutral
	case backupTypeIncremental:
		return ui.BadgeInfo
	default:
		return ui.BadgeGhost
	}
}

// backupTypeLabel renders a friendly type label ("full" → "Full").
func backupTypeLabel(t string) string {
	switch t {
	case "":
		return "full" // every gateway-created backup is full today
	default:
		return t
	}
}

// backupSizeLabel renders a byte count compactly (bytes → GB).
func backupSizeLabel(n int64) string {
	switch {
	case n >= 1<<30:
		return strconv.FormatFloat(float64(n)/(1<<30), 'f', 1, 64) + " GB"
	case n >= 1<<20:
		return strconv.FormatFloat(float64(n)/(1<<20), 'f', 1, 64) + " MB"
	case n >= 1<<10:
		return strconv.FormatFloat(float64(n)/(1<<10), 'f', 1, 64) + " KB"
	default:
		return strconv.FormatInt(n, 10) + " B"
	}
}

// backupDate renders a time as a short date (for expiry) or "—" when absent.
func backupDate(t *time.Time) string {
	if t == nil || t.IsZero() {
		return "—"
	}
	return t.Format("2006-01-02")
}

// backupCreatedLabel renders a backup's creation time with relTime. Memory
// timestamps are RFC3339 UTC.
func backupCreatedLabel(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return relTime(t.UTC().Format(time.RFC3339))
}

// backupsAnyCreating reports whether any listed backup is still creating —
// the page auto-refreshes (polls) while one is in flight.
func backupsAnyCreating(backups []Backup) bool {
	for _, b := range backups {
		if b.Status == backupStatusCreating {
			return true
		}
	}
	return false
}

// checksumLabel shortens a checksum for badge display, keeping the full value
// in the title attribute.
func checksumLabel(c *string) string {
	if c == nil || *c == "" {
		return ""
	}
	s := *c
	if len(s) <= 16 {
		return s
	}
	return s[:8] + "…" + s[len(s)-4:]
}

// backupStatsSummary flattens a backup's Stats map into a short one-line
// summary of the counts memory records (documents, chunks, graph objects/
// relationships, chat, files). Unknown keys are ignored; empty maps yield "".
func backupStatsSummary(stats map[string]any) string {
	if len(stats) == 0 {
		return ""
	}
	labels := []struct {
		key   string
		label string
	}{
		{"documents", "docs"},
		{"chunks", "chunks"},
		{"graphObjects", "objects"},
		{"graphRelationships", "relations"},
		{"chatConversations", "chats"},
		{"chatMessages", "messages"},
		{"files", "files"},
	}
	parts := make([]string, 0, 4)
	for _, l := range labels {
		v, ok := stats[l.key]
		if !ok {
			continue
		}
		n := 0
		switch t := v.(type) {
		case float64:
			n = int(t)
		case int:
			n = t
		case int64:
			n = int(t)
		case json.Number:
			if i, err := t.Int64(); err == nil {
				n = int(i)
			}
		default:
			continue
		}
		if n > 0 {
			parts = append(parts, strconv.Itoa(n)+" "+l.label)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " · ")
}

// --- detail-page helpers ---

// backupStatRow is one labelled statistics entry on the backup details page.
type backupStatRow struct {
	Label string
	Key   string
	Value string
}

// backupStatRowDefs fixes the display order and labelling of the Stats keys
// memory records (object/chunk/chat counts). Unknown keys are ignored.
var backupStatRowDefs = []struct {
	key   string
	label string
}{
	{"documents", "Documents"},
	{"chunks", "Chunks"},
	{"graphObjects", "Objects"},
	{"graphRelationships", "Relationships"},
	{"chatConversations", "Conversations"},
	{"chatMessages", "Messages"},
	{"extractionJobs", "Extraction jobs"},
	{"projectMemberships", "Memberships"},
	{"files", "Files"},
	{"totalSizeBytes", "Total size"},
}

// backupStatRows flattens a backup's Stats map into labelled rows for the
// details page: known keys only, in a fixed display order, absent keys omitted.
// totalSizeBytes is formatted with backupSizeLabel; every other key renders as
// an integer count. Empty/absent maps yield no rows.
func backupStatRows(stats map[string]any) []backupStatRow {
	rows := make([]backupStatRow, 0, len(backupStatRowDefs))
	for _, def := range backupStatRowDefs {
		raw, ok := stats[def.key]
		if !ok {
			continue
		}
		n, ok := backupStatNumber(raw)
		if !ok {
			continue
		}
		value := strconv.FormatInt(n, 10)
		if def.key == "totalSizeBytes" {
			value = backupSizeLabel(n)
		}
		rows = append(rows, backupStatRow{Label: def.label, Key: def.key, Value: value})
	}
	return rows
}

// backupStatNumber coerces one Stats value to an int64. JSON decoding yields
// float64 (or json.Number when UseNumber is set); Go callers may pass int/int64.
func backupStatNumber(raw any) (int64, bool) {
	switch v := raw.(type) {
	case float64:
		return int64(v), true
	case int:
		return int64(v), true
	case int64:
		return v, true
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return i, true
		}
	}
	return 0, false
}

// backupIncludeRow is one archive include flag rendered as an on/off row.
type backupIncludeRow struct {
	Label string
	Key   string
	On    bool
}

// backupIncludeDefs fixes the display order of the include flags. Documents,
// chunks and graph are always captured; chat, journal and deleted items are
// opt-in per create request.
var backupIncludeDefs = []struct {
	key      string
	label    string
	alwaysOn bool
}{
	{"documents", "Documents", true},
	{"chunks", "Chunks", true},
	{"graph", "Graph", true},
	{"chat", "Chat", false},
	{"journal", "Journal", false},
	{"deleted", "Deleted items", false},
}

// backupIncludeRows resolves a backup's include flags for display. An absent
// always-on flag defaults to true (it is always captured); an absent opt-in
// flag defaults to false. An explicit boolean in the map always wins.
func backupIncludeRows(includes map[string]any) []backupIncludeRow {
	rows := make([]backupIncludeRow, 0, len(backupIncludeDefs))
	for _, def := range backupIncludeDefs {
		on := def.alwaysOn
		if v, ok := includes[def.key]; ok {
			if b, isBool := v.(bool); isBool {
				on = b
			}
		}
		rows = append(rows, backupIncludeRow{Label: def.label, Key: def.key, On: on})
	}
	return rows
}

// backupHasChecksum reports whether a checksum is present and non-empty.
func backupHasChecksum(c *string) bool {
	return c != nil && *c != ""
}

// backupLoadErr resolves the error shown on the details page: the original
// load error when present, otherwise a not-found error for a nil backup.
func backupLoadErr(b *Backup, loadErr error) error {
	if loadErr != nil {
		return loadErr
	}
	if b == nil {
		return errors.New("backup not found")
	}
	return nil
}

// backupTextOrDash renders a possibly-empty string field as "—".
func backupTextOrDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

// backupCreatedBy renders the creator id, or "—" when unknown.
func backupCreatedBy(c *string) string {
	if c == nil {
		return "—"
	}
	return backupTextOrDash(*c)
}

// backupDetailTime renders a nullable timestamp for the details overview as a
// UTC date-time, or "—" when absent.
func backupDetailTime(t *time.Time) string {
	if t == nil || t.IsZero() {
		return "—"
	}
	return t.UTC().Format("2006-01-02 15:04 UTC")
}

// backupErrorMessage renders a failed backup's error, with a neutral fallback
// so the integrity alert is never empty.
func backupErrorMessage(b *Backup) string {
	if b.ErrorMessage == nil || strings.TrimSpace(*b.ErrorMessage) == "" {
		return "The backup failed without an error message."
	}
	return *b.ErrorMessage
}

// backupIncludesLabel summarises the Includes map the way the manifest
// describes the backup (documents, chunks, graph always true; chat/deleted
// per request).
func backupIncludesLabel(includes map[string]any) string {
	if len(includes) == 0 {
		return ""
	}
	on := func(k string) bool {
		v, ok := includes[k]
		if !ok {
			return false
		}
		b, _ := v.(bool)
		return b
	}
	parts := make([]string, 0, 3)
	if on("chat") {
		parts = append(parts, "chat")
	}
	if on("deleted") {
		parts = append(parts, "deleted items")
	}
	return strings.Join(parts, ", ")
}
