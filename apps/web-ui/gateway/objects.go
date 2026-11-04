package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/labstack/echo/v4"
	"golang.org/x/sync/errgroup"

	"github.com/emergent-company/go-daisy/components/nav"
)

// --- objects browser ---

// objectsStats is the small stats section's data shown on the objects browser.
type objectsStats struct {
	TotalObjects int   // via count endpoint
	PendingEmbed int64 // embedding queue pending (objects)
	FailedEmbed  int64 // embedding queue failed (objects)
	TotalErr     error // non-nil → total count unavailable
	EmbedErr     error // non-nil → embedding queue stats unavailable
}

// objectsPageData carries everything the objects browser renders, in one struct
// so the page signature stays stable while its presentation evolves. Browse
// mode fills Objects/HasMore/NextCursor/Stats; search mode fills Results.
type objectsPageData struct {
	Query      string
	Mode       string // normalized to "fulltext" | "hybrid" | "unified"
	TypeFilter string
	BranchID   string
	// Order is the browse list's created_at direction: "asc", or "" for the
	// server default (newest first). Search results are relevance-ranked and
	// ignore it. Kept as "" for the default so URLs stay clean and every
	// pre-existing link keeps its meaning.
	Order        string
	Types        []string
	Branches     []Branch
	Objects      []GraphObject        // browse mode rows
	Results      []ObjectSearchResult // search mode hits
	HasMore      bool
	NextCursor   string
	Stats        *objectsStats // nil → deferred stats partial; non-nil → loaded
	TypeUIByType map[string]typeUI
	// StatusOptionsJSON maps each workflow-declaring object type to the statuses
	// the bulk Set-status dialog may offer for it (allowed set, or the canonical
	// board order when unconstrained). Types absent here declare no workflow,
	// so the dialog disables the action for a selection containing one (#2242).
	StatusOptionsJSON string
	LoadErr           error

	// Actor provenance filter (mirrors the server's actor_type / actor_id /
	// provenance params). ActorType is "" for "any actor"; ActorID is only
	// meaningful when ActorType is set. Provenance is normalized to
	// created|updated|any.
	ActorType  string
	ActorID    string
	Provenance string

	// ActorOptions lists the selectable identities for the generic browser's
	// actor filter: project members as "user" options and agent definitions as
	// "agent" options. Empty when the sources could not be fetched; the filter
	// then still renders any active selection.
	ActorOptions []objectActorOption

	// AgentID/AgentName mark the agent-scoped view (GET /agents/:id/objects):
	// the provenance filter is fixed to (agent, AgentID), the page renders an
	// agent-scoped header, and the global search / Q&A are omitted because the
	// server's search endpoints cannot be provenance-scoped.
	AgentID   string
	AgentName string

	// Preview marks a dev-gallery render: pagination, search submission, and
	// bulk mutations render without their live wiring (no hx-* attributes, no
	// backend form action) so the sample page stays inert. Always false in the
	// real handlers.
	Preview bool

	// FlashMsg/FlashErr carry post-redirect-get feedback (a bulk action's result,
	// or its failure) into the page's toasts.
	FlashMsg string
	FlashErr error
}

// listParams builds the graph-object list query for this page's active filters
// at one cursor position.
func (d objectsPageData) listParams(cursor string) ObjectListParams {
	return ObjectListParams{
		BranchID:   d.BranchID,
		TypeFilter: d.TypeFilter,
		Cursor:     cursor,
		Limit:      25,
		ActorType:  d.ActorType,
		ActorID:    d.ActorID,
		Provenance: d.Provenance,
		Order:      d.Order,
	}
}

// objectProvenanceMode is one selectable provenance mode: the server value plus
// its display label.
type objectProvenanceMode struct {
	Value string
	Label string
}

// defaultObjectProvenance is the provenance mode used when a request omits
// `provenance` (or supplies an unrecognised value): "any" applies no
// provenance narrowing.
const defaultObjectProvenance = "any"

// objectProvenanceModes lists the provenance modes in display order with their
// copy. The values match the server's accepted `provenance` param.
func objectProvenanceModes() []objectProvenanceMode {
	return []objectProvenanceMode{
		{Value: "any", Label: "Any"},
		{Value: "created", Label: "Created by"},
		{Value: "updated", Label: "Updated by"},
	}
}

// normalizeObjectProvenance coerces a raw `provenance` query value to one the
// server accepts (created|updated|any). An absent or unrecognised value falls
// back to "any", so the rendered control and the dispatched list always agree.
func normalizeObjectProvenance(provenance string) string {
	switch provenance {
	case "created", "updated", "any":
		return provenance
	default:
		return defaultObjectProvenance
	}
}

// normalizeObjectOrder coerces a raw `order` query value to the only value the
// browse list needs to carry: "asc". Everything else (absent, "desc", garbage)
// is the server's default, newest first, so an unrecognised value never reaches
// the API and the rendered header always agrees with the request.
func normalizeObjectOrder(raw string) string {
	if strings.EqualFold(strings.TrimSpace(raw), "asc") {
		return "asc"
	}
	return ""
}

// objectsSortDir is the effective created_at direction for the table header:
// "desc" unless the page asked for ascending. It is the value the header renders
// as its aria-sort state and the one the sort link toggles away from.
func objectsSortDir(data objectsPageData) string {
	if data.Order == "asc" {
		return "asc"
	}
	return "desc"
}

// effectiveObjectProvenance is the defense-in-depth form of
// normalizeObjectProvenance: a provenance mode only narrows results alongside a
// complete actor pair, and the server 400s on `provenance` without
// `actor_type`, so an empty actor type clears the mode to "any" rather than
// forwarding a bare provenance.
func effectiveObjectProvenance(provenance, actorType string) string {
	if actorType == "" {
		return defaultObjectProvenance
	}
	return normalizeObjectProvenance(provenance)
}

// objectActorTypes lists the actor types the server's provenance filter accepts,
// in display order.
var objectActorTypes = []string{"user", "agent", "system"}

// normalizeObjectActorType keeps a raw `actor_type` value only when the server
// accepts it; anything else (empty or unknown) becomes "any actor".
func normalizeObjectActorType(actorType string) string {
	if slices.Contains(objectActorTypes, actorType) {
		return actorType
	}
	return ""
}

// actorTypeLabel renders the display label for an actor type value.
func actorTypeLabel(actorType string) string {
	switch actorType {
	case "user":
		return "User"
	case "agent":
		return "Agent"
	case "system":
		return "System"
	default:
		return "Any actor"
	}
}

// objectActorOption is one selectable identity in the generic browser's actor
// filter: the (actor_type, actor_id) pair plus a human label and the appearance
// the picker renders beside it. Project members become "user" options and agent
// definitions become "agent" options, so the filter lists real actors instead of
// asking for a pasted UUID.
type objectActorOption struct {
	Type  string // "user" | "agent"
	ID    string
	Label string

	// AvatarURL is a project member's profile picture. Empty renders initials.
	AvatarURL string
	// Icon and Color are an agent's declared appearance (its uiConfig), rendered
	// as the agent's glyph; both empty fall back to the generic bot glyph.
	Icon  string
	Color string
}

// actorKey encodes an actor option as the one value the actor picker submits:
// "<actor_type>:<actor_id>". Carrying the pair in a single value is what keeps
// it consistent — the server rejects actor_id without actor_type, and a single
// control cannot submit one without the other. An incomplete pair encodes to
// "" (the "Any actor" row).
func actorKey(actorType, actorID string) string {
	if actorType == "" || actorID == "" {
		return ""
	}
	return actorType + ":" + actorID
}

// splitActorKey decodes the combined actor picker value back into a normalized
// (actor_type, actor_id) pair. A missing separator or unknown actor type yields
// the empty pair, which clears the filter.
func splitActorKey(key string) (actorType, actorID string) {
	rawType, rawID, ok := strings.Cut(key, ":")
	if !ok {
		return "", ""
	}
	return normalizeObjectActorType(rawType), strings.TrimSpace(rawID)
}

// actorOptionsByType filters the selectable actors to one type, preserving order.
func actorOptionsByType(options []objectActorOption, actorType string) []objectActorOption {
	return slices.DeleteFunc(slices.Clone(options), func(o objectActorOption) bool {
		return o.Type != actorType
	})
}

// actorOptionOf returns the loaded option for the (actorType, actorID) pair, so
// the picker's closed state can borrow the same avatar the option row renders.
func actorOptionOf(options []objectActorOption, actorType, actorID string) (objectActorOption, bool) {
	for _, o := range options {
		if o.Type == actorType && o.ID == actorID {
			return o, true
		}
	}
	return objectActorOption{}, false
}

// actorOptionLabel is the display label for the active actor: the loaded option's
// label when there is one, otherwise a type + truncated id fallback so a former
// member or a URL-supplied pair still reads as an active filter instead of
// resetting to "Any actor".
func actorOptionLabel(options []objectActorOption, actorType, actorID string) string {
	if o, ok := actorOptionOf(options, actorType, actorID); ok {
		return o.Label
	}
	if actorType == "" || actorID == "" {
		return "Any actor"
	}
	return actorTypeLabel(actorType) + " · " + shortActorID(actorID)
}

// shortActorID is the label suffix for an active actor that is not among the
// loaded options (a former member, or a pair passed by URL): a truncated id so
// the control still shows the active filter rather than silently resetting.
func shortActorID(id string) string {
	const n = 8
	if len(id) > n {
		return id[:n] + "…"
	}
	return id
}

// memberActorLabel renders a project member as an actor option label, preferring
// the display name, then first + last name, then the email, then the id.
func memberActorLabel(m ProjectMemberDto) string {
	if name := strings.TrimSpace(m.DisplayName); name != "" {
		return name
	}
	if name := strings.TrimSpace(strings.TrimSpace(m.FirstName) + " " + strings.TrimSpace(m.LastName)); name != "" {
		return name
	}
	if email := strings.TrimSpace(m.Email); email != "" {
		return email
	}
	return m.ID
}

// objectsScopeDescription is the subtitle for an agent-scoped objects view.
func objectsScopeDescription(agentName string) string {
	return "Objects created or updated by " + agentName + "."
}

// loadObjectFilterOptions fills the filter dropdowns' data (branches + compiled
// types), shared by the generic and agent-scoped objects views. Both fetches are
// best-effort: a failure leaves the dropdowns empty rather than blanking the
// page.
func (s *Server) loadObjectFilterOptions(ctx context.Context, data *objectsPageData) {
	var (
		branches    []Branch
		branchesErr error
		compiled    *CompiledSchemaTypes
		compiledErr error
	)
	var g errgroup.Group
	g.Go(func() error { branches, branchesErr = s.memory.ListBranches(ctx); return nil })
	g.Go(func() error { compiled, compiledErr = s.memory.GetCompiledTypes(ctx); return nil })
	_ = g.Wait()
	captureError(branchesErr)
	captureError(compiledErr)

	data.Branches = branches
	if compiled != nil {
		data.TypeUIByType = objectTypeUIMap(compiled.ObjectTypes)
		// Visible declarations only: a shadowed duplicate would otherwise list
		// the same type name twice in the filter (and a name that only survives
		// as a shadowed loser is not selectable at all).
		statusOptions := make(map[string][]string)
		for _, t := range visibleCompiledTypes(compiled.ObjectTypes) {
			data.Types = append(data.Types, t.Name)
			// A type with no board flag and no allowed set declares no workflow:
			// its status is a plain schema property, not a work-status vocabulary.
			if !t.BoardEnabled && len(t.AllowedStatuses) == 0 {
				continue
			}
			opts := t.AllowedStatuses
			if len(opts) == 0 {
				opts = boardStatusOrder
			}
			statusOptions[t.Name] = opts
		}
		if raw, err := json.Marshal(statusOptions); err == nil {
			data.StatusOptionsJSON = string(raw)
		}
	}
}

