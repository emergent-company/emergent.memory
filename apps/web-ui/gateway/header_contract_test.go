package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"
)

// TestHeaderGroupMapsMirrorTheSidebar pins the header contract's first rule to
// its source of truth. `navGroupHref` and `sectionGroup` are derived from
// `sidebarGroups` rather than written down beside it, so a regrouped sidebar
// cannot leave a header pointing at a retired page — which is exactly what
// happened when #180 renamed Memory Browser → Memory and Settings → Project and
// a hand-written map kept the old vocabulary.
func TestHeaderGroupMapsMirrorTheSidebar(t *testing.T) {
	wantHref := map[string]string{}
	wantSection := map[string]string{}
	for _, g := range sidebarGroups("", true) {
		if g.Label == "" {
			// The ungrouped block: the absence of a group, not a group named "".
			if _, ok := navGroupHref[""]; ok {
				t.Errorf("navGroupHref has a %q key; the ungrouped block is not a group", "")
			}
			continue
		}
		if len(g.Items) == 0 {
			t.Fatalf("sidebar group %q has no items; a group's landing page is its first item", g.Label)
		}
		wantHref[g.Label] = g.Items[0].Href
		for _, it := range g.Items {
			wantSection[it.Label] = g.Label
		}
	}

	if len(navGroupHref) != len(wantHref) {
		t.Fatalf("navGroupHref has %d entries, sidebar has %d groups", len(navGroupHref), len(wantHref))
	}
	for label, href := range wantHref {
		if got := navGroupHref[label]; got != href {
			t.Errorf("navGroupHref[%q] = %q, want the group's landing page %q", label, got, href)
		}
	}

	if len(sectionGroup) != len(wantSection) {
		t.Fatalf("sectionGroup has %d entries, sidebar has %d items", len(sectionGroup), len(wantSection))
	}
	for item, group := range wantSection {
		if got := sectionGroup[item]; got != group {
			t.Errorf("sectionGroup[%q] = %q, want the group it is listed under %q", item, got, group)
		}
	}

	// Retired group labels (#180) must not come back as groups.
	for _, retired := range []string{"Memory Browser", "Organizations"} {
		if _, ok := navGroupHref[retired]; ok {
			t.Errorf("navGroupHref still carries the retired group %q", retired)
		}
	}
}

// TestHeaderGroupLiteralsAreSidebarGroups scans every page header's group
// argument — `pageHeader`/`pageHeaderWithMeta`'s first argument and
// `detailHeader`'s second. It must be a real sidebar group or "" (ungrouped),
// never a label the sidebar does not own: a stale label would otherwise render
// a first crumb with no destination.
//
// Detail headers normally inherit the group from their trail root and pass "",
// so this guards the call sites that do name one as well as the list pages.
func TestHeaderGroupLiteralsAreSidebarGroups(t *testing.T) {
	files, err := filepath.Glob("*.templ")
	if err != nil {
		t.Fatal(err)
	}
	pageHeaderCalls, detailHeaderCalls := 0, 0
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		text := string(src)
		for _, call := range []string{"@pageHeader(", "@pageHeaderWithMeta("} {
			for i := 0; ; {
				j := strings.Index(text[i:], call)
				if j < 0 {
					break
				}
				start := i + j + len(call)
				if args := topLevelArgs(text[start:]); len(args) > 0 {
					pageHeaderCalls++
					checkHeaderGroup(t, f, strings.TrimSuffix(call, "("), args[0])
				}
				i = start
			}
		}
		for i := 0; ; {
			j := strings.Index(text[i:], "@detailHeader(")
			if j < 0 {
				break
			}
			start := i + j + len("@detailHeader(")
			args := topLevelArgs(text[start:])
			if len(args) < 2 {
				t.Errorf("%s: detailHeader needs (crumbs, group, title, description); found %d argument(s)", f, len(args))
			} else {
				detailHeaderCalls++
				checkHeaderGroup(t, f, "@detailHeader", args[1])
			}
			i = start
		}
	}
	if pageHeaderCalls < 15 {
		t.Fatalf("parsed %d pageHeader calls across %d templates; the scan is not matching the code it is meant to guard", pageHeaderCalls, len(files))
	}
	if detailHeaderCalls < 10 {
		t.Fatalf("parsed %d detailHeader calls across %d templates; the scan is not matching the code it is meant to guard", detailHeaderCalls, len(files))
	}
}

