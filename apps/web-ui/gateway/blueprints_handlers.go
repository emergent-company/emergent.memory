package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
)

// --- blueprint/schema handlers (JSON API) ---

func (s *Server) listInstalledBlueprints(c echo.Context) error {
	out, err := s.memory.ListAppliedBlueprints(c.Request().Context())
	if err != nil {
		return blueprintMemoryError(c, err)
	}
	return c.JSON(http.StatusOK, out)
}

func (s *Server) listAvailableBlueprints(c echo.Context) error {
	ctx := c.Request().Context()
	records, err := s.memory.ListBlueprints(ctx)
	if err != nil {
		return blueprintMemoryError(c, err)
	}
	applied, err := s.memory.ListAppliedBlueprints(ctx)
	captureError(err)
	return c.JSON(http.StatusOK, buildAvailableList(records, applied))
}

func (s *Server) getCompiledTypes(c echo.Context) error {
	out, err := s.memory.GetCompiledTypes(c.Request().Context())
	if err != nil {
		return blueprintMemoryError(c, err)
	}
	return c.JSON(http.StatusOK, out)
}

// installBlueprintRequest is the body for POST /api/blueprints/install. SchemaID
// is the id of the published blueprint to apply to the project.
type installBlueprintRequest struct {
	SchemaID string `json:"schemaId"`
}

func (s *Server) installBlueprint(c echo.Context) error {
	var in installBlueprintRequest
	if err := c.Bind(&in); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	if in.SchemaID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "schemaId required"})
	}
	res, err := s.memory.ApplyBlueprint(c.Request().Context(), in.SchemaID)
	if err != nil {
		return blueprintMemoryError(c, err)
	}
	return c.JSON(http.StatusCreated, res)
}

// listPrivateDrafts returns the project's own draft blueprints (project-scoped,
// not yet published), sorted by name.
func (s *Server) listPrivateDrafts(ctx context.Context) ([]BlueprintRecord, error) {
	out, err := s.memory.ListBlueprints(ctx)
	if err != nil {
		return nil, err
	}
	var drafts []BlueprintRecord
	for _, rec := range out {
		if rec.Status == "draft" && rec.ProjectID != nil {
			drafts = append(drafts, rec)
		}
	}
	sort.Slice(drafts, func(i, j int) bool { return drafts[i].Name < drafts[j].Name })
	return drafts, nil
}

// installBlueprintById applies an already-seeded blueprint to the project
// (JSON API).
func (s *Server) installBlueprintById(c echo.Context) error {
	res, err := s.memory.ApplyBlueprint(c.Request().Context(), c.Param("id"))
	if err != nil {
		return blueprintMemoryError(c, err)
	}
	return c.JSON(http.StatusCreated, res)
}

// unapplyBlueprint reverses a blueprint apply (JSON API).
func (s *Server) unapplyBlueprint(c echo.Context) error {
	res, err := s.memory.UnapplyBlueprint(c.Request().Context(), c.Param("id"))
	if err != nil {
		return blueprintMemoryError(c, err)
	}
	return c.JSON(http.StatusOK, res)
}

// blueprintMemoryError maps memory API errors onto gateway status codes:
// unknown ids (404/not_found) are 404, an upstream 403 (the caller is forbidden
// by the backend) is surfaced as 403 rather than masked as a gateway failure,
// and everything else is 502. Note: this mapper is shared with schema.go's JSON
// handlers; the 403 passthrough is unambiguously the forbidden case there too.
func blueprintMemoryError(c echo.Context, err error) error {
	msg := err.Error()
	if isMemoryNotFound(err) {
		return c.JSON(http.StatusNotFound, map[string]string{"error": msg})
	}
	if isMemoryStatus(err, http.StatusForbidden) {
		return c.JSON(http.StatusForbidden, map[string]string{"error": msg})
	}
	return c.JSON(http.StatusBadGateway, map[string]string{"error": msg})
}

// --- UI (PRG) handlers ---