// loadObjectActorOptions fills the generic browser's actor filter with real,
// selectable identities: the project's members (actor_type "user") and its agent
// definitions (actor_type "agent"). Both fetches are best-effort and run
// concurrently: a failure drops that group rather than blanking the page.
func (s *Server) loadObjectActorOptions(ctx context.Context, data *objectsPageData) {
	var (
		members   []ProjectMemberDto
		agents    []AgentDefinitionSummary
		memberErr error
		agentErr  error
	)
	var g errgroup.Group
	g.Go(func() error { members, memberErr = s.memory.ListMembers(ctx); return nil })
	g.Go(func() error { agents, agentErr = s.memory.ListAgentDefinitions(ctx); return nil })
	_ = g.Wait()
	captureError(memberErr)
	captureError(agentErr)

	options := make([]objectActorOption, 0, len(members)+len(agents))
	for _, m := range members {
		options = append(options, objectActorOption{
			Type:      "user",
			ID:        m.ID,
			Label:     memberActorLabel(m),
			AvatarURL: strings.TrimSpace(m.AvatarURL),
		})
	}
	for _, a := range agents {
		options = append(options, objectActorOption{
			Type:  "agent",
			ID:    a.ID,
			Label: a.Name,
			Icon:  agentIcon(a.UIConfig),
			Color: agentColor(a.UIConfig),
		})
	}
	data.ActorOptions = options
}

// loadObjectBrowsePage fetches one cursor page of objects into data, applying
// the active filters (branch/type/actor/provenance). It is shared by the
// generic browse view and the agent-scoped view so both paginate identically.
func (s *Server) loadObjectBrowsePage(ctx context.Context, data *objectsPageData, cursor string) error {
	objects, nextCursor, err := s.memory.ListGraphObjectsPage(ctx, data.listParams(cursor))
	if err != nil {
		return err
	}
	data.Objects = objects
	data.HasMore = nextCursor != ""
	data.NextCursor = nextCursor
	return nil
}

// uiObjects renders the objects browser: searchable graph objects, filterable
// by type, branch, and actor provenance, with a small stats section and
// cursor-paginated browse.
func (s *Server) uiObjects(c echo.Context) error {
	ctx := c.Request().Context()
	branchID := c.QueryParam("branch")
	typeFilter := c.QueryParam("type")
	query := c.QueryParam("q")
	mode := normalizeObjectsSearchMode(c.QueryParam("mode"))
	cursor := c.QueryParam("cursor")
	actorType, actorID := objectActorFilter(c)

	data := objectsPageData{
		Query:      query,
		Mode:       mode,
		TypeFilter: typeFilter,
		BranchID:   branchID,
		ActorType:  actorType,
		ActorID:    actorID,
		Provenance: effectiveObjectProvenance(c.QueryParam("provenance"), actorType),
		Order:      normalizeObjectOrder(c.QueryParam("order")),
		FlashMsg:   objectsBulkFlash(c),
		FlashErr:   flashError(c),
	}
	s.loadObjectFilterOptions(ctx, &data)

	if query != "" {
		// Search endpoints do not accept the actor provenance filter, so search
		// mode stays project-wide; the provenance control is hidden there.
		var results []ObjectSearchResult
		var err error
		if mode == "unified" {
			results, err = s.memory.SearchObjectsUnified(ctx, query, typeFilter, branchID, 25)
		} else {
			results, _, err = s.memory.SearchObjects(ctx, mode, query, typeFilter, branchID, 25, 0)
		}
		if err != nil {
			data.LoadErr = userFacingError(err)
			return s.page(c, pageTitle("Objects"), ObjectsPage(data))
		}
		data.Results = results
		return s.page(c, pageTitle("Objects"), ObjectsPage(data))
	}

	// Browse mode: one page of 25 (most-recent-first). The object count and
	// embedding-queue stats for the small stats section are optional and slow
	// (issue #1098: /api/embeddings/progress ~29s, /api/graph/objects/count
	// ~6s), so they are fetched by a deferred HTMX partial (/objects/stats)
	// after first paint instead of gating the browse render. data.Stats is left
	// nil to select that deferred path. The selectable actor identities are
	// loaded only in browse mode: the actor filter is browse-only (the search
	// endpoints cannot apply it), so search mode does not pay for the fetches.
	s.loadObjectActorOptions(ctx, &data)
	if err := s.loadObjectBrowsePage(ctx, &data, cursor); err != nil {
		data.LoadErr = userFacingError(err)
		return s.page(c, pageTitle("Objects"), ObjectsPage(data))
	}
	return s.page(c, pageTitle("Objects"), ObjectsPage(data))
}

// objectActorFilter reads and normalizes the actor provenance pair from the
// request. The filter always keys on the (actor_type, actor_id) pair: the server
// rejects actor_id without actor_type, and an actor_type with no id would narrow
// to rows whose actor id is NULL — not a usable "any user/agent" filter — so an
// incomplete pair is dropped rather than forwarded.
//
// The generic browser's actor picker submits the pair as one combined `actor`
// value ("<type>:<id>"); explicit actor_type/actor_id params (the agent-scoped
// view's hidden inputs, pagination links, and bookmarks) keep working too.
func objectActorFilter(c echo.Context) (actorType, actorID string) {
	if key := strings.TrimSpace(c.QueryParam("actor")); key != "" {
		actorType, actorID = splitActorKey(key)
	} else {
		actorType = normalizeObjectActorType(c.QueryParam("actor_type"))
		actorID = strings.TrimSpace(c.QueryParam("actor_id"))
	}
	if actorType == "" || actorID == "" {
		return "", ""
	}
	return actorType, actorID
}

// uiAgentObjects renders the object browser scoped to one agent's provenance:
// the objects that agent created or updated. It reuses ObjectsPage, objectsList
// and the browse partial, fixing the actor filter to (agent, id). The route id
// IS the kb.agents UUID the provenance columns store, so it maps straight to
// actor_id.
func (s *Server) uiAgentObjects(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")

	agent, err := s.memory.GetAgentDefinition(ctx, id)
	if err != nil {
		return s.page(c, pageTitle("Objects"), ObjectsPage(objectsPageData{LoadErr: userFacingError(err)}))
	}

	data := objectsPageData{
		AgentID:    id,
		AgentName:  agent.Name,
		ActorType:  "agent",
		ActorID:    id,
		Provenance: normalizeObjectProvenance(c.QueryParam("provenance")),
		TypeFilter: c.QueryParam("type"),
		BranchID:   c.QueryParam("branch"),
		Order:      normalizeObjectOrder(c.QueryParam("order")),
		FlashMsg:   objectsBulkFlash(c),
		FlashErr:   flashError(c),
	}
	s.loadObjectFilterOptions(ctx, &data)

	if err := s.loadObjectBrowsePage(ctx, &data, c.QueryParam("cursor")); err != nil {
		data.LoadErr = userFacingError(err)
		return s.page(c, pageTitle(agent.Name, "Objects"), ObjectsPage(data))
	}
	return s.page(c, pageTitle(agent.Name, "Objects"), ObjectsPage(data))
}

// uiObjectsPartial returns the next page of object rows plus a replacement
// "Load more" button (or an empty slot when there are no more) as an HTMX
// fragment for the browser's cursor pagination. It carries the search/filter
// context forward so appended rows match the active view.
func (s *Server) uiObjectsPartial(c echo.Context) error {
	ctx := c.Request().Context()
	branchID := c.QueryParam("branch")
	typeFilter := c.QueryParam("type")
	query := c.QueryParam("q")
	mode := c.QueryParam("mode")
	cursor := c.QueryParam("cursor")
	actorType, actorID := objectActorFilter(c)
	provenance := effectiveObjectProvenance(c.QueryParam("provenance"), actorType)
	order := normalizeObjectOrder(c.QueryParam("order"))

	// Search mode has no cursor pagination (it renders the top 25 only).
	if query != "" {
		renderPartial(c.Response().Writer, c.Request(), objectsBrowserRowsPartial(objectsPageData{}))
		return nil
	}

	data := objectsPageData{
		Query:      query,
		Mode:       mode,
		TypeFilter: typeFilter,
		BranchID:   branchID,
		ActorType:  actorType,
		ActorID:    actorID,
		Provenance: provenance,
		Order:      order,
	}
	if err := s.loadObjectBrowsePage(ctx, &data, cursor); err != nil {
		captureError(err)
		return echo.NewHTTPError(http.StatusBadGateway, "failed to load more objects")
	}

	compiled, _ := s.memory.GetCompiledTypes(ctx)
	if compiled != nil {
		data.TypeUIByType = objectTypeUIMap(compiled.ObjectTypes)
	}

	renderPartial(c.Response().Writer, c.Request(), objectsBrowserRowsPartial(data))
	return nil
}

// objectsPartialURL builds the /objects/partial URL for the "Load more" button,
// carrying the cursor and the active search/filter context forward.
func objectsPartialURL(data objectsPageData) string {
	q := url.Values{}
	if data.NextCursor != "" {
		q.Set("cursor", data.NextCursor)
	}
	if data.Query != "" {
		q.Set("q", data.Query)
	}
	if data.Mode != "" {
		q.Set("mode", data.Mode)
	}
	if data.TypeFilter != "" {
		q.Set("type", data.TypeFilter)
	}
	if data.BranchID != "" {
		q.Set("branch", data.BranchID)
	}
	if data.ActorType != "" {
		q.Set("actor_type", data.ActorType)
		if data.ActorID != "" {
			q.Set("actor_id", data.ActorID)
		}
	}
	if data.Provenance != "" && data.Provenance != defaultObjectProvenance {
		q.Set("provenance", data.Provenance)
	}
	if data.Order != "" {
		q.Set("order", data.Order)
	}
	return "/objects/partial?" + q.Encode()
}

// objectsStatsURL builds the deferred stats-partial URL, carrying the active
// branch so the object count stays branch-scoped (the synchronous pre-deferral
// path passed branchID to CountObjects; the deferred request must too, issue
// #1098).
func objectsStatsURL(branchID string) string {
	if branchID == "" {
		return "/objects/stats"
	}
	return "/objects/stats?branch=" + url.QueryEscape(branchID)
}

// optionalFetchTimeout bounds the deferred HTMX partial calls (object count,
// embedding progress, similar objects). These sections are optional: a slow
// backend must degrade them to an empty/unavailable state quickly rather than
// hold a deferred request open (issues #1096, #1098).
const optionalFetchTimeout = 10 * time.Second

// uiObjectsStatsPartial renders the objects browser's small stats row as an HTMX
// fragment. The object count + embedding-queue aggregates are slow (issue
// #1098), so they are fetched here — after the browse page has already painted —
// with a short timeout; on any failure the section degrades to its "unavailable"
// state without affecting the already-rendered list.
func (s *Server) uiObjectsStatsPartial(c echo.Context) error {
	ctx, cancel := context.WithTimeout(c.Request().Context(), optionalFetchTimeout)
	defer cancel()
	branchID := c.QueryParam("branch")

	var (
		count       int
		countErr    error
		progress    *EmbeddingProgress
		progressErr error
	)
	var g errgroup.Group
	g.Go(func() error { count, countErr = s.memory.CountObjects(ctx, branchID); return nil })
	g.Go(func() error { progress, progressErr = s.memory.GetEmbeddingProgress(ctx); return nil })
	_ = g.Wait()
	captureError(countErr)
	captureError(progressErr)

	stats := objectsStats{TotalObjects: count, TotalErr: countErr, EmbedErr: progressErr}
	if progress != nil {
		stats.PendingEmbed = progress.Objects.Pending
		stats.FailedEmbed = progress.Objects.Failed
	}
	renderPartial(c.Response().Writer, c.Request(), objectsBrowserStats(objectsPageData{Stats: &stats}))
	return nil
}

// searchScoreLabel formats a search hit's relevance score for the score badge.
func searchScoreLabel(f float32) string {
	return strconv.FormatFloat(float64(f), 'f', 2, 32)
}