// checkHeaderGroup asserts one header's group argument is a sidebar group or "".
func checkHeaderGroup(t *testing.T, file, call, arg string) {
	t.Helper()
	arg = strings.TrimSpace(arg)
	if !strings.HasPrefix(arg, `"`) || !strings.HasSuffix(arg, `"`) {
		// A computed group would slip past the map check, so name it rather than
		// skipping it silently.
		t.Errorf("%s: %s group argument %s is not a string literal; pass a sidebar group or \"\"", file, call, arg)
		return
	}
	group := strings.Trim(arg, `"`)
	if group == "" {
		return
	}
	if _, ok := navGroupHref[group]; !ok {
		t.Errorf("%s: %s group %q is not a sidebar group (want one of %v, or \"\" for an ungrouped page)", file, call, group, knownGroups())
	}
}

// topLevelArgs splits the text after a call's opening paren into its top-level
// arguments, so a comma inside a nested call or a struct literal does not split
// an argument.
func topLevelArgs(s string) []string {
	var args []string
	depth, start := 0, 0
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == '\\' && quote != '`' {
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'' || c == '`':
			quote = c
		case c == '(' || c == '[' || c == '{':
			depth++
		case c == ')' || c == ']' || c == '}':
			if depth == 0 {
				if last := strings.TrimSpace(s[start:i]); last != "" {
					args = append(args, last)
				}
				return args
			}
			depth--
		case c == ',' && depth == 0:
			args = append(args, strings.TrimSpace(s[start:i]))
			start = i + 1
		}
	}
	return args
}

// TestRunPageHeaderKeepsTrailLinks covers the origin-trail exception: the run
// page's trail describes where the page was reached from, so every crumb stays
// a link, nothing is marked active, and the run's status chip renders under the
// description (Meta) instead of in the action slot.
func TestRunPageHeaderKeepsTrailLinks(t *testing.T) {
	html := renderHTML(t, runPageHeader(&ScheduledAgentRun{
		AgentName: "nightly digest",
		AgentID:   "sched_1",
		Status:    "succeeded",
		StartedAt: "2026-11-06T10:00:00Z",
	}))

	for _, want := range []string{
		`href="/agents"`,            // the nav group comes first, as a link
		`href="/schedules"`,         // trail root stays a link
		`href="/schedules/sched_1"`, // trail leaf stays a link
	} {
		if !strings.Contains(html, want) {
			t.Errorf("run header missing %s in:\n%s", want, html)
		}
	}
	if strings.Contains(html, "font-medium text-base-content") {
		t.Errorf("origin trail must not mark a crumb active:\n%s", html)
	}
	meta := attrRegion(html, "data-header-meta")
	if !strings.Contains(meta, "succeeded") {
		t.Errorf("run status chip must render in Meta under the description, got meta region %q", meta)
	}
}

// TestObjectDetailFoldsTheGroupCrumb covers the fold-by-destination rule: the
// object trail root ("Objects" → /objects) is the Memory group's own landing
// page, so the header renders it once instead of Memory → Objects → the object.
func TestObjectDetailFoldsTheGroupCrumb(t *testing.T) {
	obj := &GraphObject{ID: "o1", CanonicalID: "o1", Type: "person", Key: "sam-lee"}
	html := renderHTML(t, ObjectDetailPage(obj, objectBranchRef{}, nil, nil, nil, "", nil, nil, nil, nil, nil, nil, "", 0, nil))

	items := breadcrumbItems(t, html)
	if len(items) != 2 {
		t.Fatalf("crumb count = %d, want 2 (the section root and the object):\n%s", len(items), html)
	}
	root := items[0]
	if got := breadcrumbText(root); got != "Objects" {
		t.Errorf("root crumb = %q, want the trail's own root Objects", got)
	}
	link := findElementByTag(root, "a")
	if link == nil {
		t.Fatalf("root crumb is not a link: %s", breadcrumbText(root))
	}
	if got := attrValue(link, "href"); got != "/objects" {
		t.Errorf("root href = %q, want the group's landing page /objects", got)
	}
	if findElementByTag(items[1], "a") != nil {
		t.Errorf("the object itself must stay the active, non-navigable crumb")
	}
}

