package components

// guard: the class-hook wrappers' merge and go-daisy parity — every test in
// this file must gate every PR, so all of them end in `Ratchet`.

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/a-h/templ"
	ui "github.com/emergent-company/go-daisy/components/ui"
)

// badge_alert_class_test.go pins the contract of the two class-hook wrappers
// (#855): a caller's Class lands in the element's own class attribute, exactly
// once, and — because the wrappers reproduce upstream markup — with Class empty
// they render byte-identically to the go-daisy primitive they stand in for.
//
// The assertions name attribute multiplicity and utility tokens rather than
// class order (ui-rule-test-the-contract): a duplicate class attribute is the
// defect being fixed, and "the token asked for is present in the single class
// attribute" is the whole claim. The parity tests are what keep the copy honest.
//
// Every test in this file ends in `Ratchet` because the required `ci` job runs
// only `go test -run Ratchet ./...` for this module (ci.yml, "Consistency
// ratchets"): class_attr_ratchet_test.go stops a new "class" literal, but it
// cannot notice this merge disappearing from the wrappers, and the parity tests
// are the half that catches a go-daisy pin bump that drops twmerge. All of them
// must run on the PR, not only at promotion (#910). The `// guard:` marker above
// declares the file a guard, and TestGuardTestNamingConventionRatchet enforces
// both halves of that convention.

// rootTag returns the first element's opening tag, from "<tag" through ">".
func rootTag(t *testing.T, html string) string {
	t.Helper()
	open := strings.Index(html, "<")
	if open < 0 {
		t.Fatalf("no element in rendered output: %q", html)
	}
	close := strings.Index(html[open:], ">")
	if close < 0 {
		t.Fatalf("unterminated opening tag in rendered output: %q", html)
	}
	return html[open : open+close+1]
}

// classAttrCount counts class attributes in one opening tag.
func classAttrCount(tag string) int {
	return strings.Count(tag, " class=")
}

// rootClass returns the value of the tag's single class attribute.
func rootClass(t *testing.T, tag string) string {
	t.Helper()
	if n := classAttrCount(tag); n != 1 {
		t.Fatalf("want exactly one class attribute on the root, got %d: %s", n, tag)
	}
	const marker = ` class="`
	start := strings.Index(tag, marker)
	if start < 0 {
		t.Fatalf("no class attribute: %s", tag)
	}
	rest := tag[start+len(marker):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		t.Fatalf("unterminated class attribute: %s", tag)
	}
	return rest[:end]
}

func hasClassToken(class, token string) bool {
	for _, f := range strings.Fields(class) {
		if f == token {
			return true
		}
	}
	return false
}

func TestBadgeClassMergeRatchet(t *testing.T) {
	html := renderHTML(t, BadgeWithClass(BadgeWithClassProps{
		BadgeProps: ui.BadgeProps{
			Label:   "3",
			Variant: ui.BadgeError,
			Size:    ui.BadgeSizeXS,
			Attrs:   templ.Attributes{"data-testid": "documents-problem-badge"},
		},
		Class: "ml-1",
	}))

	tag := rootTag(t, html)
	class := rootClass(t, tag)
	if !hasClassToken(class, "ml-1") {
		t.Errorf("class %q is missing the caller utility ml-1", class)
	}
	if !hasClassToken(class, "badge") || !hasClassToken(class, "badge-error") || !hasClassToken(class, "badge-xs") {
		t.Errorf("class %q lost one of the badge's own classes", class)
	}
	assertContains(t, tag, `data-testid="documents-problem-badge"`)
}

func TestBadgeHrefClassMergeRatchet(t *testing.T) {
	html := renderHTML(t, BadgeWithClass(BadgeWithClassProps{
		BadgeProps: ui.BadgeProps{Label: "run", Variant: ui.BadgeGhost, Href: "/runs/1", DirectLabel: true},
		Class:      "hover:opacity-70",
	}))

	tag := rootTag(t, html)
	if !strings.HasPrefix(tag, "<a ") {
		t.Fatalf("want the <a href> variant, got %s", tag)
	}
	class := rootClass(t, tag)
	if !hasClassToken(class, "hover:opacity-70") {
		t.Errorf("class %q is missing the caller utility", class)
	}
}

func TestAlertClassMergeRatchet(t *testing.T) {
	html := renderHTML(t, AlertWithClass(AlertWithClassProps{
		AlertProps: ui.AlertProps{
			Type:    ui.AlertError,
			Message: "boom",
			Attrs:   templ.Attributes{"data-testid": "platform-email-error"},
		},
		Class: "mb-4",
	}))

	tag := rootTag(t, html)
	class := rootClass(t, tag)
	if !hasClassToken(class, "mb-4") {
		t.Errorf("class %q is missing the caller utility mb-4", class)
	}
	if !hasClassToken(class, "alert") || !hasClassToken(class, "alert-error") {
		t.Errorf("class %q lost one of the alert's own classes", class)
	}
	if !strings.Contains(tag, `role="alert"`) {
		t.Errorf("alert lost its role: %s", tag)
	}
	assertContains(t, tag, `data-testid="platform-email-error"`)
}

