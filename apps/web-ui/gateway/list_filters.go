package main

import (
	"net/http"
	"strings"

	"github.com/a-h/templ"
	"github.com/emergent-company/go-daisy/render"
)

// List surfaces whose results a filter form replaces in place. The id is both
// the region's element id and the HX-Target the filter request answers to (see
// targetsResultsRegion).
//
// A list filter must never be a document navigation: it changes the rows in one
// region, not the page. The shell rides on <main hx-boost="true">, so a form
// submit is AJAX already — but a control that calls form.submit() (as the
// filters used to) never dispatches a `submit` event, so htmx never sees it and
// the browser does a full document load. Wiring the filters as htmx requests to
// the region is the fix; docs/ui-patterns.md §3 is the contract.
const (
	objectsResultsRegion   = "objects-results"
	documentsResultsRegion = "documents-results"
	searchResultsRegion    = "search-results"
	inboxResultsRegion     = "inbox-list"
)

// targetsResultsRegion reports whether this request is an HTMX GET aimed at a
// list view's results region rather than at the page itself.
//
// A filter form requests the *page* URL (hx-get="/objects", never a /partial
// route) so the URL htmx pushes into history reloads, bookmarks, and shares
// correctly; the server then chooses between the whole page and the region from
// the HX-Target header. htmx v4 sends that header as "<tag>#<id>" — hence the
// suffix match — and a full browser load sends none at all.
func targetsResultsRegion(r *http.Request, region string) bool {
	target := strings.TrimSpace(render.HXTarget(r))
	if target == "" {
		return false
	}
	return target == region || strings.HasSuffix(target, "#"+region)
}

// objectsSearchFormSource is the HX-Source selector of the objects search form.
// A surface with two toolbar forms needs it to tell which one asked: the search
// form changes the browse/search mode, which rewrites the filter row, while the
// filter form does not.
const objectsSearchFormSource = "form#objects-search-form"

// hxSource returns the element that issued an HTMX request, as htmx v4 reports
// it in HX-Source ("form#objects-search-form", "a#sort-created", …). Empty on a
// full load. go-daisy's render package mirrors HX-Target/HX-Request but not this
// one, so it is read here once rather than at each call site.
func hxSource(r *http.Request) string {
	return strings.TrimSpace(r.Header.Get("HX-Source"))
}

// regionFilterAttrs is the htmx wiring shared by every list filter form: the
// request carries the form's own values to the page URL, the response replaces
// one results region, and the URL is pushed so the address bar, a reload, and
// back/forward all keep the applied filters.
//
// hx-target and hx-swap are explicit on purpose. The shell's <main> carries
// hx-target="#main-content" and hx-swap="innerHTML", and htmx propagates those
// attributes to descendants (implicit inheritance): without an explicit target,
// a boosted filter form swaps the whole page body back in instead of the list.
func regionFilterAttrs(action, region string) templ.Attributes {
	return templ.Attributes{
		"hx-get":      action,
		"hx-target":   "#" + region,
		"hx-swap":     "outerHTML",
		"hx-push-url": "true",
	}
}

// filterFormAttrs applies a filter form as its controls are picked (selects,
// radios) and on submit. The submit trigger is not redundant: the objects actor
// picker is a menu of submit buttons, and a filter form also has to keep working
// for a keyboard user pressing Enter.
func filterFormAttrs(action, region string) templ.Attributes {
	attrs := regionFilterAttrs(action, region)
	attrs["hx-trigger"] = "change, submit"
	return attrs
}

// selectFilterFormAttrs applies a form whose controls change on pick but which
// also carries a free-text field: only a <select>'s change may fire a request,
// so editing the text and tabbing away does not search on the user's behalf.
//
// `target:<css>` is htmx v4's trigger-spec filter on the event target (the
// handler runs only when the changed element matches). The submit spec is
// separate, so it stays unfiltered.
func selectFilterFormAttrs(action, region string) templ.Attributes {
	attrs := regionFilterAttrs(action, region)
	attrs["hx-trigger"] = "change target:select, submit"
	return attrs
}

// submitFilterFormAttrs applies a search form only when it is submitted. A
// search runs on Enter or the button, never while the user is typing.
func submitFilterFormAttrs(action, region string) templ.Attributes {
	attrs := regionFilterAttrs(action, region)
	attrs["hx-trigger"] = "submit"
	return attrs
}