// TestCollectionCountsSitBelowTheHeader pins where a count belongs after the
// header sweep: on the collection it counts — below the title, above the list —
// not in the header. Removing the old header badges without pinning the new
// location is how a count silently disappears.
func TestCollectionCountsSitBelowTheHeader(t *testing.T) {
	cases := []struct {
		name   string
		count  string
		marker string
	}{
		{"agents", "2 agents", "<table"},
		{"skills", "2 skills", "<table"},
		{"sessions", "2 sessions", `href="/sessions/`},
	}
	components := map[string]string{
		"agents":   renderHTML(t, AgentsPage([]AgentDefinitionSummary{{ID: "a1", Name: "diane"}, {ID: "a2", Name: "tom"}}, nil, "", nil, "")),
		"skills":   renderHTML(t, SkillsPage([]Skill{{Name: "summarise"}, {Name: "recall"}}, nil, nil, nil, newSkillProposalQuery("", ""), "", nil, "", nil)),
		"sessions": renderHTML(t, SessionsPage(nil, []Conversation{{ID: "c1", Title: "one"}, {ID: "c2", Title: "two"}}, nil)),
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			html := components[tc.name]
			iCount := strings.Index(html, tc.count)
			if iCount < 0 {
				t.Fatalf("missing count line %q in:\n%s", tc.count, html)
			}
			iTitle := strings.Index(html, "</h1>")
			if iTitle < 0 || iCount < iTitle {
				t.Errorf("count %q must render below the page title:\n%s", tc.count, html)
			}
			iList := strings.Index(html, tc.marker)
			if iList < 0 {
				t.Fatalf("missing collection marker %q in:\n%s", tc.marker, html)
			}
			if iCount > iList {
				t.Errorf("count %q must render above the collection it counts:\n%s", tc.count, html)
			}
			if meta := attrRegion(html, "data-header-meta"); strings.Contains(meta, tc.count) {
				t.Errorf("count %q is header material again: %q", tc.count, meta)
			}
		})
	}
}

// TestOneH1PerPage is the runtime half of the one-`h1` rule: a page in its
// error state renders exactly one `h1` — the header's when the page has one,
// the error heading when the error state is the page. A swapped-in region is
// held to the same rule from the other side: it must render none, because the
// page it lands in already owns the one.
func TestOneH1PerPage(t *testing.T) {
	cases := []struct {
		name string
		page templ.Component
		want int
	}{
		{"board (header then notice)", BoardPage(nil, nil, nil, boardAllowances{}, boardStatusMap{}, errTest), 1},
		{"agents list", AgentsPage(nil, nil, "", errTest, ""), 1},
		{"approvals", ApprovalsPage(nil, nil, errTest), 1},
		{"skills", SkillsPage(nil, errTest, nil, nil, newSkillProposalQuery("", ""), "", nil, "", nil), 1},
		{"sessions", SessionsPage(nil, nil, errTest), 1},
		{"usage", UsagePage(nil, nil, usageChartPayload{}, 0, false, 30, errTest), 1},
		{"backups", BackupsPage(nil, 0, nil, false, "", errTest, "", nil), 1},
		{"inbox (list region lowers the error)", InboxPage(inboxView{LoadErr: errTest}), 1},
		{"members", MembersPage(membersPageData{Base: "/members", LoadErr: errTest}), 1},
		{"project members", ProjectMembersPage(membersPageData{Base: "/settings/members", LoadErr: errTest}, false, true), 1},
		{"api tokens", APITokensPage(apiTokensPageData{LoadErr: errTest}), 1},
		{"account tokens", AccountTokensPage(apiTokensPageData{LoadErr: errTest}), 1},
		// The other shape: the error state *is* the page, so its heading is the
		// single h1.
		{"orgs list (error is the page)", OrgsPage(orgsPageData{LoadErr: errTest}), 1},
		// Swap regions: rendered into a page that already has its header.
		{"documents results", documentsResults(documentPageData{LoadErr: errTest}), 0},
		{"search results", searchResultsPanel(searchPageData{LoadErr: errTest}), 0},
		{"objects results", objectsBrowserResults(objectsPageData{LoadErr: errTest}), 0},
		{"inbox list region", inboxListRegion(inboxView{LoadErr: errTest}), 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			html := renderHTML(t, tc.page)
			if got := strings.Count(html, "<h1"); got != tc.want {
				t.Errorf("%d <h1> elements, want %d:\n%s", got, tc.want, html)
			}
		})
	}
}

// tmplBody is one `templ` declaration's body, keyed by the file it came from.
type tmplBody struct {
	file string
	name string
	body string
}