// pinnedAgentsFromManifest returns the manifest agents that pin an explicit
// model — the only agents the model-review step shows.
func pinnedAgentsFromManifest(manifest blueprintManifest) []blueprintAgent {
	var out []blueprintAgent
	for _, a := range manifest.Agents {
		if a.Model != nil && a.Model.Name != "" {
			out = append(out, a)
		}
	}
	return out
}

// buildModelMapRows maps each pinned manifest agent to a review row. Issue is
// empty when the pinned model resolves against the configured providers (a
// prefixed model whose provider is configured); otherwise it carries the reason
// the model needs attention. Selected is the pre-filled override: the saved
// override (when present, even an empty "clear" value), else the pinned model
// when servable, else empty (project default).
func buildModelMapRows(manifest blueprintManifest, providerNames []string, saved map[string]string) []BlueprintModelMapRow {
	rows := make([]BlueprintModelMapRow, 0, len(manifest.Agents))
	for _, a := range manifest.Agents {
		if a.Model == nil || a.Model.Name == "" {
			continue
		}
		model := a.Model.Name
		prov := modelProviderOf(model)
		issue := ""
		switch {
		case prov == "":
			issue = "Set a provider for this model (expected provider/model)."
		case !slices.Contains(providerNames, prov):
			issue = "The '" + prov + "' provider is not configured in this project."
		}
		selected := ""
		if v, ok := saved[a.Name]; ok {
			selected = v
		} else if issue == "" {
			selected = model
		}
		rows = append(rows, BlueprintModelMapRow{
			AgentName:   a.Name,
			SourceModel: model,
			Issue:       issue,
			Selected:    selected,
		})
	}
	return rows
}

// blueprintModelMapContext resolves the review-page state for a manifest: the
// project's configured generative models plus the per-agent rows, pre-filled
// from the saved per-project overrides for the blueprint name (best effort). A
// provider fetch failure leaves the catalog empty, which makes every prefixed
// model unresolved (still safe — the user maps them); a saved-override fetch
// failure just pre-fills nothing.
func (s *Server) blueprintModelMapContext(ctx context.Context, manifest blueprintManifest, name string) ([]BlueprintModelMapRow, []Model) {
	providers, _ := s.memory.ListProjectProviders(ctx)
	models := s.configuredGenerativeModels(ctx, providers)
	providerNames := projectProviderNames(providers)

	saved := map[string]string{}
	if name != "" {
		if ov, err := s.memory.GetModelOverrides(ctx, name); err == nil {
			saved = ov
		} else {
			captureError(err)
		}
	}
	return buildModelMapRows(manifest, providerNames, saved), models
}

// loadInstallManifest resolves a blueprint id's manifest and name for the
// review step. ok is false when the blueprint can't be resolved (e.g. a legacy
// registry schema, where install proceeds directly) or its manifest can't be
// parsed — either way the caller falls back to a direct apply.
func (s *Server) loadInstallManifest(ctx context.Context, id string) (manifest blueprintManifest, name string, ok bool) {
	rec, err := s.memory.GetBlueprint(ctx, id)
	if err != nil {
		return blueprintManifest{}, "", false
	}
	var m blueprintManifest
	if err := json.Unmarshal(rec.Manifest, &m); err != nil {
		return blueprintManifest{}, "", false
	}
	return m, rec.Name, true
}

// modelOverridesFromForm reads the per-agent `model.<agentName>` form fields
// into the override map. It mirrors the import confirm parsing: an empty value
// clears the model so the agent falls back to the project default.
func modelOverridesFromForm(form url.Values) map[string]string {
	overrides := make(map[string]string)
	for key, values := range form {
		agentName, ok := strings.CutPrefix(key, "model.")
		if !ok {
			continue
		}
		if len(values) > 0 {
			overrides[agentName] = values[0]
		} else {
			overrides[agentName] = ""
		}
	}
	return overrides
}

