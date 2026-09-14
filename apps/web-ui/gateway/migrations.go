package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
)

// migrationData is the state rendered by the migrations page.
type migrationData struct {
	History       []SchemaHistoryItem
	Validation    *SchemaValidationResult
	Preview       *MigrationPreviewResult // last preview result, rendered by the UI
	FromID        string                  // suggested from schema id (or submitted from)
	ToID          string                  // suggested to schema id (or submitted to)
	Msg           string
	Err           string
	Block         string
	StaleGroups   []stalePackGroup   // per-object drift detail, grouped pack → type
	ActivePacks   []activePackOption // ACTIVE packs offerable as resync targets
	SuggestedPack string             // pack owning the suggested resync target
}

// staleTypeGroup is the stale objects of one object type within an owning pack.
type staleTypeGroup struct {
	Type  string
	Items []ObjectValidationResult
}

// stalePackGroup is the stale objects owned by one pack, grouped by object type.
// Pack is "" when the type could not be mapped to a compiled pack.
type stalePackGroup struct {
	Pack  string
	Count int
	Types []staleTypeGroup
}

// activePackOption is one ACTIVE schema install offered as a resync target.
type activePackOption struct {
	SchemaID    string
	Name        string
	Version     string
	InstalledAt string
}

// migrationCardKind is which top-of-page card MigrationsPage renders above the
// preview/rollback sections.
type migrationCardKind int

const (
	// migrationCardNone is the all-clear state: no stale objects (or no
	// validation data), so the "No pending migrations" card shows.
	migrationCardNone migrationCardKind = iota
	// migrationCardUpgrade is stale objects with an installed upgrade path
	// (distinct from/to schema ids): the migrate form shows.
	migrationCardUpgrade
	// migrationCardResync is stale objects whose stored schema_version lags the
	// installed (active) schema with no newer version pending: the migrate form
	// shows prefilled from==to==active so a same-version migration re-stamps
	// objects to the installed version.
	migrationCardResync
	// migrationCardFallback is stale objects with no active schema install to
	// target (no history row): a neutral card with no repair promise.
	migrationCardFallback
)

// migrationCardKindFor classifies the migration page state from the suggested
// from/to pair and the schema-drift validation result.
func migrationCardKindFor(data migrationData) migrationCardKind {
	if data.Validation == nil || data.Validation.StaleObjects == 0 {
		return migrationCardNone
	}
	if data.FromID != "" && data.ToID != "" {
		if data.FromID == data.ToID {
			return migrationCardResync
		}
		return migrationCardUpgrade
	}
	return migrationCardFallback
}

// staleObjectsMessage renders the schema-health alert copy, keyed to the actual
// repair the page offers: a version migration for an upgrade path, a same-version
// re-stamp for lagging objects, or neutral drift copy when nothing is actionable.
// It never promises a /blueprints blueprint-upgrade remedy.
func staleObjectsMessage(v *SchemaValidationResult, kind migrationCardKind) string {
	msg := strconv.Itoa(v.StaleObjects) + " of " + strconv.Itoa(v.TotalObjects) + " objects are stale (schema drifted). "
	switch kind {
	case migrationCardUpgrade:
		return msg + "Run a migration to fix."
	case migrationCardResync:
		return msg + "Objects lag the installed schema version — re-stamp them below."
	default:
		return msg + "No active schema install is recorded to migrate against."
	}
}

// suggestResync returns the SchemaID of the most-recently-installed ACTIVE
// history row (the current installed schema), or "" when no history row is
// active. It is the from==to target for a same-version resync migration that
// re-stamps lagging objects to the installed schema version. InstalledAt is
// RFC3339 from memory, so lexical order matches time order.
func suggestResync(history []SchemaHistoryItem) string {
	bestID := ""
	bestAt := ""
	for _, h := range history {
		if !h.Active {
			continue
		}
		if bestID == "" || (h.InstalledAt != "" && h.InstalledAt > bestAt) {
			bestID = h.SchemaID
			bestAt = h.InstalledAt
		}
	}
	return bestID
}

