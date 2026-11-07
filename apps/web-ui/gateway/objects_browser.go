package main

import (
	"net/url"
	"strings"

	"github.com/a-h/templ"
	ui "github.com/emergent-company/go-daisy/components/ui"
)

// objectsBrowserSelectionJS is the Alpine model for the objects table's row
// selection. It lives on the page root so the checkbox column, the bulk bar, and
// the bulk dialogs share one source of truth.
//
// The model is a plain object literal (not Alpine.data) so it needs no
// registration script and works inside the gallery's sandboxed preview, where
// the document is loaded fresh. `all`/`some` are getters that read the DOM, so
// they stay correct after htmx appends a page of rows.
const objectsBrowserSelectionJS = `{
	sel: [],
	// The desktop table and the mobile card list are both in the DOM (one is
	// hidden by CSS), so every object has two selection inputs. All getters and
	// bulk toggles work on unique values, otherwise the select-all checkbox
	// never reaches "all" and toggleAll doubles the selection count.
	uniqueIDs(values) {
		var seen = Object.create(null), out = [];
		for (var i = 0; i < values.length; i++) {
			var v = values[i];
			if (!seen[v]) { seen[v] = true; out.push(v); }
		}
		return out;
	},
	allIDs() {
		var boxes = this.$root.querySelectorAll('input[data-object-select]');
		return this.uniqueIDs(Array.prototype.map.call(boxes, function (el) { return el.value; }));
	},
	// Statuses the bulk Set-status dialog may offer for the current selection:
	// the intersection of each selected type's declared options. A type with no
	// workflow has no entry and therefore no common option — the dialog disables
	// the action rather than stamping a workflow status on it (#2242).
	selectedTypes() {
		var seen = Object.create(null), out = [];
		var boxes = this.$root.querySelectorAll('input[data-object-select]:checked');
		for (var i = 0; i < boxes.length; i++) {
			var t = boxes[i].getAttribute('data-object-type') || "";
			if (t && !seen[t]) { seen[t] = true; out.push(t); }
		}
		return out;
	},
	get statusOptions() {
		var types = this.selectedTypes();
		if (!types.length) return [];
		var map = {};
		try { map = JSON.parse(this.$root.getAttribute('data-status-options') || '{}'); } catch (e) { return []; }
		var out = null;
		for (var i = 0; i < types.length; i++) {
			var opts = map[types[i]];
			if (!opts || !opts.length) return [];
			if (out === null) {
				out = opts.slice();
			} else {
				out = out.filter(function (s) { return opts.indexOf(s) !== -1; });
			}
			if (!out.length) return [];
		}
		return out || [];
	},
	get statusActionable() { return this.statusOptions.length > 0 },
	get count() { return this.uniqueIDs(this.sel).length },
	get all() {
		var ids = this.allIDs();
		return ids.length > 0 && this.uniqueIDs(this.sel).length === ids.length;
	},
	get some() {
		var selected = this.uniqueIDs(this.sel).length;
		var total = this.allIDs().length;
		return selected > 0 && selected < total;
	},
	toggleAll(checked) {
		this.sel = checked ? this.allIDs() : [];
	},
	clear() { this.sel = [] }
}`