// TestNoTemplateRendersHeaderBeforePageError is the static half of the rule: a
// template that renders a shared header must not go on to render `pageError`.
// Its h1 is for a page whose error state *is* the page; a page that already
// rendered a header uses `pageErrorNotice`.
//
// Both sides resolve through callers AND callees: a page reaches its header
// through a wrapper such as `@boardHeader()`, and it reaches the error through a
// body such as `@inboxListRegion()`. Comparing only literal `@pageHeader(` /
// `@pageError(` text in the page's own body missed both shapes — the board page
// (header two calls away) and the inbox/members/tokens pages (error one call
// away) each slipped past a hand sweep. Branch-exclusive sites put the error
// first by convention, so "a header call before the call that lowers to
// pageError" is the regression this catches.
//
// Position alone is not enough (#206): an error call that comes first but is
// not mutually exclusive with the header still renders two <h1>s on the failing
// path. The pair must therefore also be separated by an `else` — see
// branchesExclusive.
func TestNoTemplateRendersHeaderBeforePageError(t *testing.T) {
	problems, checked := auditHeaderOrder(readTemplateBodies(t))
	for _, p := range problems {
		t.Error(p)
	}
	t.Logf("checked %d templates that render a header and lower to pageError", checked)
	if checked < 10 {
		t.Fatalf("checked %d templates that render a header and lower to pageError; the scan is not matching the code it guards", checked)
	}
}

// readTemplateBodies parses every *.templ file in the gateway into one body per
// templ declaration.
func readTemplateBodies(t *testing.T) []tmplBody {
	t.Helper()
	files, err := filepath.Glob("*.templ")
	if err != nil {
		t.Fatal(err)
	}
	decl := regexp.MustCompile(`(?m)^templ (\w+)\(`)
	var bodies []tmplBody
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		text := string(src)
		locs := decl.FindAllStringSubmatchIndex(text, -1)
		for i, m := range locs {
			end := len(text)
			if i+1 < len(locs) {
				end = locs[i+1][0]
			}
			bodies = append(bodies, tmplBody{file: f, name: text[m[2]:m[3]], body: text[m[1]:end]})
		}
	}
	return bodies
}

// auditHeaderOrder returns one message per template that breaks the static
// one-`h1` rule, plus how many template pairs it actually checked (so a scan
// that stops matching the code fails loudly instead of going quiet).
//
// A pair is broken when the header call precedes the call that lowers to
// pageError, or when the error call comes first but is not the exclusive other
// arm of the same `if` as the header.
func auditHeaderOrder(bodies []tmplBody) (problems []string, checked int) {
	callRe := regexp.MustCompile(`@(\w+)\(`)
	calls := func(body string) []string {
		var out []string
		for _, m := range callRe.FindAllStringSubmatch(body, -1) {
			out = append(out, m[1])
		}
		return out
	}
	// rendersHeader / lowersToPageError: the leaves, plus every template that
	// reaches one through a call (fixed point over the call graph).
	rendersHeader := map[string]bool{
		"pageHead": true, "pageHeader": true, "pageHeaderWithMeta": true, "detailHeader": true,
	}
	lowersToPageError := map[string]bool{"pageError": true}
	for changed := true; changed; {
		changed = false
		for i := range bodies {
			b := &bodies[i]
			for _, callee := range calls(b.body) {
				if rendersHeader[callee] && !rendersHeader[b.name] {
					rendersHeader[b.name] = true
					changed = true
				}
				if lowersToPageError[callee] && !lowersToPageError[b.name] {
					lowersToPageError[b.name] = true
					changed = true
				}
			}
		}
	}

	// firstAt returns the earliest call to any callee in the set, so position is
	// measured at the call site in this body rather than inside a callee.
	firstAt := func(body string, reachable map[string]bool) int {
		first := -1
		for _, callee := range calls(body) {
			if !reachable[callee] {
				continue
			}
			if at := strings.Index(body, "@"+callee+"("); at >= 0 && (first < 0 || at < first) {
				first = at
			}
		}
		return first
	}

	for i := range bodies {
		b := &bodies[i]
		if !rendersHeader[b.name] || !lowersToPageError[b.name] {
			continue
		}
		firstHeader, firstError := firstAt(b.body, rendersHeader), firstAt(b.body, lowersToPageError)
		if firstHeader < 0 || firstError < 0 {
			continue // the pair only coexists through a shared callee, not in this body
		}
		checked++
		if firstHeader < firstError {
			problems = append(problems, b.file+": "+b.name+" renders a page header and then something that lowers to pageError; use pageErrorNotice so the page keeps one <h1>")
			continue
		}
		// firstError == firstHeader when one shared shell both renders the header
		// and lowers to pageError; that call's own body carries the pair and is
		// audited on its own iteration.
		if firstError < firstHeader && !branchesExclusive(b.body[firstError:firstHeader]) {
			problems = append(problems, b.file+": "+b.name+" renders something that lowers to pageError and then a page header with no `else` between them; the error branch falls through to the header, so a failing page renders two <h1>s")
		}
	}
	return problems, checked
}

