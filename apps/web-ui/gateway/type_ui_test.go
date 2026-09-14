package main

import (
	"encoding/json"
	"strings"
	"testing"

	ui "github.com/emergent-company/go-daisy/components/ui"
)

func TestTypeIconClass(t *testing.T) {
	cases := []struct {
		name string
		icon string
		want string
	}{
		{"empty", "", "lucide--box"},
		{"blank", "   ", "lucide--box"},
		{"bare pascal", "FileText", "lucide--file-text"},
		{"bare pascal box", "Box", "lucide--box"},
		{"bare kebab", "git-branch", "lucide--git-branch"},
		{"bare snake", "file_text", "lucide--file-text"},
		{"bare camel", "fileText", "lucide--file-text"},
		{"iconify", "lucide--file-text", "lucide--file-text"},
		{"iconify pascal", "lucide--FileText", "lucide--file-text"},
		{"colon form", "lucide:FileText", "lucide--file-text"},
		{"unknown name", "DefinitelyNotAnIcon", "lucide--box"},
		{"arbitrary iconify", "icon-[lucide--user]", "lucide--box"},
		{"emoji", "📝", ""},
		{"symbol glyph", "✓", ""},
	}
	for _, c := range cases {
		if got := typeIconClass(c.icon); got != c.want {
			t.Errorf("%s: typeIconClass(%q) = %q, want %q", c.name, c.icon, got, c.want)
		}
	}
}