// activePacks returns one option per ACTIVE pack, deduped by pack name and
// keeping the most recently installed row. It is the resync target list: each
// option's SchemaID is both the from and to of a same-version re-stamp. Sorted
// by name for stable rendering.
func activePacks(history []SchemaHistoryItem) []activePackOption {
	byName := map[string]activePackOption{}
	for _, h := range history {
		if !h.Active {
			continue
		}
		cur, ok := byName[h.Name]
		if !ok || h.InstalledAt > cur.InstalledAt {
			byName[h.Name] = activePackOption{
				SchemaID:    h.SchemaID,
				Name:        h.Name,
				Version:     h.Version,
				InstalledAt: h.InstalledAt,
			}
		}
	}
	out := make([]activePackOption, 0, len(byName))
	for _, p := range byName {
		out = append(out, p)
	}
	slices.SortFunc(out, func(a, b activePackOption) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// groupStaleObjects groups memory's per-object drift results by owning pack and
// object type. Unresolved packs (no compiled-pack match) sort last, then types
// alphabetically, then object key. Each item keeps its raw issues so the UI can
// show stamp lag vs NULL stamp vs content write.
func groupStaleObjects(results []ObjectValidationResult, typePack map[string]string) []stalePackGroup {
	if len(results) == 0 {
		return nil
	}
	type groupKey struct{ pack, typ string }
	items := map[groupKey][]ObjectValidationResult{}
	for _, r := range results {
		k := groupKey{pack: typePack[r.Type], typ: r.Type}
		items[k] = append(items[k], r)
	}
	keys := make([]groupKey, 0, len(items))
	for k := range items {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b groupKey) int {
		if c := strings.Compare(packSortKey(a.pack), packSortKey(b.pack)); c != 0 {
			return c
		}
		return strings.Compare(a.typ, b.typ)
	})
	var out []stalePackGroup
	for _, k := range keys {
		its := items[k]
		slices.SortFunc(its, func(a, b ObjectValidationResult) int {
			if c := strings.Compare(a.Key, b.Key); c != 0 {
				return c
			}
			return strings.Compare(a.EntityID, b.EntityID)
		})
		if n := len(out); n > 0 && out[n-1].Pack == k.pack {
			out[n-1].Count += len(its)
			out[n-1].Types = append(out[n-1].Types, staleTypeGroup{Type: k.typ, Items: its})
			continue
		}
		out = append(out, stalePackGroup{
			Pack:  k.pack,
			Count: len(its),
			Types: []staleTypeGroup{{Type: k.typ, Items: its}},
		})
	}
	return out
}

// packSortKey orders unresolved packs ("") after named packs.
func packSortKey(pack string) string {
	if pack == "" {
		return "\xff"
	}
	return pack
}

// suggestResyncPack picks the resync target among active packs: the pack owning
// the most stale objects, tie-broken by most recent install. Returns the zero
// option when no stale object maps to a pack, so callers can fall back to the
// historical most-recently-installed active row.
func suggestResyncPack(groups []stalePackGroup, packs []activePackOption) activePackOption {
	if len(packs) == 0 {
		return activePackOption{}
	}
	counts := map[string]int{}
	for _, g := range groups {
		if g.Pack != "" {
			counts[g.Pack] += g.Count
		}
	}
	best := activePackOption{}
	bestCount := 0
	for _, p := range packs {
		c := counts[p.Name]
		if c > bestCount || (c == bestCount && c > 0 && p.InstalledAt > best.InstalledAt) {
			best, bestCount = p, c
		}
	}
	return best
}

// staleTypePackIndex maps a stale object's Type to its owning compiled pack.
// Memory's /validate results do not name the pack, but /compiled-types carries
// schemaName per type; fall back to the bundled (compiled-in) pack definitions
// when memory returns nothing usable.
func (s *Server) staleTypePackIndex(ctx context.Context) map[string]string {
	idx := map[string]string{}
	if compiled, err := s.memory.GetCompiledTypes(ctx); err == nil && compiled != nil {
		for _, t := range compiled.ObjectTypes {
			if t.Name != "" && t.SchemaName != "" {
				idx[t.Name] = t.SchemaName
			}
		}
	}
	if len(idx) > 0 {
		return idx
	}
	for _, bp := range s.bundledPackDefinitions() {
		for _, t := range bp.ObjectTypeSchemas {
			name := strAny(t["name"])
			if name == "" {
				continue
			}
			if _, ok := idx[name]; !ok {
				idx[name] = bp.Name
			}
		}
	}
	return idx
}

// bundledPackDefinitions loads every bundled pack definition (embedded or the
// BLUEPRINTS_DIR override) so stale types can be mapped to their pack when
// memory's compiled-types view is unavailable.
func (s *Server) bundledPackDefinitions() []*BundledBlueprint {
	items, err := s.bundledBlueprints()
	if err != nil {
		return nil
	}
	out := make([]*BundledBlueprint, 0, len(items))
	for _, it := range items {
		bp, err := s.loadBundled(it.ID)
		if err != nil {
			continue
		}
		out = append(out, bp)
	}
	return out
}

// uiMigrations renders the schema-migrations page: install history, schema
// validation, and migrate/rollback controls.
func (s *Server) uiMigrations(c echo.Context) error {
	ctx := c.Request().Context()
	history, err := s.memory.GetSchemaHistory(ctx)
	if err != nil {
		history = nil
	}
	data := s.migrationsData(c, history)
	return s.page(c, pageTitle("Migrations"), MigrationsPage(data))
}

// migrationsData assembles the shared state for the migrations page: install
// history, validation results, the suggested from/to upgrade path, and any
// migrateMsg/migrateErr/migrateBlock query params from a redirect.
func (s *Server) migrationsData(c echo.Context, history []SchemaHistoryItem) migrationData {
	ctx := c.Request().Context()
	validation, err := s.memory.ValidateSchemas(ctx)
	captureError(err)
	packs := activePacks(history)
	data := migrationData{
		History:     history,
		Validation:  validation,
		ActivePacks: packs,
		Msg:         c.QueryParam("migrateMsg"),
		Err:         c.QueryParam("migrateErr"),
		Block:       c.QueryParam("migrateBlock"),
	}
	fromID, toID := suggestMigration(history)
	if validation != nil && validation.StaleObjects > 0 {
		data.StaleGroups = groupStaleObjects(validation.Results, s.staleTypePackIndex(ctx))
	}
	// No upgrade path but objects are stale: staleness means stored
	// schema_version lags the active schema, so offer a same-version resync
	// (from==to==active) instead of a dead end.
	if fromID == "" && toID == "" && validation != nil && validation.StaleObjects > 0 {
		target := suggestResyncPack(data.StaleGroups, packs)
		if target.SchemaID == "" {
			// No stale object mapped to a pack: keep the historical
			// most-recently-installed active row as the fallback target.
			fallback := suggestResync(history)
			for _, p := range packs {
				if p.SchemaID == fallback {
					target = p
					break
				}
			}
		}
		if target.SchemaID != "" {
			fromID, toID = target.SchemaID, target.SchemaID
			data.SuggestedPack = target.Name
		}
	}
	data.FromID, data.ToID = fromID, toID
	return data
}

// suggestMigration picks the from/to schema ids for the upgrade path: it groups
// install history by pack name, sorts each group by version ascending, and
// returns the first group where an Active version is not the newest — the active
// version as FromID and the highest version as ToID. Returns empty ids when no
// group has an upgrade path.
func suggestMigration(history []SchemaHistoryItem) (fromID, toID string) {
	groups := map[string][]SchemaHistoryItem{}
	var order []string
	for _, h := range history {
		if _, ok := groups[h.Name]; !ok {
			order = append(order, h.Name)
		}
		groups[h.Name] = append(groups[h.Name], h)
	}
	for _, name := range order {
		g := groups[name]
		sort.SliceStable(g, func(i, j int) bool { return compareVersions(g[i].Version, g[j].Version) < 0 })
		if len(g) < 2 {
			continue
		}
		highest := g[len(g)-1]
		for _, it := range g {
			if it.Active && compareVersions(it.Version, highest.Version) < 0 {
				return it.SchemaID, highest.SchemaID
			}
		}
	}
	return "", ""
}

// compareVersions orders version strings numerically for the common
// major.minor.patch form; versions with non-numeric segments fall back to plain
// string ordering so anything memory returns still sorts deterministically.
func compareVersions(a, b string) int {
	av, aOK := semverParts(a)
	bv, bOK := semverParts(b)
	if !aOK || !bOK {
		return strings.Compare(a, b)
	}
	n := len(av)
	if len(bv) > n {
		n = len(bv)
	}
	for i := range n {
		var x, y int
		if i < len(av) {
			x = av[i]
		}
		if i < len(bv) {
			y = bv[i]
		}
		if x < y {
			return -1
		}
		if x > y {
			return 1
		}
	}
	return 0
}

// semverParts splits a dotted version string into ints; ok is false if any
// segment is non-numeric.
func semverParts(v string) ([]int, bool) {
	parts := strings.Split(v, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, false
		}
		out = append(out, n)
	}
	return out, true
}