// elseKeyword matches the `else` keyword, not the letters inside an identifier.
var elseKeyword = regexp.MustCompile(`\belse\b`)

// branchesExclusive reports whether the segment between an error-lowering call
// and the header call proves the two are alternative arms of one `if`: the
// error call's enclosing block must close and continue with `else`.
//
// The search starts after the error call's own closing parenthesis, so an error
// message that merely contains the word "else" cannot satisfy it.
func branchesExclusive(segment string) bool {
	end := endOfCall(segment)
	return end >= 0 && elseKeyword.MatchString(segment[end:])
}

// endOfCall returns the offset just past the parenthesis closing the call at the
// start of s (s[0] == '@'), or -1. String and rune literals are skipped so a
// parenthesis inside an argument does not end the call early.
func endOfCall(s string) int {
	open := strings.IndexByte(s, '(')
	if open < 0 {
		return -1
	}
	depth := 0
	var quote byte
	for i := open; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == '\\' && quote != '`' {
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'' || c == '`':
			quote = c
		case c == '(':
			depth++
		case c == ')':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return -1
}

// TestHeaderOrderAuditSeesNonExclusiveBranches pins the hole from #206. These
// fixtures are the same shape the scan runs over the real templates, so a
// reverted exclusivity check fails here rather than only in a code review. The
// first case is exactly the shape that passed the old position-only guard: the
// error call is first, but nothing stops the page from also rendering its
// header.
func TestHeaderOrderAuditSeesNonExclusiveBranches(t *testing.T) {
	cases := []struct {
		name          string
		body          string
		wantViolation bool
	}{
		{
			name: "error first but header is not in an else branch",
			body: `
	if loadErr != nil {
		@pageError("Broken unavailable", loadErr)
	}
	@pageHeader("Work", "Broken", "Broken")`,
			wantViolation: true,
		},
		{
			// The word "else" in the error message must not count as the branch
			// keyword; the page still renders two h1s when the load fails.
			name: "error message contains else but there is no else branch",
			body: `
	if loadErr != nil {
		@pageError("Nothing here, try something else", loadErr)
	}
	@pageHeader("Work", "Broken", "Broken")`,
			wantViolation: true,
		},
		{
			name: "header before the error is still a violation",
			body: `
	@pageHeader("Work", "Broken", "Broken")
	@pageError("Broken unavailable", loadErr)`,
			wantViolation: true,
		},
		{
			name: "error and header are arms of one if",
			body: `
	if loadErr != nil {
		@pageError("Broken unavailable", loadErr)
	} else {
		@pageHeader("Work", "Broken", "Broken")
	}`,
			wantViolation: false,
		},
		{
			name: "error and header are arms of one if/else-if chain",
			body: `
	if loadErr != nil {
		@pageError("Broken unavailable", loadErr)
	} else if !canEdit {
		@pageHeader("Work", "Broken", "Broken")
	}`,
			wantViolation: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			problems, checked := auditHeaderOrder([]tmplBody{{file: "fixture.templ", name: "FixturePage", body: tc.body}})
			if checked != 1 {
				t.Fatalf("checked = %d, want 1; the fixture is not exercising the pair", checked)
			}
			if got := len(problems) > 0; got != tc.wantViolation {
				t.Errorf("violation = %v, want %v (problems: %v)", got, tc.wantViolation, problems)
			}
		})
	}
}

// attrRegion returns the opening element carrying attr and the markup up to its
// first closing tag — enough to inspect what a header's Meta row contains.
func attrRegion(html, attr string) string {
	i := strings.Index(html, attr)
	if i < 0 {
		return ""
	}
	end := strings.Index(html[i:], "</div>")
	if end < 0 {
		return html[i:]
	}
	return html[i : i+end]
}

func knownGroups() []string {
	out := make([]string, 0, len(navGroupHref))
	for k := range navGroupHref {
		out = append(out, k)
	}
	// small map: insertion sort keeps the helper dependency-free
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