// defaultObjectsSearchMode is the search mode used when a request omits `mode`
// (or supplies an unrecognised value). Unified fuses graph and text ranking, so
// the default search is not keyword-only.
const defaultObjectsSearchMode = "unified"

// normalizeObjectsSearchMode coerces a raw `mode` query value to one of the
// three modes the search handlers accept. An absent or unrecognised value falls
// back to the default, so the rendered label, the value the form submits, and
// the dispatched search always agree. Without this, `objectsSearchModeLabel`
// would render the default label while `SearchObjects` fell through to its
// full-text implementation.
func normalizeObjectsSearchMode(mode string) string {
	switch mode {
	case "fulltext", "hybrid", "unified":
		return mode
	default:
		return defaultObjectsSearchMode
	}
}

// objectsSearchMode is one search mode option of the split search button's
// dropdown: the submitted value plus its label and a short description of what
// the mode does.
type objectsSearchMode struct {
	Value       string
	Label       string
	Description string
}

// objectsSearchModes lists the search modes in display order (best default
// first) with their dropdown copy. The order matches the modes the search
// handlers accept (unified / hybrid / fulltext).
func objectsSearchModes() []objectsSearchMode {
	return []objectsSearchMode{
		{Value: "unified", Label: "Unified", Description: "Fuses graph and text ranking for the best overall matches."},
		{Value: "hybrid", Label: "Hybrid", Description: "Blends keyword and semantic (vector) matching."},
		{Value: "fulltext", Label: "Full-text", Description: "Keyword matching only — fast and exact."},
	}
}

// objectsSearchModeLabel renders the display label for a search mode, falling
// back to the default (Unified) for an absent or unknown value so the split
// button always shows a mode.
func objectsSearchModeLabel(mode string) string {
	for _, m := range objectsSearchModes() {
		if m.Value == mode {
			return m.Label
		}
	}
	return "Unified"
}

// uiObjectsKnowledge runs the "ask the graph" RAG Q&A (POST /objects/knowledge)
// and returns the grounded answer as an HTMX fragment for the answer box. An
// empty question redirects back to the browser; a backend failure renders an
// inline error state in place of the answer.
func (s *Server) uiObjectsKnowledge(c echo.Context) error {
	ctx := c.Request().Context()
	if err := c.Request().ParseForm(); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid form submission")
	}
	question := strings.TrimSpace(c.FormValue("question"))
	branch := c.FormValue("branch")
	if question == "" {
		return c.Redirect(http.StatusSeeOther, "/objects")
	}
	answer, sessionID, err := s.memory.QueryKnowledge(ctx, question, branch)
	if err != nil {
		captureError(err)
		renderPartial(c.Response().Writer, c.Request(), objectsKnowledgeError(err))
		return nil
	}
	renderPartial(c.Response().Writer, c.Request(), objectsKnowledgeAnswer(answer, sessionID))
	return nil
}

// loadObjectDetailShell loads the shared header/nav data every object subpage
// renders: the object itself, its branch, type accent, resolved workflow
// control, assignee candidates (workflow types only), source provenance, and the
// optional ?from= origin trail. A failed object fetch fills LoadErr; the page
// components render their whole-page error state from it.
func (s *Server) loadObjectDetailShell(ctx context.Context, id, from string) objectDetailShellData {
	obj, err := s.memory.GetGraphObject(ctx, id)
	if err != nil {
		return objectDetailShellData{LoadErr: userFacingError(err)}
	}

	// ?from= carries where the user came from (an extraction run's object list).
	// It is resolved best-effort: an unknown or stale origin must never break
	// the object page — the header simply falls back to Objects > <object>.
	trail := objectOriginTrail(ctx, from, s.memory.GetDocument)

	var (
		compiled    *CompiledSchemaTypes
		compiledErr error
		branches    []Branch
		edges       []GraphRelationship
		edgesErr    error
	)
	var g errgroup.Group
	g.Go(func() error { compiled, compiledErr = s.memory.GetCompiledTypes(ctx); return nil })
	g.Go(func() error { branches, _ = s.memory.ListBranches(ctx); return nil })
	g.Go(func() error { edges, edgesErr = s.memory.GetObjectEdges(ctx, id); return nil })
	_ = g.Wait()
	captureError(compiledErr)
	captureError(edgesErr)

	branchRef := resolveObjectBranch(branches, obj.BranchID)
	sources := objectSources(*obj)
	s.resolveObjectSourceNames(ctx, sources)

	var (
		typeUIByType  map[string]typeUI
		statusControl *objectStatusControl
		typeLabel     string
	)
	if compiled != nil {
		typeUIByType = objectTypeUIMap(compiled.ObjectTypes)
		statusControl = objectStatusControlFor(compiled.ObjectTypes, obj.Type)
		typeLabel = objectPreviewTypeLabel(compiled.ObjectTypes, obj.Type)
	} else {
		// The schema could not be loaded, so we cannot tell whether the type
		// declares a workflow; keep the stored status visible as the header
		// badge rather than dropping it.
		statusControl = objectStatusControlUnavailable()
	}

	var assignees []boardAssigneeCandidate
	if !objectWorkControlDisabled(statusControl) {
		assignees = s.boardAssigneeCandidates(ctx)
	}

	return objectDetailShellData{
		Obj:          obj,
		Branch:       branchRef,
		TypeUIByType: typeUIByType,
		Status:       statusControl,
		Assignees:    assignees,
		Trail:        trail,
		TypeLabel:    typeLabel,
		From:         from,
		Compiled:     compiled,
		Sources:      sources,
		Edges:        edges,
		EdgeCount:    len(edges),
		EdgesErr:     edgesErr,
	}
}

// uiObject renders the object Overview subpage: the workflow panel, record
// facts, and editable properties. Relationships moved to its own rail tab, so
// the overview no longer renders them or resolves related objects.
func (s *Server) uiObject(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	from := c.QueryParam("from")
	flashErr := flashError(c)

	data := s.loadObjectDetailShell(ctx, id, from)
	if data.LoadErr != nil {
		return s.page(c, pageTitle("Object"), ObjectDetailPage(nil, objectBranchRef{}, nil, nil, data.LoadErr, "", flashErr, nil, nil, nil, nil, nil, "", 0))
	}
	obj := data.Obj

	labelSuggestions := s.objectLabelSuggestions(ctx)

	var propDefs []objectPropertyDef
	if data.Compiled != nil {
		propDefs = compiledTypePropertyDefs(data.Compiled.ObjectTypes, obj.Type)
	}

	flashMsg := ""
	if c.QueryParam("updated") != "" {
		flashMsg = "Object updated."
	}
	return s.page(c, pageTitle(graphObjectLabel(*obj)), ObjectDetailPage(
		obj, data.Branch, data.Sources, data.Assignees,
		nil, flashMsg, flashErr, labelSuggestions, propDefs, data.TypeUIByType,
		data.Status, data.Trail, data.TypeLabel, data.EdgeCount,
	))
}

// uiObjectHistoryPage renders the History subpage: the shared object shell plus
// the full version timeline. The preview drawer keeps a compact inline history;
// this page is the complete, linkable version.
func (s *Server) uiObjectHistoryPage(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	data := s.loadObjectDetailShell(ctx, id, c.QueryParam("from"))
	data.Active = "history"
	data.FlashErr = flashError(c)
	if data.LoadErr != nil {
		return s.page(c, pageTitle("Object"), objectHistoryPage(data, nil, nil))
	}

	versions, err := s.memory.GetObjectHistory(ctx, id)
	if err != nil {
		captureError(err)
		versions = nil
	}
	if c.QueryParam("updated") != "" {
		data.FlashMsg = "Object updated."
	}
	return s.page(c, pageTitle(graphObjectLabel(*data.Obj)), objectHistoryPage(data, versions, err))
}

// uiObjectSimilarPage renders the Similar objects subpage. It intentionally does
// not fetch the vector-similarity results itself: the deferred placeholder
// requests /objects/:id/similar/partial after paint, so slow similarity search
// never holds the page (issue #1096).
func (s *Server) uiObjectSimilarPage(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	data := s.loadObjectDetailShell(ctx, id, c.QueryParam("from"))
	data.Active = "similar"
	data.FlashErr = flashError(c)
	if c.QueryParam("updated") != "" {
		data.FlashMsg = "Object updated."
	}
	if data.LoadErr != nil {
		return s.page(c, pageTitle("Object"), objectSimilarPage(data))
	}
	return s.page(c, pageTitle(graphObjectLabel(*data.Obj)+" · Similar"), objectSimilarPage(data))
}

// uiObjectRelationshipsPage renders the Relationships subpage: the shared
// object shell plus the relationship list/empty state and the Connect dialog.
// Edges are already loaded by the shell (for the rail count), so the page does
// not re-fetch them.
func (s *Server) uiObjectRelationshipsPage(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	data := s.loadObjectDetailShell(ctx, id, c.QueryParam("from"))
	data.Active = "relationships"
	data.FlashErr = flashError(c)
	if c.QueryParam("updated") != "" {
		data.FlashMsg = "Object updated."
	}
	if data.LoadErr != nil {
		return s.page(c, pageTitle("Object"), objectRelationshipsPage(data, nil, nil, nil, nil))
	}

	var relTypes []CompiledType
	if data.Compiled != nil {
		relTypes = data.Compiled.RelationshipTypes
	}
	var related []GraphObject
	if data.EdgesErr == nil {
		related = s.loadRelatedObjects(ctx, data.Obj, data.Edges)
	}
	return s.page(c, pageTitle(graphObjectLabel(*data.Obj)+" · Relationships"), objectRelationshipsPage(data, relTypes, data.Edges, related, data.EdgesErr))
}

// objectSimilarFetchLimit is the raw similar-object fetch size. The count badge
// uses it both for the filtered count and to mark a saturated result with "+".
const objectSimilarFetchLimit = 10

// uiObjectSimilarCountPartial renders the nav rail's deferred similar-object
// count badge. The vector search is optional and can be slow (issue #1096), so
// the rail asks for this tiny fragment after paint. A zero count renders
// nothing, which removes the badge. A raw fetch that hit the limit is marked
// with "+" because more matches may exist.
func (s *Server) uiObjectSimilarCountPartial(c echo.Context) error {
	ctx, cancel := context.WithTimeout(c.Request().Context(), optionalFetchTimeout)
	defer cancel()
	id := c.Param("id")
	similar, err := s.memory.GetSimilarObjects(ctx, id, objectSimilarFetchLimit)
	if err != nil {
		captureError(err)
		renderPartial(c.Response().Writer, c.Request(), objectRailCount("", "object-similar-count"))
		return nil
	}
	matches := filterSimilarMatches(similar)
	label := ""
	if len(matches) > 0 {
		label = strconv.Itoa(len(matches))
		if len(similar) >= objectSimilarFetchLimit {
			label += "+"
		}
	}
	renderPartial(c.Response().Writer, c.Request(), objectRailCount(label, "object-similar-count"))
	return nil
}

// objectOriginTrail resolves an optional ?from= breadcrumb origin into a trail.
// Only a document extraction run is recognised:
//
//	/documents/<docID>/extractions/<jobID>
//
// The trail stops at the document's Extraction history: the run's short id is
// already shown in the object's Source card, so repeating it in the trail adds
// noise. The document is fetched best-effort for its label. An unknown shape, a
// missing document, or a backend failure yields nil, so the object page falls
// back to Objects > <object> and never fails because of an origin hint.
func objectOriginTrail(ctx context.Context, from string, getDoc func(context.Context, string) (*Document, error)) []nav.BreadcrumbItem {
	docID, _, ok := parseExtractionOrigin(from)
	if !ok || getDoc == nil {
		return nil
	}
	doc, err := getDoc(ctx, docID)
	if err != nil || doc == nil {
		return nil
	}
	base := "/documents/" + url.PathEscape(docID)
	return nav.Crumbs(
		"Documents", "/documents",
		documentName(*doc), base,
		"Extraction history", base+"/extractions",
	)
}

