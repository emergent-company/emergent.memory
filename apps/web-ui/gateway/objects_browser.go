package main

import (
	"net/url"
	"regexp"
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

// objectsBrowserRowView is one card of the objects grid, flattened from either a
// browse object or a search hit so the grid has one rendering path. Search-only
// fields (Score) are zero in browse mode and simply not rendered.
type objectsBrowserRowView struct {
	ID               string
	Ref              string
	Label            string
	Type             string
	Excerpt          string
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
	// DataClass* describe how the server classified this version (source,
	// synthesized, adopted). Empty when unreported, in which case the card
	// renders no chip rather than inventing provenance.
	DataClassLabel  string
	DataClassIntent ui.BadgeIntent
	DataClassIcon   string
	DataClassHint   string
	// Writer is the version's resolved write stamp: who wrote it, through which
	// credential, and the audit tooltip behind the name. When is the relative age
	// the card shows in its footer (a card is a glance, not an audit row, so the
	// history timeline's absolute stamp would be out of place).
	Writer objectVersionProvenance
	When   string
	// RevisionCount is reported on read paths only (a materialized view), so the
	// card links to the history only when the server actually said how many
	// versions exist.
	RevisionCount    int
	HasRevisionCount bool
}

// objectsBrowserRows flattens the page data into card views: browse objects in
// browse mode, ranked search hits otherwise. identities resolves each version's
// writer to a name; the page already loads them for the actor filter.
func objectsBrowserRows(data objectsPageData) []objectsBrowserRowView {
	if data.Query != "" {
		rows := make([]objectsBrowserRowView, 0, len(data.Results))
		for _, r := range data.Results {
			rows = append(rows, newObjectsBrowserRow(r.Object, data.TypeUIByType, data.ActorOptions, r.Score))
		}
		return rows
	}

	rows := make([]objectsBrowserRowView, 0, len(data.Objects))
	for _, o := range data.Objects {
		rows = append(rows, newObjectsBrowserRow(o, data.TypeUIByType, data.ActorOptions, 0))
	}
	return rows
}

// newObjectsBrowserRow is the single mapping from a graph object to the card
// view. Browse rows pass score 0 because they have no relevance chip.
func newObjectsBrowserRow(o GraphObject, typeUIByType map[string]typeUI, identities []objectActorOption, score float32) objectsBrowserRowView {
	embedLabel, embedVariant := embeddingStatusBadge(o.EmbeddingStatus)
	embedIcon, embedClass := embeddingStatusIcon(o.EmbeddingStatus)
	classLabel, classIntent, classIcon, classHint := dataClassChip(o.DataClass)
	r := objectsBrowserRowView{
		ID:               o.ID,
		Ref:              o.Ref,
		Label:            graphObjectLabel(o),
		Type:             o.Type,
		Excerpt:          objectExcerpt(o.Properties),
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
		DataClassLabel:   classLabel,
		DataClassIntent:  classIntent,
		DataClassIcon:    classIcon,
		DataClassHint:    classHint,
		Writer:           objectProvenanceOf(o, identities),
		When:             relTime(o.CreatedAt),
	}
	if o.RevisionCount != nil && *o.RevisionCount > 0 {
		r.RevisionCount = *o.RevisionCount
		r.HasRevisionCount = true
	}
	return r
}

// objectExcerptMaxRunes caps a card excerpt. A card is two or three lines of
// text at any card width, so the cap is about the cut, not the container: the
// template clamps the rest.
const objectExcerptMaxRunes = 280

// objectExcerptKeys is the prose-property precedence a card excerpt follows. The
// order runs from the most deliberate short field to the long-form body, so a
// type that declares both a summary and a description shows the summary. Keys
// are matched case-insensitively at each step — schemas in the wild use
// "Summary", "Description" and "content" interchangeably.
var objectExcerptKeys = []string{"summary", "description", "excerpt", "abstract", "content", "body", "notes"}

// objectExcerpt resolves the first paragraph of an object's prose for a card.
// It reads only string properties: an array or a number is not prose, and a
// nested object's first key is not a stand-in for one. No prose property → ""
// and the card renders without an excerpt rather than a truncated JSON dump.
func objectExcerpt(props map[string]any) string {
	if len(props) == 0 {
		return ""
	}
	for _, key := range objectExcerptKeys {
		s, ok := proseProperty(props, key)
		if !ok {
			continue
		}
		if text := flattenProseParagraph(s); text != "" {
			return truncateProseRunes(text, objectExcerptMaxRunes)
		}
	}
	return ""
}

// proseProperty finds a property by name, tolerating the case and separators
// schemas vary on ("LongDescription", "long_description").
func proseProperty(props map[string]any, key string) (string, bool) {
	_, s, ok := prosePropertyNamed(props, key)
	return s, ok
}

// prosePropertyNamed is proseProperty that also reports the property's actual
// name, which the article needs so the rail can skip the fields it rendered.
func prosePropertyNamed(props map[string]any, key string) (name, value string, ok bool) {
	if v, found := props[key]; found {
		s, isString := v.(string)
		return key, s, isString
	}
	for k, v := range props {
		if !strings.EqualFold(normalizeProseKey(k), key) {
			continue
		}
		s, isString := v.(string)
		return k, s, isString
	}
	return "", "", false
}

// normalizeProseKey folds a property name for comparison: separators dropped and
// spaces removed, so "long_description" and "Long Description" both match
// "longdescription".
func normalizeProseKey(k string) string {
	r := strings.NewReplacer("_", "", "-", "", " ", "", ".", "")
	return r.Replace(strings.TrimSpace(k))
}

var (
	proseImageRE  = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	proseLinkRE   = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	proseListItem = regexp.MustCompile(`^(?:[-*+]|\d+[.)])\s+`)
	proseHeading  = regexp.MustCompile(`^#{1,6}\s*`)
	proseQuote    = regexp.MustCompile(`^>\s*`)
	proseMarks    = strings.NewReplacer("**", "", "__", "", "~~", "", "`", "")
	proseSpaces   = regexp.MustCompile(`\s+`)
)

// flattenProseParagraph turns one markdown property into the single flat line a
// card shows: the first paragraph, with block markers (headings, quotes, list
// bullets), link targets, images and emphasis markers removed. It deliberately
// keeps the words of the first paragraph's list items — a card excerpt that
// stops at "the house has:" reads worse than one that continues into its list.
func flattenProseParagraph(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = proseImageRE.ReplaceAllString(s, "")
	s = proseLinkRE.ReplaceAllString(s, "$1")

	var (
		parts   []string
		started bool
	)
	for _, raw := range strings.Split(s, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			// The first blank line after prose ends the paragraph.
			if started {
				break
			}
			continue
		}
		if strings.HasPrefix(line, "```") {
			continue
		}
		// A heading is not a paragraph: its marker and its text are structure,
		// not prose, so a card excerpt never starts with a section title.
		if proseHeading.MatchString(line) {
			continue
		}
		line = proseQuote.ReplaceAllString(line, "")
		line = proseListItem.ReplaceAllString(line, "")
		line = proseMarks.Replace(line)
		if line = strings.TrimSpace(line); line == "" {
			continue
		}
		started = true
		parts = append(parts, line)
	}
	return proseSpaces.ReplaceAllString(strings.Join(parts, " "), " ")
}

// truncateProseRunes cuts text to at most max runes at a word boundary, appending an
// ellipsis. Cutting mid-word reads as a typo; cutting mid-rune corrupts the
// string, which is why the limit counts runes rather than bytes.
func truncateProseRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	cut := string(runes[:max])
	if i := strings.LastIndex(cut, " "); i > 0 {
		cut = cut[:i]
	}
	return strings.TrimSpace(cut) + "…"
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