func TestNormalizeIconName(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"FileText", "file-text"},
		{"git-branch", "git-branch"},
		{"file_text", "file-text"},
		{"lucide--map-pin", "map-pin"},
		{"lucide:MapPin", "map-pin"},
		{"  AlertTriangle  ", "alert-triangle"},
		{"", ""},
		{"icon-[lucide--user]", ""},
		{"bad!name", ""},
	}
	for _, c := range cases {
		if got := normalizeIconName(c.in); got != c.want {
			t.Errorf("normalizeIconName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestObjectTypeUIMap(t *testing.T) {
	types := []CompiledType{
		{Name: "note", UI: json.RawMessage(`{"icon":"📝","color":"#4F46E5"}`)},
		{Name: "task", UI: json.RawMessage(`{"icon":"lucide--check-square"}`)},
		{Name: "event", UI: json.RawMessage(`{"color":"#7C3AED"}`)},
		{Name: "person"}, // no ui block
		{Name: "broken", UI: json.RawMessage(`not json`)},
		{Name: "blank", UI: json.RawMessage(`{}`)},
	}
	m := objectTypeUIMap(types)
	want := map[string]typeUI{
		"note":  {Icon: "📝", Color: "#4F46E5"},
		"task":  {Icon: "lucide--check-square"},
		"event": {Color: "#7C3AED"},
	}
	if len(m) != len(want) {
		t.Fatalf("map = %v, want %v", m, want)
	}
	for name, ui := range want {
		if got := m[name]; got != ui {
			t.Errorf("map[%q] = %+v, want %+v", name, got, ui)
		}
	}

	// no declared ui anywhere → nil map
	if got := objectTypeUIMap([]CompiledType{{Name: "person"}}); got != nil {
		t.Errorf("no declarations = %v, want nil", got)
	}
	if got := objectTypeUIMap(nil); got != nil {
		t.Errorf("nil types = %v, want nil", got)
	}
}

func TestTypeUIOf(t *testing.T) {
	if got := typeUIOf(nil, "note"); got != (typeUI{}) {
		t.Errorf("nil map = %+v, want zero typeUI", got)
	}
	m := map[string]typeUI{"note": {Icon: "📝", Color: "#4F46E5"}}
	if got := typeUIOf(m, "note"); got.Icon != "📝" || got.Color != "#4F46E5" {
		t.Errorf("present = %+v, want declared ui", got)
	}
	if got := typeUIOf(m, "missing"); got != (typeUI{}) {
		t.Errorf("missing = %+v, want zero typeUI", got)
	}
}

func TestTypeColorStyle(t *testing.T) {
	if got := typeColorStyle(""); got != "" {
		t.Errorf("empty = %q, want empty", got)
	}
	if got := typeColorStyle("  "); got != "" {
		t.Errorf("blank = %q, want empty", got)
	}
	want := "color:#4F46E5;background-color:color-mix(in oklch,#4F46E5 10%,transparent);border-color:color-mix(in oklch,#4F46E5 15%,transparent)"
	if got := typeColorStyle("#4F46E5"); got != want {
		t.Errorf("hex = %q, want %q", got, want)
	}
	// any CSS color value passes through (surrounding whitespace trimmed)
	if got := typeColorStyle("  purple  "); !strings.Contains(got, "color:purple") {
		t.Errorf("name = %q, want color:purple inside", got)
	}
}

func TestSupportedIconPickerOptions(t *testing.T) {
	opts := supportedIconPickerOptions()

	// Derived from the catalog minus the reserved box default: the picker can
	// only offer icons Tailwind actually compiles.
	if want := len(supportedTypeIconClasses) - 1; len(opts) != want {
		t.Fatalf("options = %d, want %d (catalog minus box)", len(opts), want)
	}
	seen := map[string]bool{}
	for i, o := range opts {
		if o.Name == "" || o.Class == "" || o.Label == "" {
			t.Errorf("option %d incomplete: %+v", i, o)
		}
		if o.Class != "lucide--"+o.Name {
			t.Errorf("option %d class/name mismatch: %+v", i, o)
		}
		if o.Name == "box" {
			t.Errorf("box must be reserved as the default, got %+v", o)
		}
		if seen[o.Name] {
			t.Errorf("duplicate option %q", o.Name)
		}
		seen[o.Name] = true
		if got := typeIconClass(o.Name); got != o.Class {
			t.Errorf("option %q resolves to %q, want %q", o.Name, got, o.Class)
		}
	}
	if opts[0].Name != "file-text" || opts[0].Label != "File Text" {
		t.Errorf("first option = %+v, want file-text / File Text", opts[0])
	}
}

func TestRenderSchemaIconPicker(t *testing.T) {
	// The data-gd-* attributes below are the runtime contract: the bundled
	// go-daisy-icon-picker.js resolves every one of them via querySelector
	// (root scope, panel, search, options, commit target/label/glyph, reset and
	// empty state), and the popover runtime needs its trigger/content pair to
	// open the panel at all. Dropping any one silently no-ops an interaction —
	// a missing data-gd-icon-panel made both commit and search dead while the
	// popover still opened, which is exactly the regression this guards.
	html := renderHTML(t, SchemaObjectTypeEditPage(&SchemaEditView{
		Detail: &ObjectTypeDetail{Name: "person"},
		Icon:   "lucide--file-text",
	}))
	for _, want := range []string{
		`data-gd-icon-picker`,     // rootOf() scopes the delegated runtime
		`data-gd-icon-panel`,      // panelOf() — the hook the fix restored
		`data-gd-icon-search`,     // filter() search box
		`data-gd-icon-input`,      // hidden input commit() writes into
		`data-gd-icon-option`,     // options() selectable tiles
		`data-gd-icon-reset`,      // reset-to-default control
		`data-gd-icon-empty`,      // filter()'s no-match state
		`data-gd-icon-glyph`,      // trigger glyph commit() replaces
		`data-gd-icon-label`,      // trigger label commit() rewrites
		`data-gd-icon-value`,      // committed option value
		`data-gd-icon-class`,      // option/trigger iconify class
		`data-gd-popover-content`, // popover runtime panel
		`data-gd-popover-trigger`, // popover runtime trigger
		`id="object-type-icon"`,   // hidden input POST contract: id
		`name="icon"`,             // hidden input POST contract: field name
		`aria-haspopup="listbox"`,
		`id="object-type-icon-panel"`,    // popover panel
		`id="object-type-icon-search"`,   // search box
		`role="listbox"`,                 //
		`data-gd-icon-value="file-text"`, // catalog tile (selected)
		`data-gd-icon-class="lucide--file-text"`,
		`data-gd-icon-label="File Text"`,
		`aria-selected="true"`,
		`bg-primary/10`,           // selected tile highlight
		`go-daisy-popover.js`,     // popover runtime
		`go-daisy-icon-picker.js`, // picker runtime
	} {
		if !strings.Contains(html, want) {
			t.Errorf("icon picker markup missing %q", want)
		}
	}
	// The go-daisy picker runtime owns search/keyboard/commit now; the retired
	// app.js hooks (all under data-icon-*) must not linger in the markup.
	if strings.Contains(html, "data-icon-") {
		t.Errorf("legacy local icon-picker hooks still present in:\n%s", html)
	}
	// default state selects the default tile and hides the reset footer
	def := renderHTML(t, SchemaObjectTypeEditPage(&SchemaEditView{Detail: &ObjectTypeDetail{Name: "person"}}))
	for _, want := range []string{`data-gd-icon-default="1"`, `data-gd-icon-value=""`, `data-gd-icon-label="Default icon"`, "display:none"} {
		if !strings.Contains(def, want) {
			t.Errorf("default icon picker missing %q", want)
		}
	}
}

func TestRenderSchemaColorPicker(t *testing.T) {
	html := renderHTML(t, SchemaObjectTypeEditPage(&SchemaEditView{
		Detail: &ObjectTypeDetail{Name: "person"},
		Color:  "#4F46E5",
	}))
	for _, want := range []string{
		`data-gd-color-picker`,   // go-daisy picker root
		`id="object-type-color"`, // real named text input keeps the POST contract
		`data-gd-color-text`,
		`name="color"`,
		`value="#4F46E5"`, // current value preserved
		`data-gd-color-swatch`,
		`data-gd-color-clear`, // AllowEmpty renders a clear affordance
		`data-gd-color-preset`,
		`_gdColorPickerInit`, // inline sync runtime
	} {
		if !strings.Contains(html, want) {
			t.Errorf("color picker markup missing %q", want)
		}
	}
}

// The Appearance card must separate the icon and color controls on both the
// stacked (mobile) and side-by-side (sm:) layouts; this guards the reported
// "fields touching" regression.
func TestRenderAppearanceFieldSpacing(t *testing.T) {
	html := renderHTML(t, SchemaObjectTypeEditPage(&SchemaEditView{Detail: &ObjectTypeDetail{Name: "person"}}))
	// The grid gutters plus a flex-column cell that lets each fieldset stretch
	// keep the two fields evenly spaced and equal-height.
	for _, want := range []string{`gap-x-8 gap-y-6`, `flex min-w-0 flex-col`, `[&>fieldset]:grow`} {
		if !strings.Contains(html, want) {
			t.Errorf("appearance layout missing spacing hook %q", want)
		}
	}
}

func TestRenderTypeIconTile(t *testing.T) {
	// no declaration → the generic primary box tile (today's row look)
	html := renderHTML(t, typeIconTile("", ""))
	for _, want := range []string{"lucide--box", "bg-primary/10", "size-9"} {
		if !strings.Contains(html, want) {
			t.Errorf("default tile missing %q in:\n%s", want, html)
		}
	}

	// catalogued Lucide name + color → iconify span, color applied via inline style
	html = renderHTML(t, typeIconTile("lucide--file-text", "#4F46E5"))
	for _, want := range []string{"lucide--file-text", "iconify", "size-4.5", "color:#4F46E5", "color-mix"} {
		if !strings.Contains(html, want) {
			t.Errorf("iconify tile missing %q in:\n%s", want, html)
		}
	}
	if strings.Contains(html, "lucide--box") {
		t.Errorf("iconify tile should not fall back to box in:\n%s", html)
	}
	if !strings.Contains(html, `aria-hidden="true"`) {
		t.Error("tile glyph must be aria-hidden")
	}

	// bare PascalCase schema name normalises to the catalog iconify class
	html = renderHTML(t, typeIconTile("FileText", ""))
	for _, want := range []string{"lucide--file-text", "iconify"} {
		if !strings.Contains(html, want) {
			t.Errorf("bare-name tile missing %q in:\n%s", want, html)
		}
	}

	// unknown name → generic box, never the raw declared text
	html = renderHTML(t, typeIconTile("DefinitelyNotAnIcon", ""))
	if !strings.Contains(html, "lucide--box") {
		t.Errorf("unknown-name tile should fall back to box in:\n%s", html)
	}
	if strings.Contains(html, "DefinitelyNotAnIcon") {
		t.Errorf("unknown-name tile must not render the raw icon text in:\n%s", html)
	}

	// emoji glyph → plain text span carrying the emoji, no iconify markup
	html = renderHTML(t, typeIconTile("📝", ""))
	for _, want := range []string{"📝", `aria-hidden="true"`, "bg-primary/10"} {
		if !strings.Contains(html, want) {
			t.Errorf("emoji tile missing %q in:\n%s", want, html)
		}
	}
	if strings.Contains(html, "iconify") {
		t.Errorf("emoji tile must not use iconify markup in:\n%s", html)
	}

	// color-only declaration still renders the box icon, tinted
	html = renderHTML(t, typeIconTile("", "#7C3AED"))
	if !strings.Contains(html, "lucide--box") || !strings.Contains(html, "color:#7C3AED") {
		t.Errorf("color-only tile should tint the box icon in:\n%s", html)
	}
}

func TestRenderTypeNameChip(t *testing.T) {
	// no declaration → the generic box badge with the given variant
	html := renderHTML(t, typeNameChip("person", ui.BadgeGhost, "", ""))
	for _, want := range []string{"person", "badge-ghost", "lucide--box", "badge-sm"} {
		if !strings.Contains(html, want) {
			t.Errorf("default chip missing %q in:\n%s", want, html)
		}
	}

	// emoji + color → glyph prefix, tinted chip, label kept readable
	html = renderHTML(t, typeNameChip("person", ui.BadgePrimary, "📝", "#4F46E5"))
	for _, want := range []string{"person", "📝", "color:#4F46E5", "text-base-content"} {
		if !strings.Contains(html, want) {
			t.Errorf("emoji chip missing %q in:\n%s", want, html)
		}
	}
	if strings.Contains(html, "badge-primary") || strings.Contains(html, "badge-ghost") {
		t.Errorf("colored chip should drop the daisy variant class in:\n%s", html)
	}

	// iconify icon, no color → chip keeps its variant tone, icon prefix swaps in
	html = renderHTML(t, typeNameChip("person", ui.BadgeGhost, "lucide--file-text", ""))
	for _, want := range []string{"person", "badge-ghost", "lucide--file-text", "iconify", "size-3"} {
		if !strings.Contains(html, want) {
			t.Errorf("iconify chip missing %q in:\n%s", want, html)
		}
	}
	if strings.Contains(html, "lucide--box") {
		t.Errorf("iconify chip should not fall back to box in:\n%s", html)
	}

	// bare schema name normalises to the catalog class
	html = renderHTML(t, typeNameChip("Document", ui.BadgeGhost, "FileText", ""))
	if !strings.Contains(html, "lucide--file-text") || strings.Contains(html, "FileText") {
		t.Errorf("bare-name chip should render the catalog icon, not the raw label prefix:\n%s", html)
	}

	// unknown icon name → generic box, never the raw text
	html = renderHTML(t, typeNameChip("Asset", ui.BadgeGhost, "DefinitelyNotAnIcon", ""))
	if !strings.Contains(html, "lucide--box") || strings.Contains(html, "DefinitelyNotAnIcon") {
		t.Errorf("unknown-icon chip should fall back to box without echoing the name:\n%s", html)
	}
}