// applyBlueprintPRG applies a blueprint (optionally with model overrides) and
// redirects to the appropriate flash. Apply does not surface migration status,
// so fall back to the drift scan around the apply: redirect to migrations only
// when this install increased the stale-object count.
func (s *Server) applyBlueprintPRG(c echo.Context, id string, overrides map[string]string) error {
	ctx := c.Request().Context()
	before := s.checkMigrationAfterApply(ctx)
	var err error
	if len(overrides) > 0 {
		_, err = s.memory.ApplyBlueprintWithOverrides(ctx, id, overrides)
	} else {
		_, err = s.memory.ApplyBlueprint(ctx, id)
	}
	if err != nil {
		return redirectWithError(c, "/blueprints", err)
	}
	if after := s.checkMigrationAfterApply(ctx); after > before {
		msg := "Blueprint applied; stale objects went from " + strconv.Itoa(before) + " to " + strconv.Itoa(after) + ". Review schema migrations."
		return c.Redirect(http.StatusSeeOther, "/blueprints/migrations?migrateMsg="+url.QueryEscape(msg))
	}
	return c.Redirect(http.StatusSeeOther, "/blueprints?installed=1")
}

// uiInstallBlueprint handles the install form on the blueprints page. When the
// blueprint pins models it renders the review step first; otherwise it applies
// directly. A missing schemaId short-circuits to the generic error flash.
func (s *Server) uiInstallBlueprint(c echo.Context) error {
	schemaID := c.FormValue("schemaId")
	ctx := c.Request().Context()
	if schemaID == "" {
		return c.Redirect(http.StatusSeeOther, "/blueprints?err=1")
	}
	manifest, name, ok := s.loadInstallManifest(ctx, schemaID)
	if !ok || len(pinnedAgentsFromManifest(manifest)) == 0 {
		return s.applyBlueprintPRG(c, schemaID, nil)
	}
	rows, models := s.blueprintModelMapContext(ctx, manifest, name)
	return s.page(c, pageTitle("Review blueprint models"), BlueprintImportModelMapPage(BlueprintModelMapData{
		Action:      "/blueprints/install/confirm",
		BlueprintID: schemaID,
		Rows:        rows,
		Models:      models,
	}))
}

// uiInstallBlueprintById applies an already-seeded blueprint, showing the
// review step first when it pins models (PRG).
func (s *Server) uiInstallBlueprintById(c echo.Context) error {
	id := c.Param("id")
	ctx := c.Request().Context()
	manifest, name, ok := s.loadInstallManifest(ctx, id)
	if !ok || len(pinnedAgentsFromManifest(manifest)) == 0 {
		return s.applyBlueprintPRG(c, id, nil)
	}
	rows, models := s.blueprintModelMapContext(ctx, manifest, name)
	return s.page(c, pageTitle("Review blueprint models"), BlueprintImportModelMapPage(BlueprintModelMapData{
		Action:      "/blueprints/install/confirm",
		BlueprintID: id,
		Rows:        rows,
		Models:      models,
	}))
}

// uiInstallBlueprintConfirm handles the review-step form submission for an
// install: it reads the hidden id plus the per-agent `model.<agentName>`
// overrides and applies them (an empty value clears the model → project
// default). Success redirects to the installed flash; failure redirects with
// the mapped error.
func (s *Server) uiInstallBlueprintConfirm(c echo.Context) error {
	id := c.FormValue("id")
	if id == "" {
		return c.Redirect(http.StatusSeeOther, "/blueprints?err=1")
	}
	if err := c.Request().ParseForm(); err != nil {
		return redirectWithError(c, "/blueprints", err)
	}
	return s.applyBlueprintPRG(c, id, modelOverridesFromForm(c.Request().PostForm))
}

