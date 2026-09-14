package main

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"

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
	schemas, err := s.memory.ListAllSchemas(ctx)
	if err != nil {
		// A registry failure still lets bundled packs surface.
		schemas = nil
	}
	applied, err := s.memory.ListAppliedBlueprints(ctx)
	captureError(err)
	bundled, _ := s.bundledBlueprints()
	return c.JSON(http.StatusOK, buildAvailableList(schemas, bundled, applied, s.existingAgents(ctx)))
}

func (s *Server) getCompiledTypes(c echo.Context) error {
	out, err := s.memory.GetCompiledTypes(c.Request().Context())
	if err != nil {
		return blueprintMemoryError(c, err)
	}
	return c.JSON(http.StatusOK, out)
}

// installBlueprintRequest is the body for POST /api/blueprints/install. Either
// SchemaID (registry pack) or Source+"bundled" + Name (bundled pack) is set.
type installBlueprintRequest struct {
	SchemaID string `json:"schemaId"`
	Source   string `json:"source"`
	Name     string `json:"name"`
}

// enableBlueprintRequest is the body for POST /api/blueprints/enable.
type enableBlueprintRequest struct {
	Name string `json:"name"`
}

func (s *Server) installBlueprint(c echo.Context) error {
	var in installBlueprintRequest
	if err := c.Bind(&in); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	ctx := c.Request().Context()
	var result any
	var err error
	switch {
	case in.Source == "bundled" && in.Name != "":
		result, err = s.installBundledBlueprint(ctx, in.Name)
	case in.SchemaID != "":
		result, err = s.installRegistryBlueprint(ctx, in.SchemaID)
	default:
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "schemaId or bundled source+name required"})
	}
	if err != nil {
		return blueprintMemoryError(c, err)
	}
	return c.JSON(http.StatusCreated, result)
}

// seedBundledBlueprint resolves (create → publish) the definition of a bundled
// pack in memory so it exists as a GLOBAL, published blueprint, ready to be
// applied. It never applies — enabling and installing are separate steps. The
// resolved record is returned (a newly created draft is published before
// returning).
func (s *Server) seedBundledBlueprint(ctx context.Context, name string) (*BlueprintRecord, error) {
	bp, err := s.loadBundled(name)
	if err != nil {
		return nil, err
	}
	manifest, err := buildBlueprintManifest(bp)
	if err != nil {
		return nil, err
	}
	id, err := s.resolveOrCreateBlueprint(ctx, bp.Name, bp.Version, bp.Description, bp.Author, manifest)
	if err != nil {
		return nil, err
	}
	rec, err := s.memory.GetBlueprint(ctx, id)
	if err != nil {
		return nil, err
	}
	if rec.Status == "draft" {
		if err := s.memory.PublishBlueprint(ctx, id); err != nil {
			return nil, err
		}
	}
	return rec, nil
}

// installBundledBlueprint installs a bundled pack through memory's blueprint
// API: seed the definition (create+publish), then apply it. The definition row
// persists in memory — the foundation for uninstall/update via /applied and
// /unapply.
func (s *Server) installBundledBlueprint(ctx context.Context, name string) (*BlueprintApplyResult, error) {
	rec, err := s.seedBundledBlueprint(ctx, name)
	if err != nil {
		return nil, err
	}
	return s.memory.ApplyBlueprint(ctx, rec.ID)
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

// enableBlueprint seeds a bundled blueprint as a GLOBAL published definition
// without applying it (JSON API).
func (s *Server) enableBlueprint(c echo.Context) error {
	var in enableBlueprintRequest
	if err := c.Bind(&in); err != nil || in.Name == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "name required"})
	}
	rec, err := s.seedBundledBlueprint(c.Request().Context(), in.Name)
	if err != nil {
		return blueprintMemoryError(c, err)
	}
	return c.JSON(http.StatusCreated, rec)
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
// unknown ids (404/not_found) are 404, everything else is 502.
func blueprintMemoryError(c echo.Context, err error) error {
	msg := err.Error()
	if isMemoryNotFound(err) {
		return c.JSON(http.StatusNotFound, map[string]string{"error": msg})
	}
	return c.JSON(http.StatusBadGateway, map[string]string{"error": msg})
}

// --- UI (PRG) handlers ---

// uiInstallBlueprint handles the install form on the blueprints page: it
// performs the install and redirects back to the list (or to ?err=1 on failure).
func (s *Server) uiInstallBlueprint(c echo.Context) error {
	schemaID := c.FormValue("schemaId")
	name := c.FormValue("name")
	ctx := c.Request().Context()
	if schemaID == "" && name == "" {
		return c.Redirect(http.StatusSeeOther, "/blueprints?err=1")
	}
	// Blueprint apply does not surface migration status, so fall back to the
	// drift scan around the install: redirect to migrations only when this
	// install increased the stale-object count. A pure-agent install (no schema
	// packs) can never cause drift, so it must not bounce the user over
	// unrelated pre-existing staleness.
	before := s.checkMigrationAfterApply(ctx)
	var err error
	if schemaID != "" {
		_, err = s.installRegistryBlueprint(ctx, schemaID)
	} else {
		_, err = s.installBundledBlueprint(ctx, name)
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

// uiInstallBlueprintById applies an already-seeded blueprint and redirects back
// to the list (PRG).
func (s *Server) uiInstallBlueprintById(c echo.Context) error {
	if _, err := s.memory.ApplyBlueprint(c.Request().Context(), c.Param("id")); err != nil {
		return redirectWithError(c, "/blueprints", err)
	}
	return c.Redirect(http.StatusSeeOther, "/blueprints?installed=1")
}

// uiEnableBlueprint seeds a bundled blueprint (global, published, not applied)
// and redirects back to the list (PRG).
func (s *Server) uiEnableBlueprint(c echo.Context) error {
	name := c.FormValue("name")
	if name == "" {
		return c.Redirect(http.StatusSeeOther, "/blueprints?err=1")
	}
	if _, err := s.seedBundledBlueprint(c.Request().Context(), name); err != nil {
		return redirectWithError(c, "/blueprints", err)
	}
	return c.Redirect(http.StatusSeeOther, "/blueprints?enabled=1")
}

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

// uiBlueprint renders the details page for one blueprint: schema metadata plus
// object/relationship type definitions. Bundled packs resolve by directory
// name; applied blueprints resolve by blueprint id; registry/built-in schemas
// resolve by schema id. When the detail carries agents with pinned models, the
// project's configured providers are fetched best-effort so the page can warn
// about models whose provider isn't configured.
func (s *Server) uiBlueprint(c echo.Context) error {
	id := c.Param("id")
	ctx := c.Request().Context()

	var d *BlueprintDetail
	fromRecord := false
	if bp, err := s.loadBundled(id); err == nil && bp.Name != "" {
		d = buildBundledDetail(bp)
	} else if rec, err := s.memory.GetBlueprint(ctx, id); err == nil {
		d = buildBlueprintRecordDetail(rec)
		fromRecord = true
	} else {
		sch, err := s.memory.GetSchema(ctx, id)
		if err != nil {
			return s.page(c, pageTitle("Blueprint"), BlueprintDetailPage(nil, err))
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
	// A blueprint record carries a version history; bundled packs and registry
	// schemas do not. Best-effort: a failed version fetch just hides the section.
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