// classAttrRE matches every class attribute value in a rendered fragment.
var classAttrRE = regexp.MustCompile(`class="([^"]*)"`)

// normalizeClasses rewrites every class attribute value to its sorted token
// list. Class *order* is not the contract (ui-rule-test-the-contract): the
// wrappers merge through twmerge, which resolves conflicts and may reorder the
// component's own utilities, so parity is asserted over the token set plus the
// rest of the markup byte-for-byte.
func normalizeClasses(html string) string {
	return classAttrRE.ReplaceAllStringFunc(html, func(attr string) string {
		value := classAttrRE.FindStringSubmatch(attr)[1]
		tokens := strings.Fields(value)
		sort.Strings(tokens)
		return `class="` + strings.Join(tokens, " ") + `"`
	})
}

// TestBadgeAndAlertFoldAClassLeftInAttrs pins the read-delete-merge shape: a
// caller that still passes "class" through Attrs (the pre-#855 shape, e.g. a
// site converted later) gets the utility applied once instead of dropped.
func TestBadgeAndAlertFoldAClassLeftInAttrsRatchet(t *testing.T) {
	attrs := templ.Attributes{"class": "ml-1", "data-testid": "folded"}

	badge := renderHTML(t, BadgeWithClass(BadgeWithClassProps{
		BadgeProps: ui.BadgeProps{Label: "3", Variant: ui.BadgeError, Attrs: attrs},
	}))
	tag := rootTag(t, badge)
	if class := rootClass(t, tag); !hasClassToken(class, "ml-1") {
		t.Errorf("badge class %q is missing the Attrs utility ml-1", class)
	}
	assertContains(t, tag, `data-testid="folded"`)

	alert := renderHTML(t, AlertWithClass(AlertWithClassProps{
		AlertProps: ui.AlertProps{Type: ui.AlertSuccess, Message: "saved", Attrs: attrs},
	}))
	tag = rootTag(t, alert)
	if class := rootClass(t, tag); !hasClassToken(class, "ml-1") {
		t.Errorf("alert class %q is missing the Attrs utility ml-1", class)
	}
	assertContains(t, tag, `data-testid="folded"`)

	// The caller's map is copied, not mutated.
	if _, ok := attrs["class"]; !ok {
		t.Error("the caller's Attrs map lost its class key")
	}
}

// TestTakeClassAttrFoldsEveryShapeTemplRendersRatchet pins the class values
// that can arrive through templ.Attributes: the shapes templ itself renders as
// class text are folded, anything else is dropped. The key never survives,
// whatever the type — a second class attribute is the defect (#855), so an
// unfashionable value must not get one.
func TestTakeClassAttrFoldsEveryShapeTemplRendersRatchet(t *testing.T) {
	str := "ml-1"
	for _, tc := range []struct {
		name  string
		value any
		want  string
	}{
		{"string", "ml-1", "ml-1"},
		{"string pointer", &str, "ml-1"},
		{"nil string pointer", (*string)(nil), ""},
		{"key-value on", templ.KV("mb-4", true), "mb-4"},
		{"key-value off", templ.KV("mb-4", false), ""},
		{"non-string", 5, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			attrs := templ.Attributes{"class": tc.value, "data-testid": "kept"}
			got, class := takeClassAttr(attrs)
			if class != tc.want {
				t.Fatalf("class = %q, want %q", class, tc.want)
			}
			if _, ok := got["class"]; ok {
				t.Errorf("class key survived takeClassAttr: %v", got)
			}
			if got["data-testid"] != "kept" {
				t.Errorf("unrelated attribute lost: %v", got)
			}
		})
	}
}

// TestBadgeNeverRendersASecondClassAttributeForAnUnfoldableValueRatchet is the
// render half of the test above: an int class value is dropped, not emitted
// beside the badge's own class attribute.
func TestBadgeNeverRendersASecondClassAttributeForAnUnfoldableValueRatchet(t *testing.T) {
	html := renderHTML(t, BadgeWithClass(BadgeWithClassProps{
		BadgeProps: ui.BadgeProps{Label: "3", Variant: ui.BadgeError, Attrs: templ.Attributes{"class": 5}},
	}))
	tag := rootTag(t, html)
	if n := classAttrCount(tag); n != 1 {
		t.Fatalf("want exactly one class attribute, got %d: %s", n, tag)
	}
}