// uiImportGitHubBlueprint previews a blueprint import from a GitHub repository
// URL (side-effect free), then either imports+applies it directly when no agent
// pins a model, or renders the model-review page so the user can confirm the
// pinned models first. The optional token is a private-repo credential: it is
// forwarded only to memory and is never placed in the redirect target or flash.
func (s *Server) uiImportGitHubBlueprint(c echo.Context) error {
	repoURL := c.FormValue("url")
	ref := c.FormValue("ref")
	token := c.FormValue("token")
	if repoURL == "" {
		return c.Redirect(http.StatusSeeOther, "/blueprints?err=1")
	}
	ctx := c.Request().Context()

	prev, err := s.memory.PreviewImportBlueprint(ctx, repoURL, ref, token)
	if err != nil {
		return redirectWithError(c, "/blueprints", err)
	}

	var manifest blueprintManifest
	if err := json.Unmarshal(prev.Manifest, &manifest); err != nil {
		return redirectWithError(c, "/blueprints", err)
	}

	rows, models := s.blueprintModelMapContext(ctx, manifest, prev.Name)

	if len(rows) == 0 {
		if _, err := s.memory.ImportAndApplyBlueprint(ctx, repoURL, ref, token, nil); err != nil {
			return redirectWithError(c, "/blueprints", err)
		}
		return c.Redirect(http.StatusSeeOther, "/blueprints?installed=1")
	}

	return s.page(c, pageTitle("Review blueprint models"), BlueprintImportModelMapPage(BlueprintModelMapData{
		Action: "/blueprints/import/confirm",
		URL:    repoURL,
		Ref:    ref,
		Token:  token,
		Rows:   rows,
		Models: models,
	}))
}

// uiImportGitHubBlueprintConfirm handles the review-step form submission: it
// reads the per-agent `model.<agentName>` overrides and applies the import with
// them (an empty value clears the model → project default). Success redirects
// to the installed flash; failure redirects with the mapped error.
func (s *Server) uiImportGitHubBlueprintConfirm(c echo.Context) error {
	repoURL := c.FormValue("url")
	ref := c.FormValue("ref")
	token := c.FormValue("token")
	if repoURL == "" {
		return c.Redirect(http.StatusSeeOther, "/blueprints?err=1")
	}
	ctx := c.Request().Context()

	if err := c.Request().ParseForm(); err != nil {
		return redirectWithError(c, "/blueprints", err)
	}

	if _, err := s.memory.ImportAndApplyBlueprint(ctx, repoURL, ref, token, modelOverridesFromForm(c.Request().PostForm)); err != nil {
		return redirectWithError(c, "/blueprints", err)
	}
	return c.Redirect(http.StatusSeeOther, "/blueprints?installed=1")
}

// uiRemoveBlueprint deletes a private, unapplied blueprint from the registry
// (DELETE /api/blueprints/:id) and redirects back to the list. A 409 (applied
// or global built-in) maps to a friendly flash; success flashes "removed".
func (s *Server) uiRemoveBlueprint(c echo.Context) error {
	if err := s.memory.DeleteBlueprint(c.Request().Context(), c.Param("id")); err != nil {
		if isMemoryStatus(err, http.StatusConflict) {
			return c.Redirect(http.StatusSeeOther, "/blueprints?err="+url.QueryEscape(blueprintRemoveConflictMsg))
		}
		return redirectWithError(c, "/blueprints", err)
	}
	return c.Redirect(http.StatusSeeOther, "/blueprints?removed=1")
}

// blueprintRemoveConflictMsg is the friendly 409 flash shown when removing a
// blueprint that is applied (or a global built-in).
const blueprintRemoveConflictMsg = "Uninstall the blueprint before removing it from the registry."

// checkMigrationAfterApply returns the count of stale objects after a blueprint
// apply (heuristic for a pending migration, since apply exposes no status).
func (s *Server) checkMigrationAfterApply(ctx context.Context) int {
	v, err := s.memory.ValidateSchemas(ctx)
	if err != nil {
		return 0
	}
	return v.StaleObjects
}

// uiUnapplyBlueprint reverses a blueprint apply and redirects back. A 404
// (blueprint not applied) is benign — nothing to remove.
func (s *Server) uiUnapplyBlueprint(c echo.Context) error {
	if _, err := s.memory.UnapplyBlueprint(c.Request().Context(), c.Param("id")); err != nil {
		if isMemoryNotFound(err) {
			return c.Redirect(http.StatusSeeOther, "/blueprints")
		}
		return redirectWithError(c, "/blueprints", err)
	}
	return c.Redirect(http.StatusSeeOther, "/blueprints")
}