// parseExtractionOrigin accepts exactly /documents/<id>/extractions/<id> and
// rejects every other shape. Query and fragment markers are refused so a
// crafted origin cannot smuggle extra path semantics into the trail.
func parseExtractionOrigin(from string) (docID, jobID string, ok bool) {
	if !strings.HasPrefix(from, "/") || strings.ContainsAny(from, "?#") {
		return "", "", false
	}
	parts := strings.Split(strings.TrimPrefix(from, "/"), "/")
	if len(parts) != 4 || parts[0] != "documents" || parts[2] != "extractions" {
		return "", "", false
	}
	docID, err := url.PathUnescape(parts[1])
	if err != nil || docID == "" || strings.Contains(docID, "/") {
		return "", "", false
	}
	jobID, err = url.PathUnescape(parts[3])
	if err != nil || jobID == "" || strings.Contains(jobID, "/") {
		return "", "", false
	}
	return docID, jobID, true
}

// uiObjectSimilarPartial renders the "Similar objects" section as an HTMX
// fragment. It is fetched after the object detail page paints (issue #1096), so
// a slow /similar (vector search) can no longer hold the page render; a failure
// degrades to the "no similar objects" empty state.
func (s *Server) uiObjectSimilarPartial(c echo.Context) error {
	ctx, cancel := context.WithTimeout(c.Request().Context(), optionalFetchTimeout)
	defer cancel()
	id := c.Param("id")
	similar, err := s.memory.GetSimilarObjects(ctx, id, objectSimilarFetchLimit)
	if err != nil {
		captureError(err)
		similar = nil
	}
	renderPartial(c.Response().Writer, c.Request(), objectSimilarObjects(&GraphObject{ID: id}, filterSimilarMatches(similar)))
	return nil
}

// uiObjectStatus applies the object header's status picker. A workflow object
// with a key goes through the work-item status endpoint so status_changed events
// and board live-refresh semantics are preserved; an object without a work item
// (no key) falls back to the generic graph-object update. A preview surface
// re-renders the drawer body in place; a page surface redirects with PRG
// feedback and the optional ?from= trail.
func (s *Server) uiObjectStatus(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	surface := objectWorkSurfaceFromForm(c)
	value := strings.TrimSpace(c.FormValue("status"))

	obj, err := s.memory.GetGraphObject(ctx, id)
	if err != nil {
		captureError(err)
		return s.renderObjectWorkResponse(c, surface, id, err)
	}

	if value == "" {
		return s.renderObjectWorkResponse(c, surface, id, fmt.Errorf("choose a status"))
	}

	updatedID := id
	if obj.Key != "" {
		_, err = s.memory.SetWorkItemStatus(ctx, firstNonEmpty(obj.CanonicalID, obj.ID), value)
	} else {
		var updated *GraphObject
		updated, err = s.memory.UpdateObject(ctx, id, &UpdateObjectRequest{Status: &value})
		if updated != nil && updated.ID != "" {
			updatedID = updated.ID
		}
	}
	if err != nil {
		captureError(err)
		return s.renderObjectWorkResponse(c, surface, id, err)
	}
	return s.renderObjectWorkResponse(c, surface, updatedID, nil)
}

// uiObjectAssignee applies the object header's assignee picker. The empty value
// is meaningful: it clears the assignment (Unassigned). Workflow objects with a
// key use the work-item reassign endpoint; objects without a key fall back to
// the generic graph-object update.
func (s *Server) uiObjectAssignee(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	surface := objectWorkSurfaceFromForm(c)
	value := strings.TrimSpace(c.FormValue("assignee"))

	obj, err := s.memory.GetGraphObject(ctx, id)
	if err != nil {
		captureError(err)
		return s.renderObjectWorkResponse(c, surface, id, err)
	}

	updatedID := id
	if obj.Key != "" {
		_, err = s.memory.ReassignWorkItem(ctx, firstNonEmpty(obj.CanonicalID, obj.ID), value)
	} else {
		var updated *GraphObject
		updated, err = s.memory.UpdateObject(ctx, id, &UpdateObjectRequest{Assignee: &value})
		if updated != nil && updated.ID != "" {
			updatedID = updated.ID
		}
	}
	if err != nil {
		captureError(err)
		return s.renderObjectWorkResponse(c, surface, id, err)
	}
	return s.renderObjectWorkResponse(c, surface, updatedID, nil)
}

// objectWorkSurface is the POST context shared by the status/assignee controls.
type objectWorkSurface struct {
	Preview bool
	Actions string
	Rel     string
	From    string
}

// objectWorkSurfaceFromForm reads the control form's surface context. Page
// forms carry only the optional from; preview forms add surface=preview plus
// the drawer's ?actions=board / ?rel= context.
func objectWorkSurfaceFromForm(c echo.Context) objectWorkSurface {
	from := c.FormValue("from")
	if from == "" {
		from = c.QueryParam("from")
	}
	return objectWorkSurface{
		Preview: c.FormValue("surface") == "preview",
		Actions: c.FormValue("actions"),
		Rel:     c.FormValue("rel"),
		From:    from,
	}
}

// renderObjectWorkResponse renders the control POST's result: a refreshed
// preview body for the drawer surface, or a redirect for a full page. actionErr
// is rendered visibly in the preview rather than being silently swallowed.
func (s *Server) renderObjectWorkResponse(c echo.Context, surface objectWorkSurface, id string, actionErr error) error {
	if surface.Preview {
		return s.renderObjectPreview(c, id, surface.Rel, surface.Actions == "board", actionErr)
	}
	if actionErr != nil {
		return redirectWithError(c, objectWorkRedirectPath(id, surface.From, nil), actionErr)
	}
	return c.Redirect(http.StatusSeeOther, objectWorkRedirectPath(id, surface.From, url.Values{"updated": []string{"1"}}))
}

// objectWorkRedirectPath builds the object page redirect for a control POST,
// preserving the optional ?from= origin trail and adding the given query params.
func objectWorkRedirectPath(id, from string, params url.Values) string {
	q := url.Values{}
	if from != "" {
		q.Set("from", from)
	}
	for k, vs := range params {
		for _, v := range vs {
			q.Set(k, v)
		}
	}
	path := "/objects/" + url.PathEscape(id)
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	return path
}

// uiObjectUpdate applies an object edit from the detail form (key, status,
// labels, prop_* properties) as a delta merge and redirects to the updated
// object — the graph store is versioned, so memory returns a NEW id for the
// updated version and the redirect target is that id, not the submitted one.
// ?updated=1 carries PRG feedback so the detail page shows a success toast;
// failures redirect with ?err= so the real reason is surfaced instead of
// silently discarding the save.
func (s *Server) uiObjectUpdate(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	if err := c.Request().ParseForm(); err != nil {
		return redirectWithError(c, "/objects/"+url.PathEscape(id), err)
	}

	req := &UpdateObjectRequest{ReplaceLabels: true}
	if key := c.FormValue("key"); key != "" {
		req.Key = &key
	}
	if status := c.FormValue("status"); status != "" {
		req.Status = &status
	}
	req.Labels = indexedFormValues(c.Request().PostForm, "labels")

	// Every rendered property carries a `propm_present_<name>` marker. Start
	// from "delete" for each rendered property, then overwrite the ones with a
	// submitted value: emptying a field (or a TagList) now clears the stored key
	// instead of being silently skipped (#2330). nil is the server's delete
	// sentinel; an untouched boolean stays unset instead of persisting false.
	props := map[string]any{}
	for field := range c.Request().PostForm {
		if name, ok := strings.CutPrefix(field, "propm_present_"); ok && name != "" {
			props[name] = nil
		}
	}
	for field, vals := range c.Request().PostForm {
		name, ok := strings.CutPrefix(field, "prop_")
		if !ok || name == "" {
			continue
		}
		if i := strings.IndexByte(name, '['); i >= 0 {
			name = name[:i] // TagList field prop_x[0] → x
		}
		if indexed := indexedFormValues(c.Request().PostForm, "prop_"+name); indexed != nil {
			if len(indexed) > 0 {
				props[name] = indexed
			}
			continue
		}
		last := ""
		for _, v := range vals {
			if v != "" {
				last = v
			}
		}
		if last == "" {
			continue
		}
		// A jsonTextarea submits its whole document as this one scalar value, so
		// the parsed form is what keeps an object a map (and an array a list)
		// instead of a stringified copy.
		parsed := parseFormJSON(last)
		if parsed == nil {
			// "null" is valid JSON but not a property value: nil is the server's
			// delete sentinel, so accepting it would erase the property on a save
			// the user meant as an edit. Clearing the field is how a property is
			// removed; a string property can still store the text by quoting it.
			return redirectWithError(c, "/objects/"+url.PathEscape(id), fmt.Errorf("%s: null is not a value — clear the field to remove the property, or quote it to store the text", name))
		}
		props[name] = parsed
	}
	if len(props) > 0 {
		req.Properties = props
	}

	updated, err := s.memory.UpdateObject(ctx, id, req)
	if err != nil {
		return redirectWithError(c, "/objects/"+url.PathEscape(id), err)
	}
	s.invalidateObjectLabelSuggestions(ctx)
	// The graph store is versioned: PATCH returns a new object id for the
	// updated version. Redirect to it (memory resolves either the version or
	// canonical id, so stale links still land on the head version) and carry
	// ?updated=1 so the detail page flashes a success toast.
	return c.Redirect(http.StatusSeeOther, "/objects/"+url.PathEscape(updated.ID)+"?updated=1")
}

// uiObjectRelationshipCreate creates a relationship from the Relationships
// subpage modal (type + src/dst) and redirects back to that subpage on both
// success and failure.
func (s *Server) uiObjectRelationshipCreate(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	path := "/objects/" + url.PathEscape(id) + "/relationships"
	relType := c.FormValue("type")
	srcID := c.FormValue("src_id")
	dstID := c.FormValue("dst_id")
	if relType == "" || srcID == "" || dstID == "" {
		return c.Redirect(http.StatusSeeOther, path)
	}
	err := s.memory.CreateRelationship(ctx, &CreateRelationshipRequest{Type: relType, SrcID: srcID, DstID: dstID})
	if err != nil {
		captureError(err)
		return redirectWithError(c, path, err)
	}
	return c.Redirect(http.StatusSeeOther, path)
}

// uiObjectSearch is the JSON autocomplete endpoint behind the "add
// relationship" modal: it full-text-searches objects and returns their
// id / display label / type for the src/dst pickers.
func (s *Server) uiObjectSearch(c echo.Context) error {
	ctx := c.Request().Context()
	q := c.QueryParam("q")
	if q == "" {
		return c.JSON(http.StatusOK, []any{})
	}
	objects, err := s.memory.SearchObjectsFTS(ctx, q, c.QueryParam("type"))
	if err != nil {
		captureError(err)
		return c.JSON(http.StatusOK, []any{})
	}
	type objectSearchResult struct {
		ID    string `json:"id"`
		Label string `json:"label"`
		Type  string `json:"type"`
	}
	out := make([]objectSearchResult, 0, len(objects))
	for _, o := range objects {
		out = append(out, objectSearchResult{ID: o.ID, Label: graphObjectLabel(o), Type: o.Type})
	}
	return c.JSON(http.StatusOK, out)
}

// objectCreateForm carries create-form state for rendering and re-render on error.
type objectCreateForm struct {
	Type   string
	Key    string
	Status string
	Labels []string
	Props  map[string][]string // prop name -> raw submitted values (indexed for multi-value TagLists)
	// JSONProps marks the properties the form submitted as one JSON document
	// (the jsonTextarea widget) rather than as chips or a scalar, so a rejected
	// save re-renders the same editor with the same text.
	JSONProps map[string]bool
	Error     string
}