// TestStatusBadgeFoldsClassFromAttrs pins the coordination with StatusBadge:
// StatusBadgeOpts.Attrs still accepts a class (the pre-#855 shape) and the badge
// applies it once, because StatusBadge renders through BadgeWithClass. Without
// the fold the shape would recreate #855 one call frame down.
func TestStatusBadgeFoldsClassFromAttrsRatchet(t *testing.T) {
	html := renderHTML(t, StatusBadge("Pending", ui.BadgeWarning, "", StatusBadgeOpts{
		Attrs: templ.Attributes{"class": "ml-1", "data-testid": "pending-badge"},
	}))

	tag := rootTag(t, html)
	class := rootClass(t, tag)
	if !hasClassToken(class, "ml-1") {
		t.Errorf("class %q is missing the Attrs utility ml-1", class)
	}
	if !hasClassToken(class, "badge") || !hasClassToken(class, "badge-warning") {
		t.Errorf("class %q lost one of the badge's own classes", class)
	}
	assertContains(t, tag, `data-testid="pending-badge"`)
}

// TestBadgeMatchesUpstreamWithoutClassRatchet is the anti-drift guard for the
// Badge copy: every prop shape renders the same markup as ui.Badge when Class
// is empty.
func TestBadgeMatchesUpstreamWithoutClassRatchet(t *testing.T) {
	cases := map[string]ui.BadgeProps{
		"bare":         {Label: "hi"},
		"intent+size":  {Label: "hi", Variant: ui.BadgeSuccess, Size: ui.BadgeSizeSM},
		"style":        {Label: "hi", Variant: ui.BadgeWarning, Style: ui.BadgeStyleSoft, Size: ui.BadgeSizeXS},
		"dot":          {Label: "hi", Variant: ui.BadgeError, Dot: true, Animate: true},
		"glyph":        {Label: "hi", Glyph: "⚑"},
		"icon":         {Label: "hi", Icon: "lucide--radio"},
		"max-width":    {Label: "a very long label", MaxWidth: "max-w-20"},
		"position":     {Label: "4", Position: "indicator-item indicator-top indicator-end"},
		"direct label": {Label: ">5", DirectLabel: true, LabelClass: "tabular-nums"},
		"colour":       {Label: "hi", Color: "oklch(60% 0.2 20)", LabelClass: "text-base-content"},
		"href":         {Label: "run", Variant: ui.BadgeGhost, Href: "/runs/1", DirectLabel: true},
		"attrs":        {Label: "hi", Attrs: templ.Attributes{"data-testid": "x", "title": "t"}},
		"everything at once": {
			Label: "hi", Variant: ui.BadgeInfo, Style: ui.BadgeStyleOutline, Size: ui.BadgeSizeMD,
			Icon: "lucide--circle-check", MaxWidth: "max-w-24", Href: "/x", Position: "indicator-item",
			Attrs: templ.Attributes{"data-x": "1"},
		},
	}

	for name, props := range cases {
		t.Run(name, func(t *testing.T) {
			want := normalizeClasses(renderHTML(t, ui.Badge(props)))
			got := normalizeClasses(renderHTML(t, BadgeWithClass(BadgeWithClassProps{BadgeProps: props})))
			if got != want {
				t.Fatalf("wrapper diverged from ui.Badge\n got: %s\nwant: %s", got, want)
			}
		})
	}
}

// TestAlertMatchesUpstreamWithoutClassRatchet is the anti-drift guard for the
// Alert copy.
func TestAlertMatchesUpstreamWithoutClassRatchet(t *testing.T) {
	cases := map[string]ui.AlertProps{
		"bare":          {Message: "hi"},
		"type+style":    {Type: ui.AlertSuccess, Style: ui.AlertStyleSoft, Message: "saved"},
		"icon":          {Type: ui.AlertError, Icon: "lucide--triangle-alert", Message: "boom"},
		"body":          {Type: ui.AlertInfo, Body: templ.Raw("<strong>composed</strong>")},
		"attrs":         {Type: ui.AlertWarning, Message: "careful", Attrs: templ.Attributes{"data-testid": "warn"}},
		"type only":     {Type: ui.AlertError, Message: "boom"},
		"message only":  {Message: "plain"},
		"style only":    {Style: ui.AlertStyleOutline, Message: "outline"},
		"icon and body": {Type: ui.AlertSuccess, Icon: "lucide--check", Body: templ.Raw("done")},
	}

	for name, props := range cases {
		t.Run(name, func(t *testing.T) {
			want := normalizeClasses(renderHTML(t, ui.Alert(props)))
			got := normalizeClasses(renderHTML(t, AlertWithClass(AlertWithClassProps{AlertProps: props})))
			if got != want {
				t.Fatalf("wrapper diverged from ui.Alert\n got: %s\nwant: %s", got, want)
			}
		})
	}
}