// uiMigrate handles the migrate form (preview or execute). The preview branch
// renders the page directly with the plan visible; execute redirects with the
// result message.
func (s *Server) uiMigrate(c echo.Context) error {
	from := c.FormValue("from")
	to := c.FormValue("to")
	// A pack-targeted resync picker submits the chosen active pack's schema id
	// instead of the separate from/to selects (from == to == pack).
	if packID := c.FormValue("resyncPack"); packID != "" {
		from, to = packID, packID
	}
	force := c.FormValue("force") == "true"
	action := c.FormValue("action") // "preview" | "execute"
	ctx := c.Request().Context()
	if from == "" || to == "" {
		return c.Redirect(http.StatusSeeOther, "/blueprints/migrations?migrateErr="+url.QueryEscape("from and to schema ids are required"))
	}
	switch action {
	case "execute":
		q := url.Values{}
		res, err := s.memory.ExecuteMigration(ctx, from, to, force)
		if err != nil {
			q.Set("migrateErr", err.Error())
		} else {
			q.Set("migrateMsg", fmt.Sprintf("Migrated %d objects (%d failed)", res.ObjectsMigrated, res.ObjectsFailed))
		}
		return c.Redirect(http.StatusSeeOther, "/blueprints/migrations?"+q.Encode())
	default:
		res, err := s.memory.PreviewMigration(ctx, from, to)
		if err != nil {
			return c.Redirect(http.StatusSeeOther, "/blueprints/migrations?migrateErr="+url.QueryEscape(err.Error()))
		}
		// Render the page directly so the plan stays visible, with the same
		// history/validation state as uiMigrations and the submitted from/to.
		history, herr := s.memory.GetSchemaHistory(ctx)
		if herr != nil {
			history = nil
		}
		data := s.migrationsData(c, history)
		data.Preview = res
		data.FromID = from
		data.ToID = to
		data.Msg = fmt.Sprintf("Preview: %d objects, risk %q", res.TotalObjects, res.OverallRiskLevel)
		if !res.CanProceed {
			data.Block = res.BlockReason
		}
		return s.page(c, pageTitle("Migrations"), MigrationsPage(data))
	}
}

// uiRollbackMigration handles the rollback form.
func (s *Server) uiRollbackMigration(c echo.Context) error {
	toVersion := c.FormValue("toVersion")
	if toVersion == "" {
		return c.Redirect(http.StatusSeeOther, "/blueprints/migrations?migrateErr="+url.QueryEscape("to_version is required"))
	}
	res, err := s.memory.RollbackMigration(c.Request().Context(), toVersion)
	if err != nil {
		return c.Redirect(http.StatusSeeOther, "/blueprints/migrations?migrateErr="+url.QueryEscape(err.Error()))
	}
	return c.Redirect(http.StatusSeeOther, "/blueprints/migrations?migrateMsg="+url.QueryEscape(fmt.Sprintf("Rolled back to %s: %d objects restored", toVersion, res.ObjectsRestored)))
}