// uiCheckBlueprintUpdates enqueues an on-demand update check for one applied
// blueprint and redirects back to the list (PRG). The check runs asynchronously
// in memory, so the redirect carries a "queued" flash rather than a result.
func (s *Server) uiCheckBlueprintUpdates(c echo.Context) error {
	name := c.Param("name")
	if name == "" {
		return c.Redirect(http.StatusSeeOther, "/blueprints?err=1")
	}
	if _, err := s.memory.CheckBlueprintUpdates(c.Request().Context(), name); err != nil {
		return redirectWithError(c, "/blueprints", err)
	}
	return c.Redirect(http.StatusSeeOther, "/blueprints?checkQueued=1")
}

// uiApplyBlueprintUpdate enqueues the asynchronous apply of a detected upstream
// update for one applied blueprint and redirects back to the list (PRG). A
// duplicate enqueue while an update is already pending/running is surfaced as a
// clear flash error (the reload then shows the running job).
func (s *Server) uiApplyBlueprintUpdate(c echo.Context) error {
	name := c.Param("name")
	if name == "" {
		return c.Redirect(http.StatusSeeOther, "/blueprints?err=1")
	}
	if _, err := s.memory.ApplyBlueprintUpdate(c.Request().Context(), name); err != nil {
		if isMemoryStatus(err, http.StatusConflict) {
			return redirectWithError(c, "/blueprints", errors.New("an update for this blueprint is already pending or running"))
		}
		return redirectWithError(c, "/blueprints", err)
	}
	return c.Redirect(http.StatusSeeOther, "/blueprints?updateQueued=1")
}

// uiSaveGithubCredential stores (or replaces) the project's encrypted GitHub
// personal access token used for private-repo update checks, then redirects
// back to the list (PRG). The token is forwarded only to memory and is never
// echoed back or placed in the redirect target.
func (s *Server) uiSaveGithubCredential(c echo.Context) error {
	token := strings.TrimSpace(c.FormValue("token"))
	if token == "" {
		return c.Redirect(http.StatusSeeOther, "/blueprints?err=1")
	}
	if err := s.memory.SetGithubCredential(c.Request().Context(), token); err != nil {
		return redirectWithError(c, "/blueprints", err)
	}
	return c.Redirect(http.StatusSeeOther, "/blueprints?ghCredentialSaved=1")
}