// objectsSortHref is the query-only URL that flips the browse list between
// newest-first and oldest-first. It carries every active filter forward and
// omits the default direction, so the URL only grows when it has to. A
// query-only href (no path) resolves against the current document, which keeps
// it correct on /objects, on the agent-scoped route, and inside the gallery's
// page-preview iframe — the preview re-renders its sample rows from the same
// query string.
func objectsSortHref(data objectsPageData, dir string) string {
	q := url.Values{}
	if data.Query != "" {
		q.Set("q", data.Query)
		if data.Mode != "" {
			q.Set("mode", data.Mode)
		}
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
	if data.CredentialID != "" {
		q.Set("credential_id", data.CredentialID)
		if data.ViaShare != "" {
			q.Set("via", data.ViaShare)
		}
	}
	if dir != "" && dir != "desc" {
		q.Set("order", dir)
	}
	return "?" + q.Encode()
}

// objectsNextSortDir is the direction the header link toggles to.
func objectsNextSortDir(data objectsPageData) string {
	if objectsSortDir(data) == "asc" {
		return "desc"
	}
	return "asc"
}

// objectsFormAttrs is the htmx wiring for the objects toolbar's forms: each
// request carries the form's values to the page URL, replaces #objects-results
// in place, and pushes the URL so reloading keeps the filters. Searching and
// filtering therefore behave alike — the list changes, the header, stats strip,
// and controls stay where they are — and neither is a document navigation.
//
// The gallery preview renders no wiring: it has no backend, and it must not
// navigate its iframe.
func objectsFormAttrs(data objectsPageData, onChange bool, action string) templ.Attributes {
	if data.Preview {
		return nil
	}
	var attrs templ.Attributes
	if onChange {
		attrs = filterFormAttrs(action, objectsResultsRegion)
	} else {
		attrs = submitFilterFormAttrs(action, objectsResultsRegion)
	}
	attrs["hx-indicator"] = "#objects-results-loading"
	attrs["data-objects-list-refresh"] = "true"
	return attrs
}

// objectsPreviewGuards keeps a gallery preview inert: the form renders with all
// of its markup and validation, but a submit is discarded instead of posting to
// a handler (or navigating away from the preview). Live pages get no attribute.
func objectsPreviewGuards(preview bool) templ.Attributes {
	if !preview {
		return nil
	}
	return templ.Attributes{"onsubmit": "return false"}
}

// objectsBulkAction is the bulk mutation endpoint, or "#" in the gallery
// preview so a stray submit cannot reach a backend.
func objectsBulkAction(data objectsPageData) string {
	if data.Preview {
		return "#"
	}
	return "/objects/bulk"
}

// objectsBulkBackURL is the page a bulk dialog returns to after the mutation:
// the current view with its filters and order, so the reload lands where the
// selection was made instead of resetting to the unfiltered list. The cursor is
// deliberately not carried — the list starts over at the newest page.
func objectsBulkBackURL(data objectsPageData) string {
	return strings.TrimSuffix(objectsTargetURL(data)+objectsSortHref(data, data.Order), "?")
}

// objectsBrowserRowView is one row of the objects table, flattened from either a
// browse object or a search hit so the table has one rendering path. Search-only
// fields (Score) are zero in browse mode and simply not rendered.
type objectsBrowserRowView struct {
	ID               string
	Label            string
	Type             string
	Status           string
	StatusLabel      string
	StatusIntent     ui.BadgeIntent
	EmbeddingLabel   string
	EmbeddingVariant ui.BadgeIntent
	EmbeddingIcon    string
	EmbeddingClass   string
	Labels           []string
	CreatedAt        string
	Score            float32
	UI               typeUI
}

// objectsBrowserRows flattens the page data into table rows: browse objects in
// browse mode, ranked search hits otherwise.
func objectsBrowserRows(data objectsPageData) []objectsBrowserRowView {
	if data.Query != "" {
		rows := make([]objectsBrowserRowView, 0, len(data.Results))
		for _, r := range data.Results {
			rows = append(rows, newObjectsBrowserRow(r.Object, data.TypeUIByType, r.Score))
		}
		return rows
	}

	rows := make([]objectsBrowserRowView, 0, len(data.Objects))
	for _, o := range data.Objects {
		rows = append(rows, newObjectsBrowserRow(o, data.TypeUIByType, 0))
	}
	return rows
}

// newObjectsBrowserRow is the single mapping from a graph object to the table
// row view. Browse rows pass score 0 because they have no relevance column.
func newObjectsBrowserRow(o GraphObject, typeUIByType map[string]typeUI, score float32) objectsBrowserRowView {
	embedLabel, embedVariant := embeddingStatusBadge(o.EmbeddingStatus)
	embedIcon, embedClass := embeddingStatusIcon(o.EmbeddingStatus)
	return objectsBrowserRowView{
		ID:               o.ID,
		Label:            graphObjectLabel(o),
		Type:             o.Type,
		Status:           o.Status,
		StatusLabel:      boardStatusLabel(o.Status),
		StatusIntent:     boardStatusIntent(o.Status),
		EmbeddingLabel:   embedLabel,
		EmbeddingVariant: embedVariant,
		EmbeddingIcon:    embedIcon,
		EmbeddingClass:   embedClass,
		Labels:           o.Labels,
		CreatedAt:        o.CreatedAt,
		Score:            score,
		UI:               typeUIOf(typeUIByType, o.Type),
	}
}

// objectShortID is the id fragment shown above an object's name: the first
// segment of a UUID ("9f1c0a2e"), so the fragment never ends mid-segment with a
// dangling hyphen the way shortID's flat 10-character cut can.
func objectShortID(id string) string {
	if len(id) > 8 && id[8] == '-' {
		return id[:8]
	}
	return shortID(id)
}

// firstLabels returns at most n labels, so a row with many labels stays one
// line; the caller renders a "+N" chip for the remainder.
func firstLabels(labels []string, n int) []string {
	if len(labels) <= n {
		return labels
	}
	return labels[:n]
}