// knownObjectType reports whether name is a compiled object type.
func knownObjectType(objectTypes []CompiledType, name string) bool {
	for _, t := range objectTypes {
		if t.Name == name {
			return true
		}
	}
	return false
}

// objectTypeDisplayName renders a compiled object type's option label,
// falling back to its raw name when no human label is configured.
func objectTypeDisplayName(t CompiledType) string {
	if t.Label != "" {
		return t.Label
	}
	return t.Name
}

// objectPropertyDef is one schema property of an object type, with the schema
// metadata needed to render a type-appropriate input. Type drives the default
// widget; Widget is an explicit schema hint ("textarea" for free-form long
// text, "input" to force a single line) that overrides the default only for
// string-ish properties; Enum, when declared, renders a native select of the
// allowed values. Description is the schema's own prose for the property and is
// used by the read-only object preview, not by the input widgets. The schema
// carries these losslessly from the pack through the compiled-types view.
type objectPropertyDef struct {
	Name        string
	Title       string
	Type        string
	Widget      string
	Enum        []string
	Description string
	// ItemType is the declared element type of an `array` property
	// (`items.type` in the schema, e.g. "string" or "object"), or "" when the
	// schema declares no element schema. A non-empty value is a contract the
	// server enforces, so the form submits chip text and lets the server coerce
	// it rather than guessing with JSON parsing; a declared object/array element
	// forces the JSON editor because a chip carries text only.
	ItemType string
}

// Label is the property's display name: the schema's declared title (JSON-Schema
// `title`), falling back to the machine key when none is set.
func (d objectPropertyDef) Label() string {
	if d.Title != "" {
		return d.Title
	}
	return d.Name
}

// KeyHint is the machine key rendered as secondary hint text under the field, or
// "" when the label already is the key (nothing extra to show).
func (d objectPropertyDef) KeyHint() string {
	if d.Title != "" && !strings.EqualFold(d.Title, d.Name) {
		return d.Name
	}
	return ""
}

// objectTypePropertyRow is one editable field of the object detail page's schema
// card: the compiled definition when the type declares the key (Declared true),
// the values the object stores, and the label/hint the field renders with.
type objectTypePropertyRow struct {
	Def      objectPropertyDef
	Name     string
	Label    string
	Hint     string
	Values   []string
	Stored   any
	Declared bool
}

// objectTypePropertyHint is the single muted hint line under a schema field: the
// machine key when the label is an authored title — property-labels requires the
// name to stay visible — otherwise the schema's own description, the same prose
// the preview drawer shows under its row label. Never both: one line competes
// with the field, not with itself.
func objectTypePropertyHint(def objectPropertyDef) string {
	if hint := def.KeyHint(); hint != "" {
		return hint
	}
	return def.Description
}

// objectTypePropertyRows merges the type's declared properties with the stored
// properties the type does not declare, so a declared property with no value yet
// is still editable (the create form renders those too) and an agent-written key
// is never hidden. Declared properties keep the schema's order; undeclared ones
// follow in key order, humanised with the raw key as their hint — the preview
// drawer's rule (objectPreviewTypeFields), applied to editable fields.
func objectTypePropertyRows(o GraphObject, propDefs []objectPropertyDef) []objectTypePropertyRow {
	declared := make(map[string]bool, len(propDefs))
	out := make([]objectTypePropertyRow, 0, len(propDefs)+len(o.Properties))
	for _, def := range propDefs {
		if def.Name == "" {
			continue
		}
		declared[def.Name] = true
		out = append(out, objectTypePropertyRow{
			Def:      def,
			Name:     def.Name,
			Label:    def.Label(),
			Hint:     objectTypePropertyHint(def),
			Values:   propValues(o.Properties[def.Name]),
			Stored:   o.Properties[def.Name],
			Declared: true,
		})
	}
	for _, p := range graphObjectProperties(o) {
		if declared[p.Key] {
			continue
		}
		label := objectPreviewFieldLabel(p.Key)
		hint := ""
		if !strings.EqualFold(label, p.Key) {
			hint = p.Key
		}
		out = append(out, objectTypePropertyRow{
			Name:   p.Key,
			Label:  label,
			Hint:   hint,
			Values: []string{p.Value},
		})
	}
	return out
}

// propertyHashLengths maps each hex-digest property type to its digest length in
// characters, mirroring the server's accepted vocabulary
// (apps/server/domain/graph/validation.go). Unknown types return 0.
var propertyHashLengths = map[string]int{
	"md5":    32,
	"sha1":   40,
	"sha256": 64,
	"sha512": 128,
}

// isHashPropertyType reports whether t names a hex-digest property type.
func isHashPropertyType(t string) bool {
	_, ok := propertyHashLengths[t]
	return ok
}

// hashInputPattern is the HTML input pattern for a hash property type: exactly
// its digest length of hex characters. Empty for a non-hash type.
func hashInputPattern(t string) string {
	n := propertyHashLengths[t]
	if n == 0 {
		return ""
	}
	return "[0-9a-fA-F]{" + strconv.Itoa(n) + "}"
}

// hashInputMaxLength is the maxlength attribute value for a hash property type,
// i.e. its digest length. Empty for a non-hash type.
func hashInputMaxLength(t string) string {
	n := propertyHashLengths[t]
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n)
}

// hashInputHint is the placeholder for a hash property type, e.g. "SHA256 · 64
// hex characters". Empty for a non-hash type.
func hashInputHint(t string) string {
	n := propertyHashLengths[t]
	if n == 0 {
		return ""
	}
	return strings.ToUpper(t) + " · " + strconv.Itoa(n) + " hex characters"
}