// uiBlueprint renders the details page for one blueprint: schema metadata plus
// object/relationship type definitions. Applied blueprints resolve by blueprint
// id; registry schemas resolve by schema id. When the detail carries agents
// with pinned models, the project's configured providers are fetched
// best-effort so the page can warn about models whose provider isn't configured.
func (s *Server) uiBlueprint(c echo.Context) error {
	id := c.Param("id")
	ctx := c.Request().Context()

	var d *BlueprintDetail
	fromRecord := false
	if rec, err := s.memory.GetBlueprint(ctx, id); err == nil {
		d = buildBlueprintRecordDetail(rec)
		fromRecord = true
	} else {
		sch, err := s.memory.GetSchema(ctx, id)
		if err != nil {
			return s.page(c, pageTitle("Blueprint"), BlueprintDetailPage(nil, userFacingError(err)))
		}
		d = buildSchemaDetail(sch)
	}
	// Applied blueprints mark which exact version is installed in the project.
	// Best-effort: a failed fetch just leaves every row unmarked.
	applied, err := s.memory.ListAppliedBlueprints(ctx)
	if err != nil {
		captureError(err)
		applied = nil
	}
	// A blueprint record carries a version history; registry schemas do not.
	// Best-effort: a failed version fetch just hides the section.
	if fromRecord {
		d.CanDeriveVersion = true
		d.IsDraft = d.Status == "draft"
		d.Installed = appliedContainsBlueprint(applied, d.ID)
		if records, err := s.memory.ListBlueprintVersions(ctx, d.Name); err == nil {
			d.Versions = blueprintVersionViews(records, applied, d.ID)
			sortVersionsNewestFirst(d.Versions)
		} else {
			captureError(err)
		}
		if len(d.Versions) > 0 {
			d.CanonicalID = d.Versions[0].ID
			d.SuggestedVersion = nextVersion(d.Versions[0].Version)
		} else {
			d.CanonicalID = d.ID
			d.SuggestedVersion = nextVersion(d.Version)
		}
		// Draft versions may be compared against another version of the same
		// blueprint; the comparison base defaults to the highest version
		// strictly lower than the one being viewed.
		if d.IsDraft {
			options := make([]BlueprintVersionView, 0, len(d.Versions))
			for _, v := range d.Versions {
				if v.ID != d.ID {
					options = append(options, v)
				}
			}
			d.CompareOptions = options
			if baseID := chooseCompareBase(options, c.QueryParam("compare"), d.Version); baseID != "" {
				if baseRec, err := s.memory.GetBlueprint(ctx, baseID); err == nil {
					d.CompareVersion = baseRec.Version
					d.Diff = diffBlueprintDetails(buildBlueprintRecordDetail(baseRec), d)
				} else {
					captureError(err)
				}
			}
		}
	} else {
		d.CanonicalID = d.ID
		d.Installed = slices.ContainsFunc(applied, func(ap AppliedBlueprint) bool { return ap.Name == d.Name })
	}
	d.FlashErr = flashError(c)
	if v := c.QueryParam("derived"); v != "" {
		d.FlashMsg = "Draft version " + v + " created."
	}
	if detailCarriesPinnedModel(d) {
		if ps, err := s.memory.ListProjectProviders(ctx); err == nil {
			d.ProviderNames = projectProviderNames(ps)
		} else {
			captureError(err)
		}
	}
	return s.page(c, pageTitle(d.Name), BlueprintDetailPage(d, nil))
}

// detailCarriesPinnedModel reports whether the blueprint detail includes at
// least one agent with an explicit model — the only case that needs the
// project's configured provider keys for the availability warning.
func detailCarriesPinnedModel(d *BlueprintDetail) bool {
	for _, a := range d.Agents {
		if a.Model != "" {
			return true
		}
	}
	return false
}

// blueprintVersionViews maps blueprint records to the version-history view,
// marking the exact version installed in the project and the one being viewed.
func blueprintVersionViews(records []BlueprintRecord, applied []AppliedBlueprint, currentID string) []BlueprintVersionView {
	installed := make(map[string]bool, len(applied))
	for _, ap := range applied {
		installed[ap.BlueprintID] = true
	}
	out := make([]BlueprintVersionView, 0, len(records))
	for _, r := range records {
		out = append(out, BlueprintVersionView{
			ID:        r.ID,
			Version:   r.Version,
			Status:    r.Status,
			CreatedAt: r.CreatedAt,
			Installed: installed[r.ID],
			Current:   r.ID == currentID,
		})
	}
	return out
}

// appliedContainsBlueprint reports whether a blueprint id is applied.
func appliedContainsBlueprint(applied []AppliedBlueprint, id string) bool {
	return slices.ContainsFunc(applied, func(ap AppliedBlueprint) bool { return ap.BlueprintID == id })
}

// chooseCompareBase picks the comparison base id from options (newest first):
// an explicitly requested option id when valid, else the highest option whose
// version is strictly lower than current, else the first option.
func chooseCompareBase(options []BlueprintVersionView, requested, current string) string {
	if requested != "" {
		for _, o := range options {
			if o.ID == requested {
				return o.ID
			}
		}
	}
	for _, o := range options {
		if versionGreater(current, o.Version) {
			return o.ID
		}
	}
	if len(options) > 0 {
		return options[0].ID
	}
	return ""
}