// indexedFormValues collects base[0], base[1], … from the submitted form
// values in ascending index order — the submission format of form.TagList's
// hidden inputs — stopping at the first missing index. Returns nil when no
// indexed field is present.
func indexedFormValues(v url.Values, base string) []string {
	var out []string
	for i := 0; ; i++ {
		vs := v[base+"["+strconv.Itoa(i)+"]"]
		if len(vs) == 0 {
			break
		}
		out = append(out, vs[len(vs)-1])
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// parseFormJSON parses one submitted form value as JSON, falling back to the
// raw string so an agent-written plain value keeps working. It is the single
// rule for every form field that may carry a typed value: a JSON literal is
// stored as its typed form, anything else as text.
func parseFormJSON(raw string) any {
	var parsed any
	if json.Unmarshal([]byte(raw), &parsed) == nil {
		return parsed
	}
	return raw
}

// objectPropertyType returns the declared schema type for a property within
// the given compiled property defs, or "" when the property is unknown
// (treated as a plain string).
func objectPropertyType(defs []objectPropertyDef, name string) string {
	for _, d := range defs {
		if d.Name == name {
			return d.Type
		}
	}
	return ""
}

// objectPropertyItemType returns an array property's declared element type
// (`items.type`), or "" when the property is unknown or its schema declares no
// element schema. A declared type is a contract the server enforces, so a chip
// submission carries the chip's text and the server coerces it — the form does
// not have to guess from the text.
func objectPropertyItemType(defs []objectPropertyDef, name string) string {
	for _, d := range defs {
		if d.Name == name {
			return d.ItemType
		}
	}
	return ""
}

// compiledTypePropertyDefs returns the schema properties for the named object
// type (skipping underscore-prefixed internal properties), sorted by name.
// Each property's declared type, widget hint, and enum are read from its raw
// JSON schema ({"type":"...","widget":"...","enum":[...]}); empty Type means
// string/unrecognised. Shadowed (losing) duplicates are skipped, so the
// properties always belong to the declaration the rest of the page shows (the
// status field and the preview panel title already resolve that way). Returns
// nil when the type is unknown or has no parseable properties.
func compiledTypePropertyDefs(objectTypes []CompiledType, typeName string) []objectPropertyDef {
	var props json.RawMessage
	for _, t := range visibleCompiledTypes(objectTypes) {
		if t.Name == typeName {
			props = t.Properties
			break
		}
	}
	if len(props) == 0 {
		return nil
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(props, &m) != nil {
		return nil
	}
	out := make([]objectPropertyDef, 0, len(m))
	for k, raw := range m {
		if strings.HasPrefix(k, "_") {
			continue
		}
		def := objectPropertyDef{Name: k}
		var meta struct {
			Type        string          `json:"type"`
			Title       string          `json:"title"`
			Widget      string          `json:"widget"`
			Enum        []string        `json:"enum"`
			Description string          `json:"description"`
			Items       json.RawMessage `json:"items"`
		}
		if json.Unmarshal(raw, &meta) == nil {
			def.Type = meta.Type
			def.Title = meta.Title
			def.Widget = meta.Widget
			def.Enum = meta.Enum
			def.Description = meta.Description
			// Decode the element type on its own: JSON Schema also allows a
			// non-object items (a tuple array, or true/false), and failing the
			// whole metadata decode over one would drop the property's type,
			// label and widget.
			if len(meta.Items) > 0 {
				var item struct {
					Type string `json:"type"`
				}
				if json.Unmarshal(meta.Items, &item) == nil {
					def.ItemType = item.Type
				}
			}
		}
		out = append(out, def)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// objectStatusControl is the built-in workflow-status field of the object
// create/edit forms. Show is false for a type that declares no workflow, in
// which case no status field is rendered at all; Options carries the statuses
// the type accepts (declared allowedStatuses, else the canonical board order);
// Free marks the degraded edit-form render where compiled types could not be
// loaded, so the stored value stays editable through a text input.
type objectStatusControl struct {
	Show    bool
	Free    bool
	Options []string
}

// objectStatusControlFor resolves the workflow-status control for an object
// type. A type declares a workflow when its registry row is board-enabled or
// lists allowedStatuses; only a declaring type gets the built-in field, and it
// is labelled "Workflow status". The canonical board order is the option set
// for a board-enabled type with no explicit allowedStatuses. A type with no
// declaration has no workflow status to set — its own `status` schema property
// (when it has one) is edited with the rest of its properties — so no field is
// rendered. An unknown type name resolves to no field: there is no declaration
// to honour.
func objectStatusControlFor(objectTypes []CompiledType, typeName string) *objectStatusControl {
	if typeName == "" {
		return &objectStatusControl{}
	}
	// Iterate the visible view so a shadowed (losing) declaration cannot leak
	// stale statuses for an overridden type — same rule as every other
	// compiled-type consumer.
	for _, t := range visibleCompiledTypes(objectTypes) {
		if t.Name != typeName {
			continue
		}
		if len(t.AllowedStatuses) > 0 {
			return &objectStatusControl{Show: true, Options: t.AllowedStatuses}
		}
		if t.BoardEnabled {
			return &objectStatusControl{Show: true, Options: boardStatusOrder}
		}
		return &objectStatusControl{}
	}
	return &objectStatusControl{}
}

// objectStatusControlUnavailable is the edit form's degraded control when the
// compiled types could not be loaded: the schema cannot say whether the type
// declares a workflow, so the stored status stays visible and editable as a
// free-form value rather than silently disappearing.
func objectStatusControlUnavailable() *objectStatusControl {
	return &objectStatusControl{Show: true, Free: true}
}

// uiObjectNew renders the create-object form, driven by the project's compiled
// object types. Selecting a type (via ?type=) reveals that type's property inputs.
func (s *Server) uiObjectNew(c echo.Context) error {
	ctx := c.Request().Context()
	compiled, err := s.memory.GetCompiledTypes(ctx)
	if err != nil {
		return s.page(c, pageTitle("New object"), ObjectCreatePage(nil, nil, objectCreateForm{}, nil, userFacingError(err), nil))
	}
	var objectTypes []CompiledType
	if compiled != nil {
		objectTypes = visibleCompiledTypes(compiled.ObjectTypes)
	}
	selectedType := c.QueryParam("type")
	propDefs := compiledTypePropertyDefs(objectTypes, selectedType)
	return s.page(c, pageTitle("New object"), ObjectCreatePage(objectTypes, propDefs, objectCreateForm{Type: selectedType}, s.objectLabelSuggestions(ctx), nil, objectStatusControlFor(objectTypes, selectedType)))
}

// uiObjectCreate creates an object from the create form and redirects to the
// new object's detail view. On validation or create failure it re-renders the
// form with an error and the submitted values preserved.
func (s *Server) uiObjectCreate(c echo.Context) error {
	ctx := c.Request().Context()
	if err := c.Request().ParseForm(); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid form submission")
	}
	form := objectCreateForm{
		Type:      c.FormValue("type"),
		Key:       c.FormValue("key"),
		Status:    c.FormValue("status"),
		Labels:    indexedFormValues(c.Request().PostForm, "labels"),
		Props:     map[string][]string{},
		JSONProps: map[string]bool{},
	}
	for field := range c.Request().PostForm {
		if name, ok := strings.CutPrefix(field, jsonPropMarkerPrefix); ok && name != "" {
			form.JSONProps[name] = true
		}
	}
	for field, vals := range c.Request().PostForm {
		name, ok := strings.CutPrefix(field, "prop_")
		if !ok || name == "" {
			continue
		}
		if i := strings.IndexByte(name, '['); i >= 0 {
			name = name[:i] // TagList field prop_x[0] → x
		}
		if _, seen := form.Props[name]; seen {
			continue
		}
		// TagList submits prop_x[0], prop_x[1], … — collect all in index order.
		if indexed := indexedFormValues(c.Request().PostForm, "prop_"+name); indexed != nil {
			form.Props[name] = indexed
			continue
		}
		// Scalar field: the last submitted value wins (a boolean property
		// submits its hidden "false" then, when checked, the toggle's "true").
		last := ""
		for _, v := range vals {
			if v != "" {
				last = v
			}
		}
		if last != "" {
			form.Props[name] = []string{last}
		}
	}

	compiled, cerr := s.memory.GetCompiledTypes(ctx)
	if cerr != nil {
		captureError(cerr)
	}
	var objectTypes []CompiledType
	if compiled != nil {
		objectTypes = visibleCompiledTypes(compiled.ObjectTypes)
	}
	suggestions := s.objectLabelSuggestions(ctx)

	render := func(errMsg string) error {
		form.Error = errMsg
		return s.page(c, pageTitle("New object"), ObjectCreatePage(objectTypes, compiledTypePropertyDefs(objectTypes, form.Type), form, suggestions, nil, objectStatusControlFor(objectTypes, form.Type)))
	}

	if form.Type == "" {
		return render("Select an object type.")
	}
	if !knownObjectType(objectTypes, form.Type) {
		return render("Unknown object type.")
	}

	req := &CreateObjectRequest{Type: form.Type}
	if form.Key != "" {
		req.Key = &form.Key
	}
	if form.Status != "" {
		req.Status = &form.Status
	}
	req.Labels = form.Labels
	props := map[string]any{}
	defs := compiledTypePropertyDefs(objectTypes, form.Type)
	for name, vals := range form.Props {
		switch objectPropertyType(defs, name) {
		case "object":
			// An object property is one JSON document: parse it whole so the
			// server receives a map. Wrapping the text in an array (the old
			// chip behaviour) is rejected by validation as
			// "expected object, got []interface {}".
			if len(vals) == 0 || vals[0] == "" {
				continue
			}
			doc := parseFormJSON(vals[0])
			if doc == nil {
				return render(name + ": null is not a value — clear the field to leave the property unset")
			}
			props[name] = doc
			continue
		case "array":
			// Two shapes reach here: the JSON text field submits the whole array
			// as one unindexed prop_<name> document, and the chip editor submits
			// one prop_<name>[i] value per chip. The document is sent with the
			// shape it was parsed into — never wrapped in a one-element list —
			// so a document that is not a JSON array (or text that is not JSON)
			// fails the server's array validation instead of being stored as a
			// list that silently changed its elements.
			if indexedFormValues(c.Request().PostForm, "prop_"+name) == nil {
				if len(vals) == 0 || vals[0] == "" {
					continue
				}
				doc := parseFormJSON(vals[0])
				if doc == nil {
					return render(name + ": null is not a value — clear the field to leave the property unset")
				}
				props[name] = doc
				continue
			}
			itemType := objectPropertyItemType(defs, name)
			var items []any
			for _, raw := range vals {
				if raw == "" {
					continue
				}
				if itemType != "" {
					// The schema declares the element type, so the chip's text is
					// the value: submit it verbatim and let the server coerce it.
					// JSON-parsing here would turn a string array's "123"/"null"
					// chips into a number/null, which the server then rejects as
					// the wrong element type (or stores under the wrong type).
					items = append(items, raw)
					continue
				}
				item := parseFormJSON(raw)
				if item == nil {
					// Same rule as a scalar: nil is the delete sentinel, so a chip
					// reading "null" would store null where the user meant a value.
					return render(name + ": null is not a value — clear the field to leave the property unset, or quote it to store the text")
				}
				items = append(items, item)
			}
			if len(items) > 0 {
				props[name] = items
			}
			continue
		}
		// Scalar: keep the existing JSON-parse-or-string behaviour on the value.
		if len(vals) == 0 || vals[0] == "" {
			continue
		}
		parsed := parseFormJSON(vals[0])
		if parsed == nil {
			return render(name + ": null is not a value — clear the field to leave the property unset")
		}
		props[name] = parsed
	}
	if len(props) > 0 {
		req.Properties = props
	}

	created, err := s.memory.CreateObject(ctx, req)
	if err != nil {
		captureError(err)
		// A 4xx carries the backend's validation copy (for example which
		// property and array element failed); echo it so the user can correct
		// the form. Transport failures and 5xx responses stay on the generic
		// line, which never leaks backend detail (see userFacingMemoryError).
		if status := memoryStatus(err); status >= http.StatusBadRequest && status < http.StatusInternalServerError {
			if msg := memoryHTTPErrorMessage(err); msg != "" {
				return render(msg)
			}
		}
		return render("Could not create the object.")
	}
	s.invalidateObjectLabelSuggestions(ctx)
	return c.Redirect(http.StatusSeeOther, "/objects/"+url.PathEscape(created.ID))
}

// uiObjectChat starts (or resumes) the refinement conversation linked to an
// object: it get-or-creates the canonical conversation seeded with the object
// instruction, then deep-links into /chat with the editor agent preselected.
// When no editor agent resolves, it redirects back with an ?err= flash telling
// the user to create an agent first — never a silent no-op.
func (s *Server) uiObjectChat(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	obj, err := s.memory.GetGraphObject(ctx, id)
	if err != nil {
		return c.Redirect(http.StatusSeeOther, "/objects/"+url.PathEscape(id))
	}
	agentID := s.resolveEditorAgentID(ctx)
	if agentID == "" {
		return redirectWithError(c, "/objects/"+url.PathEscape(id), fmt.Errorf("no editor-capable agent — in Project settings pick an agent with %s", editorCapabilityRequirement))
	}
	convID, err := s.memory.CreateObjectConversation(ctx, obj.CanonicalID, graphObjectLabel(*obj), objectChatInstruction(obj))
	if err != nil {
		return c.Redirect(http.StatusSeeOther, "/objects/"+url.PathEscape(id))
	}
	return c.Redirect(http.StatusSeeOther, "/chat?c="+url.QueryEscape(convID)+"&agent="+url.QueryEscape(agentID))
}

// uiObjectMerge starts a merge session: redirects to /chat with the merge
// instruction pre-filled and the merge agent preselected.
func (s *Server) uiObjectMerge(c echo.Context) error {
	srcID := c.Param("id")
	dstID := c.QueryParam("with")
	if dstID == "" {
		return c.Redirect(http.StatusSeeOther, "/objects/"+srcID)
	}
	ctx := c.Request().Context()
	src, err := s.memory.GetGraphObject(ctx, srcID)
	if err != nil {
		return c.Redirect(http.StatusSeeOther, "/objects/"+srcID)
	}
	dst, err := s.memory.GetGraphObject(ctx, dstID)
	if err != nil {
		return c.Redirect(http.StatusSeeOther, "/objects/"+srcID)
	}
	agentID := s.resolveMergeAgentID(ctx)
	if agentID == "" {
		return redirectWithError(c, "/objects/"+url.PathEscape(srcID), fmt.Errorf("no merge-capable agent — in Project settings pick an agent with %s", mergeCapabilityRequirement))
	}
	msg := mergeInstruction(src, dst)
	return c.Redirect(http.StatusSeeOther, "/chat?agent="+url.QueryEscape(agentID)+"&prompt="+url.QueryEscape(msg))
}

// defaultAgentID picks the generic default agent: the default agent
// (IsDefault), else the first enabled agent, else the first agent. Empty on
// error/none. Callers that need a specific capability (e.g. the merge flow)
// must filter on top of this.
func (s *Server) defaultAgentID(ctx context.Context) string {
	agents, err := s.memory.ListAgentDefinitions(ctx)
	if err != nil {
		return ""
	}
	for _, a := range agents {
		if a.IsDefault {
			return a.ID
		}
	}
	for _, a := range agents {
		if a.Enabled {
			return a.ID
		}
	}
	if len(agents) > 0 {
		return agents[0].ID
	}
	return ""
}

// resolveEditorAgentID returns the agent to run object-content edits. The flow
// needs the editor tool set (editorAgentEligibility), so an arbitrary agent is
// not a usable answer.
func (s *Server) resolveEditorAgentID(ctx context.Context) string {
	return s.resolveFlowAgentID(ctx, settingsEditorCategory, settingsEditorKey, editorRequirement)
}

// resolveMergeAgentID returns the agent to run a merge. It needs the merge tool
// set (mergeAgentEligibility).
func (s *Server) resolveMergeAgentID(ctx context.Context) string {
	return s.resolveFlowAgentID(ctx, settingsMergeCategory, settingsMergeKey, mergeRequirement)
}

// resolveFlowAgentID returns the agent to run an agent-driven flow, where the
// agent must grant that flow's tool set. Resolution order:
//
//  1. the agent stored under category/key, when it is still capable
//  2. the default agent, when it is capable
//  3. the first enabled capable agent
//  4. the first capable agent
//
// Empty when no agent qualifies, which makes the caller surface an actionable
// configuration error instead of starting a run the agent cannot finish.
func (s *Server) resolveFlowAgentID(ctx context.Context, category, key string, req toolRequirement) string {
	cache := map[string]bool{}
	capable := func(id string) bool {
		if id == "" {
			return false
		}
		if v, ok := cache[id]; ok {
			return v
		}
		def, err := s.memory.GetAgentDefinition(ctx, id)
		ok := err == nil && def != nil
		if ok {
			missing, _ := agentToolEligibility(def, req)
			ok = len(missing) == 0
		}
		cache[id] = ok
		return ok
	}

	if ps, perr := s.memory.GetProjectSetting(ctx, category, key); perr == nil && ps != nil {
		if id, ok := ps.Value["agentId"].(string); ok && capable(id) {
			return id
		}
	}
	agents, err := s.memory.ListAgentDefinitions(ctx)
	if err != nil {
		return ""
	}
	for _, a := range agents {
		if a.IsDefault && capable(a.ID) {
			return a.ID
		}
	}
	for _, a := range agents {
		if a.Enabled && capable(a.ID) {
			return a.ID
		}
	}
	for _, a := range agents {
		if capable(a.ID) {
			return a.ID
		}
	}
	return ""
}

// similarObjectLabel renders the display label for a similar-object row: the
// object's key, else a name/title/summary/content property, else type + short id.
func similarObjectLabel(so SimilarObject) string {
	return graphObjectLabel(GraphObject{ID: so.ID, Type: so.Type, Key: so.Key, Properties: so.Properties})
}

// similarMatchThresholdPercent is the minimum match percentage (0..100) a
// similar object must exceed to be surfaced. 80 = "over 80% match".
const similarMatchThresholdPercent = 80

// similarMatchPercent converts a pgvector cosine distance (0..2, lower = more
// similar) into a match percentage (0..100), clamped. Higher = more similar.
func similarMatchPercent(d float32) int {
	s := 1 - float64(d)
	if s < 0 {
		s = 0
	}
	if s > 1 {
		s = 1
	}
	return int(math.Round(s * 100))
}

// filterSimilarMatches keeps only similar objects whose match percentage
// exceeds the threshold, preserving order.
func filterSimilarMatches(similar []SimilarObject) []SimilarObject {
	out := make([]SimilarObject, 0, len(similar))
	for _, so := range similar {
		if similarMatchPercent(so.Distance) > similarMatchThresholdPercent {
			out = append(out, so)
		}
	}
	return out
}

// similarityLabel converts a pgvector cosine distance (0..2, lower = more
// similar) into a match percentage label, e.g. "83% match".
func similarityLabel(d float32) string {
	return strconv.Itoa(similarMatchPercent(d)) + "% match"
}

// mergeInstruction builds the first message for a merge session: it names both
// objects (label, id, type, properties) and asks the agent to propose a merged
// shape before applying the merge with its memory tools.
func mergeInstruction(src, dst *GraphObject) string {
	var b strings.Builder
	writeObject := func(o *GraphObject) {
		fmt.Fprintf(&b, "- Label: %s\n", graphObjectLabel(*o))
		fmt.Fprintf(&b, "- ID: %s\n", o.ID)
		if o.Type != "" {
			fmt.Fprintf(&b, "- Type: %s\n", o.Type)
		}
		if o.Status != "" {
			fmt.Fprintf(&b, "- Status: %s\n", o.Status)
		}
		if len(o.Properties) > 0 {
			b.WriteString("- Properties:\n")
			for _, p := range graphObjectProperties(*o) {
				fmt.Fprintf(&b, "  %s: %s\n", p.Key, p.Value)
			}
		}
	}
	b.WriteString("Merge the following two knowledge-graph objects into one.\n\n")
	b.WriteString("Object A (source):\n")
	writeObject(src)
	b.WriteString("\nObject B (target):\n")
	writeObject(dst)
	b.WriteString("\nSteps:\n")
	b.WriteString("1. FIRST propose the merged shape: a single type, a single key, merged properties (reconciling conflicting values), and which relationships to keep or rewire.\n")
	b.WriteString("2. THEN apply the merge using your memory tools: create/update the merged object and retire the source so only one object remains.\n")
	b.WriteString("3. Rewire the source's remaining relationships onto the merged object (read them with entity-edges-get, then relationship-create). If you have relationship-delete, remove the stale edges left behind.\n")
	return b.String()
}

// objectChatInstruction builds the first message for an object-chat session:
// it names the object (label, id, type, status, properties) and asks the agent
// to discuss/edit/refine the object's content.
func objectChatInstruction(obj *GraphObject) string {
	var b strings.Builder
	writeObject := func(o *GraphObject) {
		fmt.Fprintf(&b, "- Label: %s\n", graphObjectLabel(*o))
		fmt.Fprintf(&b, "- ID: %s\n", o.ID)
		if o.Type != "" {
			fmt.Fprintf(&b, "- Type: %s\n", o.Type)
		}
		if o.Status != "" {
			fmt.Fprintf(&b, "- Status: %s\n", o.Status)
		}
		if len(o.Properties) > 0 {
			b.WriteString("- Properties:\n")
			for _, p := range graphObjectProperties(*o) {
				fmt.Fprintf(&b, "  %s: %s\n", p.Key, p.Value)
			}
		}
	}
	b.WriteString("Help me work with this knowledge-graph object.\n\n")
	b.WriteString("Object:\n")
	writeObject(obj)
	b.WriteString("\nSteps:\n")
	b.WriteString("1. FIRST review the object and propose any edits: corrected or added properties, a better key or type, reworded content, or relationships worth creating.\n")
	b.WriteString("2. THEN apply the edits with your memory tools once I confirm: create or update the object as needed, and create relationships. Search for related objects first when your tools allow it.\n")
	return b.String()
}

// loadRelatedObjects fetches the objects referenced by the given edges so
// relationship rows can resolve src/dst labels.
func (s *Server) loadRelatedObjects(ctx context.Context, obj *GraphObject, edges []GraphRelationship) []GraphObject {
	idSet := map[string]bool{}
	for _, r := range edges {
		for _, id := range []string{r.SrcID, r.DstID} {
			if id != "" && id != obj.ID && id != obj.CanonicalID {
				idSet[id] = true
			}
		}
	}
	ids := make([]string, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil
	}
	objects, err := s.memory.ListGraphObjects(ctx, obj.BranchID, "", ids)
	captureError(err)
	return objects
}

// distinctObjectTypes returns the sorted distinct types of the given objects.
func distinctObjectTypes(objects []GraphObject) []string {
	seen := map[string]bool{}
	var out []string
	for _, o := range objects {
		if o.Type != "" && !seen[o.Type] {
			seen[o.Type] = true
			out = append(out, o.Type)
		}
	}
	sort.Strings(out)
	return out
}

// distinctObjectLabels returns the sorted, deduped, non-empty labels across
// the given objects (used to autocomplete the create form's label TagList).
func distinctObjectLabels(objects []GraphObject) []string {
	seen := map[string]bool{}
	var out []string
	for _, o := range objects {
		for _, l := range o.Labels {
			if l != "" && !seen[l] {
				seen[l] = true
				out = append(out, l)
			}
		}
	}
	sort.Strings(out)
	return out
}

// objectLabelSuggestions returns the project's existing distinct object labels
// for the create form's label autocomplete — best-effort: an error or an empty
// graph yields nil (the input then simply has no suggestions).
const labelSuggestionsTTL = time.Minute

type labelSuggestionsEntry struct {
	labels    []string
	expiresAt time.Time
}

// labelSuggestionsProjectID resolves the cache key the same way
// MemoryClient.projectIDFor scopes requests: the session's active project, else
// the static server project id (dev/API-key mode has no session context but
// still targets one project).
func (s *Server) labelSuggestionsProjectID(ctx context.Context) string {
	pid := s.cfg.MemoryProjectID
	if sc, ok := sessionContextFrom(ctx); ok && sc.ProjectID != "" {
		pid = sc.ProjectID
	}
	return pid
}

func (s *Server) objectLabelSuggestions(ctx context.Context) []string {
	pid := s.labelSuggestionsProjectID(ctx)
	if pid == "" {
		// No project scope at all: nothing to key or cache on.
		objects, err := s.memory.ListGraphObjects(ctx, "", "", nil)
		if err != nil {
			captureError(err)
			return nil
		}
		return distinctObjectLabels(objects)
	}

	// Fast path: a fresh cached entry skips the list query entirely.
	s.labelCacheMu.Lock()
	if e, ok := s.labelCache[pid]; ok && time.Now().Before(e.expiresAt) {
		s.labelCacheMu.Unlock()
		return e.labels
	}
	// Evict expired entries so a long-lived gateway serving many projects
	// doesn't retain one labels slice per project forever (the TTL bounds
	// staleness, not memory).
	s.deleteExpiredLabelSuggestionsLocked(time.Now())
	s.labelCacheMu.Unlock()

	// Coalesce concurrent cold/expired misses per project so a burst of requests
	// triggers exactly one ListGraphObjects instead of a stampede.
	v, err, _ := s.labelSuggestionsGroup.Do(pid, func() (any, error) {
		// Another request may have populated the entry while we waited on the
		// flight — re-check before querying.
		s.labelCacheMu.Lock()
		if e, ok := s.labelCache[pid]; ok && time.Now().Before(e.expiresAt) {
			s.labelCacheMu.Unlock()
			return e.labels, nil
		}
		s.labelCacheMu.Unlock()

		objects, err := s.memory.ListGraphObjects(ctx, "", "", nil)
		if err != nil {
			captureError(err)
			return nil, err
		}
		labels := distinctObjectLabels(objects)
		s.labelCacheMu.Lock()
		if s.labelCache == nil {
			s.labelCache = make(map[string]labelSuggestionsEntry)
		}
		s.labelCache[pid] = labelSuggestionsEntry{labels: labels, expiresAt: time.Now().Add(labelSuggestionsTTL)}
		s.labelCacheMu.Unlock()
		return labels, nil
	})
	if err != nil {
		return nil
	}
	return v.([]string)
}

// deleteExpiredLabelSuggestionsLocked removes label-suggestion entries whose TTL
// has elapsed. Caller must hold labelCacheMu.
func (s *Server) deleteExpiredLabelSuggestionsLocked(now time.Time) {
	for pid, e := range s.labelCache {
		if !now.Before(e.expiresAt) {
			delete(s.labelCache, pid)
		}
	}
}

// invalidateObjectLabelSuggestions drops the cached label list for the active
// project and forgets any in-flight coalesced fill so a freshly created or
// updated object's new labels appear in the create form's suggestion dropdown
// immediately instead of up to labelSuggestionsTTL later (issue #1504).
func (s *Server) invalidateObjectLabelSuggestions(ctx context.Context) {
	pid := s.labelSuggestionsProjectID(ctx)
	if pid == "" {
		return
	}
	s.labelCacheMu.Lock()
	delete(s.labelCache, pid)
	s.labelCacheMu.Unlock()
	s.labelSuggestionsGroup.Forget(pid)
}

// objectEndpointLabel resolves one relationship endpoint id to a display
// label: the object itself, a related object, or a shortened id.
func objectEndpointLabel(obj *GraphObject, related []GraphObject, id string) string {
	if obj != nil && (id == obj.ID || id == obj.CanonicalID) {
		return graphObjectLabel(*obj)
	}
	return graphObjectLabelByID(related, id)
}

// objectSource is one extraction provenance record attached to an object: which
// document/version/job extracted it, and when. DocumentName is the resolved
// document filename for display ("" until resolveObjectSourceNames fills it; the
// card then falls back to the short id).
type objectSource struct {
	DocumentID      string
	DocumentName    string
	DocumentVersion string
	JobID           string
	ExtractedAt     string
}

// objectSources parses an object's extraction provenance. The modern shape is
// `_sources` (an array of snake_case records); legacy objects carry the single
// `_source_id` / `_extraction_job_id` / `_document_version` scalar props. It
// tolerates absent fields and non-conforming values, returning nil when there
// is nothing to show.
func objectSources(o GraphObject) []objectSource {
	raw, ok := o.Properties["_sources"]
	if !ok {
		legacy := objectSource{
			DocumentID:      strAny(o.Properties["_source_id"]),
			JobID:           strAny(o.Properties["_extraction_job_id"]),
			DocumentVersion: strAny(o.Properties["_document_version"]),
		}
		if legacy.DocumentID == "" && legacy.JobID == "" && legacy.DocumentVersion == "" {
			return nil
		}
		return []objectSource{legacy}
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]objectSource, 0, len(arr))
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		src := objectSource{
			DocumentID:      strAny(m["document_id"]),
			DocumentVersion: strAny(m["document_version"]),
			JobID:           strAny(m["job_id"]),
			ExtractedAt:     strAny(m["extracted_at"]),
		}
		if src.DocumentID == "" && src.DocumentVersion == "" && src.JobID == "" && src.ExtractedAt == "" {
			continue
		}
		out = append(out, src)
	}
	return out
}

// objectSourceDocLookupCap bounds how many distinct GetDocument calls the source
// card may make per render, so a pathological _sources array cannot trigger
// unbounded upstream fan-out.
const objectSourceDocLookupCap = 10

// resolveObjectSourceNames fills each source's DocumentName via GetDocument,
// cached by id so a shared document is fetched once. Lookups are capped and
// best-effort: a failure or a missing document leaves the name empty, and the
// source card falls back to the short id.
func (s *Server) resolveObjectSourceNames(ctx context.Context, sources []objectSource) {
	cache := make(map[string]string, len(sources))
	lookups := 0
	for i := range sources {
		id := sources[i].DocumentID
		if id == "" {
			continue
		}
		if name, ok := cache[id]; ok {
			sources[i].DocumentName = name
			continue
		}
		if lookups >= objectSourceDocLookupCap {
			continue
		}
		lookups++
		doc, err := s.memory.GetDocument(ctx, id)
		if err != nil {
			captureError(err)
			cache[id] = ""
			continue
		}
		if doc == nil {
			cache[id] = ""
			continue
		}
		cache[id] = doc.Filename
		sources[i].DocumentName = doc.Filename
	}
}

// objectBranchRef is the resolved branch context for an object's detail header
// chip: the branch id ("" = implicit main), its display name when the id
// resolves, and its merged-at time when the branch has been merged into main.
type objectBranchRef struct {
	ID       string
	Name     string
	MergedAt string
}

// resolveObjectBranch resolves an object's branch id to its chip context. An
// empty id is the implicit main branch; a resolved Branch supplies its name and
// merged-at time; an id that fails to resolve still yields a chip labelled by
// its short id, never an empty chip or a page failure.
func resolveObjectBranch(branches []Branch, id string) objectBranchRef {
	if id == "" {
		return objectBranchRef{}
	}
	for _, b := range branches {
		if b.ID == id {
			return objectBranchRef{ID: id, Name: b.Name, MergedAt: b.MergedAt}
		}
	}
	return objectBranchRef{ID: id}
}

// objectBranchChipLabel renders the branch chip's label: "main" for the
// implicit main graph, the branch name for an open branch, "<name> · merged
// <reltime>" for a merged branch, and the short id when the id fails to resolve.
func objectBranchChipLabel(b objectBranchRef) string {
	if b.ID == "" {
		return "main"
	}
	name := b.Name
	if name == "" {
		name = shortID(b.ID)
	}
	if b.MergedAt != "" {
		return name + " · merged " + relTime(b.MergedAt)
	}
	return name
}

// objectBranchChipIcon returns the chip's leading icon: a git-branch glyph for a
// named branch, none for the implicit main graph.
func objectBranchChipIcon(b objectBranchRef) string {
	if b.ID == "" {
		return ""
	}
	return "lucide--git-branch"
}

// objectBranchChipHref returns the chip's link target (the object browser
// filtered to this branch), or "" for the main graph so the chip renders as a
// plain badge.
func objectBranchChipHref(b objectBranchRef) string {
	if b.ID == "" {
		return ""
	}
	return "/objects?branch=" + url.QueryEscape(b.ID)
}

// objectSourceDocumentLabel renders a source record's document display label:
// the resolved filename when available, else the short id.
func objectSourceDocumentLabel(src objectSource) string {
	if src.DocumentName != "" {
		return src.DocumentName
	}
	return shortID(src.DocumentID)
}

// propertyKV is one key/value pair of an object's properties for display.
type propertyKV struct {
	Key   string
	Value string
}

// graphObjectProperties returns the object's display properties sorted by key,
// skipping internal (underscore-prefixed) and empty values.
func graphObjectProperties(o GraphObject) []propertyKV {
	keys := make([]string, 0, len(o.Properties))
	for k := range o.Properties {
		if strings.HasPrefix(k, "_") {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]propertyKV, 0, len(keys))
	for _, k := range keys {
		if v := strAny(o.Properties[k]); v != "" {
			out = append(out, propertyKV{Key: k, Value: v})
		}
	}
	return out
}

// propInputValue renders an object property value as form input text: strings
// pass through unchanged; non-scalar values (numbers, bools, arrays, objects)
// stringify as JSON so the update handler can parse them back to the same
// typed value.
func propInputValue(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

// jsonEditorElementLimit is the longest array element the chip editor may hold.
// A chip is a fixed-height, single-line badge: past this length (or as soon as
// a value carries a line break) the element cannot be read or edited as a chip,
// so the whole array is edited as JSON text instead.
const jsonEditorElementLimit = 100

// jsonPropMarkerPrefix names the hidden "submitted as JSON" field for a property.
// It shares no prefix with prop_* / propm_present_*, so the property parsers in
// uiObjectCreate and uiObjectUpdate ignore it.
const jsonPropMarkerPrefix = "jsonprop_"

// arrayNeedsJSONEditor reports whether an array property's displayed values must
// be edited as a JSON document rather than as one chip per value: any element
// that is multi-line or longer than jsonEditorElementLimit cannot be represented
// by a chip, and a chip holding it used to paint over the rest of the form.
func arrayNeedsJSONEditor(vals []string) bool {
	for _, v := range vals {
		if strings.ContainsAny(v, "\r\n") || utf8.RuneCountInString(v) > jsonEditorElementLimit {
			return true
		}
	}
	return false
}

// arrayStoredNeedsJSONEditor reports whether an array's stored value holds an
// element that is not plain text. A chip submits text, so a stored object, list,
// number, boolean or null edited through the chip editor would be written back
// as a string, silently changing the data (#2419); such an array is edited as
// JSON so its element types survive the round trip.
func arrayStoredNeedsJSONEditor(stored any) bool {
	switch t := stored.(type) {
	case []string:
		return false
	case []any:
		for _, e := range t {
			if _, ok := e.(string); !ok {
				return true
			}
		}
	}
	return false
}

// arrayUsesJSONEditor reports whether an array property must be edited as one
// JSON document instead of one chip per element. It always does when the form
// was re-rendered after a rejected JSON save, when an element is multi-line or
// longer than jsonEditorElementLimit, or when a stored element is not plain
// text. It also does when the schema declares an object or array element type:
// a chip holds text only, so it cannot represent either shape — not even for an
// empty array, where there is no stored value to inspect.
func arrayUsesJSONEditor(def objectPropertyDef, vals []string, stored any, submittedJSON bool) bool {
	if submittedJSON || arrayNeedsJSONEditor(vals) || arrayStoredNeedsJSONEditor(stored) {
		return true
	}
	switch def.ItemType {
	case "object", "array":
		return true
	}
	return false
}

// jsonDocumentEditorValue renders a stored object value as the JSON text the
// editor shows: pretty-printed when it parses, so a compact multi-KB snapshot is
// readable and editable, and passed through unchanged when it does not, so an
// agent-written plain string is never hidden or rewritten.
func jsonDocumentEditorValue(vals []string) string {
	if len(vals) == 0 || vals[0] == "" {
		return ""
	}
	var doc any
	if json.Unmarshal([]byte(vals[0]), &doc) == nil {
		if b, err := json.MarshalIndent(doc, "", "  "); err == nil {
			return string(b)
		}
	}
	return vals[0]
}

// jsonArrayEditorValue renders an array property's values as the one JSON array
// its editor shows. Unlike an object, an array is always submitted as a list —
// including when it holds a single element, which would otherwise be submitted
// as a bare document and rejected as "expected array, got string" (or a map).
// jsonPropMarker is the hidden field a jsonTextarea submits alongside its
// document, so the create form can re-render that property as a JSON editor
// (with the submitted text intact) instead of guessing from the value's shape.
func jsonPropMarker(propField string) string {
	return jsonPropMarkerPrefix + strings.TrimPrefix(propField, "prop_")
}

// declaresItems tells the builder the array's schema declares an element type,
// so the create form's chip text is the element value (the server coerces it)
// rather than a JSON literal to re-parse.
func jsonArrayEditorValue(vals []string, stored any, submittedJSON bool, declaresItems bool) string {
	// The create form has no stored value; if the property came back from a JSON
	// editor, its submitted text is the document — keep it verbatim so a
	// rejected save can be corrected rather than silently reshaped.
	if submittedJSON && stored == nil && len(vals) > 0 {
		return vals[0]
	}
	// With a stored value, render it: the stored types are exact, so a string
	// that looks like JSON stays quoted and a number stays a number.
	switch t := stored.(type) {
	case []any:
		if b, err := json.MarshalIndent(t, "", "  "); err == nil {
			return string(b)
		}
	case []string:
		items := make([]any, 0, len(t))
		for _, s := range t {
			items = append(items, s)
		}
		if b, err := json.MarshalIndent(items, "", "  "); err == nil {
			return string(b)
		}
	}
	// No stored value (the create form): rebuild the values. A declared element
	// type makes the chip text the value, so keep it as a JSON string — parsing
	// it would turn a string array's "123"/"null" chips into a number/null and
	// the retry would be rejected as the wrong element type. Without one, the
	// long-standing chip rule applies (a JSON literal becomes a typed value).
	items := make([]any, 0, len(vals))
	for _, raw := range vals {
		if raw == "" {
			continue
		}
		if declaresItems {
			items = append(items, raw)
			continue
		}
		items = append(items, parseFormJSON(raw))
	}
	if b, err := json.MarshalIndent(items, "", "  "); err == nil {
		return string(b)
	}
	return strings.Join(vals, "\n")
}

// dateInputValue normalizes a stored date value to yyyy-mm-dd for an
// <input type="date">. Stored values arrive as RFC3339 ("2019-03-07T00:00:00Z")
// or space-separated ("2019-03-08 14:30"); the browser rejects anything but
// yyyy-mm-dd and renders the field blank.
//
// Contract: when the value begins with a valid yyyy-mm-dd calendar date, return
// just that prefix. For anything empty, too short, unparseable, or an invalid
// calendar date (e.g. "2019-99-99"), return "" so we never hand the browser a
// value it will silently discard.
func dateInputValue(v string) string {
	if len(v) < len(time.DateOnly) {
		return ""
	}
	prefix := v[:len(time.DateOnly)]
	if _, err := time.Parse(time.DateOnly, prefix); err != nil {
		return ""
	}
	return prefix
}

// propValues converts a stored object property value to the []string form the
// propertyInput templ expects: scalar → single element, slice → one per element.
func propValues(v any) []string {
	if v == nil {
		return nil
	}
	switch t := v.(type) {
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			out = append(out, propInputValue(e))
		}
		return out
	case []string:
		return t
	default:
		return []string{propInputValue(v)}
	}
}

// charCountLabel renders the initial, server-side character count for a
// long-text field ("1,234 characters"). A browser normalizes a textarea's line
// endings to LF in its .value / on form submit, so CRLF and lone CR sequences in
// the raw stored string are normalized to LF before counting — otherwise the
// server-rendered count would be one too high per CR compared with the
// client-side counter. It counts code points and groups thousands.
func charCountLabel(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	n := utf8.RuneCountInString(s)
	noun := "characters"
	if n == 1 {
		noun = "character"
	}
	return groupDigits(int64(n)) + " " + noun
}

// groupDigits inserts thousands separators into a non-negative integer
// ("41594" -> "41,594") so long counts stay readable.
func groupDigits(n int64) string {
	digits := strconv.FormatInt(n, 10)
	if len(digits) <= 3 {
		return digits
	}
	var b strings.Builder
	lead := len(digits) % 3
	if lead > 0 {
		b.WriteString(digits[:lead])
	}
	for i := lead; i < len(digits); i += 3 {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(digits[i : i+3])
	}
	return b.String()
}

// relationshipOptions returns the compiled relationship types usable from an
// object of the given type: those whose source or target is that type,
// falling back to all types when objType is empty or nothing matches.
func relationshipOptions(relTypes []CompiledType, objType string) []CompiledType {
	relTypes = visibleCompiledTypes(relTypes)
	if objType == "" {
		return relTypes
	}
	var out []CompiledType
	for _, rt := range relTypes {
		if rt.SourceType == objType || rt.TargetType == objType {
			out = append(out, rt)
		}
	}
	if len(out) > 0 {
		return out
	}
	return relTypes
}

// relTypeDisplayName renders a compiled relationship type's option label,
// falling back to its raw name when no human label is configured.
func relTypeDisplayName(rt CompiledType) string {
	if rt.Label != "" {
		return rt.Label
	}
	return rt.Name
}
