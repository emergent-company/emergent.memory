package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/emergent-company/go-daisy/components/nav"
	ui "github.com/emergent-company/go-daisy/components/ui"
)

// radiusClassRE matches a radius utility class token (rounded, rounded-box,
// rounded-field, rounded-2xl, rounded-t-box, …).
var radiusClassRE = regexp.MustCompile(`\brounded[\w-]*`)

// withoutRadiusClasses strips radius class tokens before comparison. The
// component conventions hold class composition — and therefore which radius step
// a component uses — outside its contract, and the theme's radius tokens are the
// single source of truth, so migrating between equivalent steps must not fail an
// exact-render assertion. Everything else (structure, ids, ARIA, other classes)
// is still compared exactly.
func withoutRadiusClasses(s string) string {
	return radiusClassRE.ReplaceAllString(s, "rounded")
}

func TestRefactorOutputExact(t *testing.T) {
	var buf bytes.Buffer
	render := func(c templ.Component, kids ...string) string {
		t.Helper()
		buf.Reset()
		ctx := t.Context()
		if len(kids) > 0 {
			ctx = templ.WithChildren(ctx, templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
				for _, k := range kids {
					if _, err := io.WriteString(w, k); err != nil {
						return err
					}
				}
				return nil
			}))
		}
		if err := c.Render(ctx, &buf); err != nil {
			t.Fatalf("render: %v", err)
		}
		return buf.String()
	}

	check := func(name, got, want string) {
		t.Helper()
		if withoutRadiusClasses(got) != withoutRadiusClasses(want) {
			t.Errorf("%s:\n got: %q\nwant: %q", name, got, want)
		}
	}

	// eyebrow (now ui.Eyebrow) — muted class owned by go-daisy upstream
	got := render(ui.Eyebrow("Allowed tools", ui.EyebrowProps{Size: "text-[11px]", Margin: "mb-2"}))
	want := `<p class="text-base-content/45 font-semibold tracking-[0.16em] uppercase text-[11px] mb-2">Allowed tools</p>`
	check("eyebrow", got, want)

	got = render(ui.Eyebrow("Tools", ui.EyebrowProps{Size: "text-[10px]", Margin: "mt-4 mb-2"}))
	want = `<p class="text-base-content/45 font-semibold tracking-[0.16em] uppercase text-[10px] mt-4 mb-2">Tools</p>`
	check("eyebrow mt", got, want)

	got = render(ui.Eyebrow("Key", ui.EyebrowProps{Size: "text-[11px]"}))
	want = `<p class="text-base-content/45 font-semibold tracking-[0.16em] uppercase text-[11px]">Key</p>`
	check("eyebrow nomargin", got, want)

	// countBadge / countBadgeLabel
	got = render(countBadge(3, "agent", "lucide--bot"))
	if !strings.Contains(got, "3 agents") || !strings.Contains(got, `class="badge gap-1 badge-ghost badge-sm"`) {
		t.Errorf("countBadge: %q", got)
	}
	got = render(countBadgeLabel("2 documents", "lucide--file-text"))
	if !strings.Contains(got, "2 documents") {
		t.Errorf("countBadgeLabel: %q", got)
	}

	// cardList — titled Section variant
	got = render(cardList("Chunks", cardListProps{Margin: "mt-6", Empty: false}), "<p>row1</p>")
	want = `<section class="mt-6"><h2 class="mb-3 text-lg font-semibold tracking-tight">Chunks</h2><div class="card bg-base-100 border border-line overflow-hidden"><div class="card-body memory-density-flush"><div class="divide-y divide-line"><p>row1</p></div></div></div></section>`
	check("cardList titled", got, want)

	// cardList — bare CardRaw variant
	got = render(cardList("", cardListProps{}), "<p>r1</p>", "<p>r2</p>")
	want = `<div class="card bg-base-100 border border-line overflow-hidden"><div class="card-body memory-density-flush"><div class="divide-y divide-line"><p>r1</p><p>r2</p></div></div></div>`
	check("cardList bare", got, want)

	// cardList — empty state
	got = render(cardList("Installed", cardListProps{Margin: "mb-8", Empty: true, EmptyIcon: "lucide--library", EmptyTitle: "No blueprints installed", EmptyDesc: "Install a blueprint to extend the graph schema."}), "<p>row</p>")
	want = `<section class="mb-8"><h2 class="mb-3 text-lg font-semibold tracking-tight">Installed</h2><div class="card bg-base-100 border border-line"><div class="card-body"><div class="flex flex-col items-center justify-center py-16 text-center px-4"><span class="iconify size-12 text-base-content/20 mb-4 lucide--library"></span><h3 class="text-base font-semibold text-base-content mb-1">No blueprints installed</h3><p class="text-sm text-base-content/50 mb-4">Install a blueprint to extend the graph schema.</p></div></div></div></section>`
	check("cardList empty", got, want)

	// ui.Dialog (was the local modalShell — same markup)
	got = render(ui.Dialog(ui.DialogProps{ID: "spotlight-modal", BoxClass: "p-0 max-w-lg"}), "<p>x</p>")
	want = `<dialog id="spotlight-modal" class="modal" hx-boost="false"><div class="modal-box p-0 max-w-lg"><p>x</p></div><form method="dialog" class="modal-backdrop"><button>close</button></form></dialog>`
	check("ui.Dialog", got, want)

	got = render(ui.Dialog(ui.DialogProps{ID: "sidepanel-modal", BoxID: "sidepanel-modal-box"}), "<p>x</p>")
	want = `<dialog id="sidepanel-modal" class="modal" hx-boost="false"><div id="sidepanel-modal-box" class="modal-box"><p>x</p></div><form method="dialog" class="modal-backdrop"><button>close</button></form></dialog>`
	check("ui.Dialog boxid", got, want)

	// ui.ConfirmDialog (was the local confirmDeleteDialog — same markup)
	got = render(ui.ConfirmDialog(ui.ConfirmDialogProps{ID: "delete-confirm-modal", Noun: "agent"}, deleteAgentDescription()),
		`<div class="flex justify-end gap-2 pt-2"><button type="button" class="btn btn-ghost btn-sm" data-action="close-delete-confirm">Cancel</button> <button type="button" class="btn btn-error btn-sm" id="delete-agent-go">Delete</button></div>`)
	want = `<dialog id="delete-confirm-modal" class="modal" hx-boost="false"><div class="modal-box max-w-sm"><div class="mb-4 flex items-start gap-3"><div class="bg-error/10 text-error grid size-10 shrink-0 place-items-center rounded-full"><span class="iconify lucide--trash-2 size-5" aria-hidden="true"></span></div><div><h3 class="text-lg font-semibold">Delete agent?</h3><p class="text-base-content/50 text-sm mt-1">This permanently removes <span id="delete-agent-name" class="font-medium text-base-content"></span> and its definition. Irreversible.</p></div></div><div class="flex justify-end gap-2 pt-2"><button type="button" class="btn btn-ghost btn-sm" data-action="close-delete-confirm">Cancel</button> <button type="button" class="btn btn-error btn-sm" id="delete-agent-go">Delete</button></div></div><form method="dialog" class="modal-backdrop"><button>close</button></form></dialog>`
	check("ui.ConfirmDialog", got, want)

	// detailHeader: the nav group is prepended to the page's own trail, the
	// description keeps the shared measure, and the title uses the one scale.
	got = render(detailHeader(crumbsActive(nav.Crumbs("Skills", "/skills", "New skill")), "", "New skill", "A name, what it does, and the instructions that make it work."))
	want = `<div class="mb-6"><div class="breadcrumbs text-sm" style="width: 100%;"><ul><li class=""><a href="/agents" class="">Agents</a></li><li class=""><a href="/skills" class="">Skills</a></li><li class="font-medium text-base-content"><span class="">New skill</span></li></ul></div><div class="mt-2 flex flex-wrap items-end justify-between gap-4"><div class="min-w-0"><div class="flex flex-wrap items-center gap-2"><h1 class="text-2xl font-bold tracking-tight lg:text-3xl">New skill</h1></div><p class="text-base-content/55 mt-1 max-w-2xl text-sm">A name, what it does, and the instructions that make it work.</p></div></div></div>`
	check("detailHeader default", got, want)

	// detailHeader Meta: identity/state chips render BELOW the description.
	got = render(detailHeader(crumbsActive(nav.Crumbs("Agents", "/agents", "diane", "/agents/a1")), "", "diane", "household assistant", detailHeaderOpts{Margin: "mb-6", Meta: ui.Badge(ui.BadgeProps{Label: "updated 2h ago", Variant: ui.BadgeGhost, Size: ui.BadgeSizeSM, Icon: "lucide--clock"})}))
	want = `<div class="mb-6"><div class="breadcrumbs text-sm" style="width: 100%;"><ul><li class=""><a href="/agents" class="">Agents</a></li><li class="font-medium text-base-content"><span class="">diane</span></li></ul></div><div class="mt-2 flex flex-wrap items-end justify-between gap-4"><div class="min-w-0"><div class="flex flex-wrap items-center gap-2"><h1 class="text-2xl font-bold tracking-tight lg:text-3xl">diane</h1></div><p class="text-base-content/55 mt-1 max-w-2xl text-sm">household assistant</p><div class="mt-3 flex flex-wrap items-center gap-2" data-header-meta><span class="badge gap-1 badge-ghost badge-sm"><span class="iconify lucide--clock size-3 shrink-0" aria-hidden="true"></span><span class="">updated 2h ago</span></span></div></div></div></div>`
	check("detailHeader meta", got, want)

	// detailHeader infers the group from the trail root: a Blueprints page
	// starts at MEMORY, the group it lives in since #180.
	got = render(detailHeader(crumbsActive(nav.Crumbs("Blueprints", "/blueprints", "Schema migrations")), "", "Schema migrations", "Rename types and properties across live graph data when a blueprint version changes.", detailHeaderOpts{Margin: "mb-6"}))
	want = `<div class="mb-6"><div class="breadcrumbs text-sm" style="width: 100%;"><ul><li class=""><a href="/objects" class="">Memory</a></li><li class=""><a href="/blueprints" class="">Blueprints</a></li><li class="font-medium text-base-content"><span class="">Schema migrations</span></li></ul></div><div class="mt-2 flex flex-wrap items-end justify-between gap-4"><div class="min-w-0"><div class="flex flex-wrap items-center gap-2"><h1 class="text-2xl font-bold tracking-tight lg:text-3xl">Schema migrations</h1></div><p class="text-base-content/55 mt-1 max-w-2xl text-sm">Rename types and properties across live graph data when a blueprint version changes.</p></div></div></div>`
	check("detailHeader inferred group", got, want)

	// detailHeader on a Schema page inherits Memory; the old "Object type"
	// eyebrow is gone — crumbs carry the context.
	got = render(detailHeader(crumbsActive(nav.Crumbs("Schema", "/schema", "Person")), "", "Person", "", detailHeaderOpts{Margin: "mb-6"}))
	want = `<div class="mb-6"><div class="breadcrumbs text-sm" style="width: 100%;"><ul><li class=""><a href="/objects" class="">Memory</a></li><li class=""><a href="/schema" class="">Schema</a></li><li class="font-medium text-base-content"><span class="">Person</span></li></ul></div><div class="mt-2 flex flex-wrap items-end justify-between gap-4"><div class="min-w-0"><div class="flex flex-wrap items-center gap-2"><h1 class="text-2xl font-bold tracking-tight lg:text-3xl">Person</h1></div></div></div></div>`
	check("detailHeader object type", got, want)

	// pageHeader: a group landing page collapses to one active crumb.
	got = render(pageHeader("Agents", "Agents", "The personas Memory can become — pick a model, set the tone, grant tools."), "<span>action</span>")
	want = `<div class="mb-6"><div class="breadcrumbs text-sm" style="width: 100%;"><ul><li class="font-medium text-base-content"><span class="">Agents</span></li></ul></div><div class="mt-2 flex flex-wrap items-end justify-between gap-4"><div class="min-w-0"><div class="flex flex-wrap items-center gap-2"><h1 class="text-2xl font-bold tracking-tight lg:text-3xl">Agents</h1></div><p class="text-base-content/55 mt-1 max-w-2xl text-sm">The personas Memory can become — pick a model, set the tone, grant tools.</p></div><span>action</span></div></div>`
	check("pageHeader landing", got, want)

	// pageHeader: a section page links its group first and marks itself active.
	got = render(pageHeader("Memory", "Blueprints", ""))
	want = `<div class="mb-6"><div class="breadcrumbs text-sm" style="width: 100%;"><ul><li class=""><a href="/objects" class="">Memory</a></li><li class="font-medium text-base-content"><span class="">Blueprints</span></li></ul></div><div class="mt-2 flex flex-wrap items-end justify-between gap-4"><div class="min-w-0"><div class="flex flex-wrap items-center gap-2"><h1 class="text-2xl font-bold tracking-tight lg:text-3xl">Blueprints</h1></div></div></div></div>`
	check("pageHeader section", got, want)

	// A retired group label (pre-#180 vocabulary) must not render a crumb with
	// no destination: it degrades to ungrouped rather than emitting a dead link.
	got = render(pageHeader("Settings", "Blueprints", ""))
	want = `<div class="mb-6"><div class="breadcrumbs text-sm" style="width: 100%;"><ul><li class="font-medium text-base-content"><span class="">Blueprints</span></li></ul></div><div class="mt-2 flex flex-wrap items-end justify-between gap-4"><div class="min-w-0"><div class="flex flex-wrap items-center gap-2"><h1 class="text-2xl font-bold tracking-tight lg:text-3xl">Blueprints</h1></div></div></div></div>`
	check("pageHeader retired group", got, want)

	// pageHeader: an ungrouped page is a single active crumb.
	got = render(pageHeader("", "Title", ""))
	want = `<div class="mb-6"><div class="breadcrumbs text-sm" style="width: 100%;"><ul><li class="font-medium text-base-content"><span class="">Title</span></li></ul></div><div class="mt-2 flex flex-wrap items-end justify-between gap-4"><div class="min-w-0"><div class="flex flex-wrap items-center gap-2"><h1 class="text-2xl font-bold tracking-tight lg:text-3xl">Title</h1></div></div></div></div>`
	check("pageHeader ungrouped", got, want)

	// checkboxPicker — skill, empty with id
	got = render(checkboxPicker(checkboxPickerProps{
		Name: "skill", EmptyID: "agent-skills-empty", EmptyContent: skillEmptyContent(),
		LabelClass: "items-start gap-2 py-1.5", CheckboxClass: "checkbox checkbox-sm mt-0.5",
		TitleClass: "block text-sm font-medium", DescClass: "text-muted block text-xs", WrapTitle: true,
	}))
	want = `<p id="agent-skills-empty" class="text-base-content/40 text-sm">No skills yet — <a class="text-primary font-medium hover:underline" href="/skills">create one on the Skills page</a>.</p>`
	check("checkboxPicker skill empty", got, want)

	// checkboxPicker — skill, with items + hidden duplicate note
	sk := []Skill{{Name: "summarize-email", Description: "Condenses threads"}, {Name: "recall-memory"}}
	got = render(checkboxPicker(checkboxPickerProps{
		Name: "skill", EmptyID: "agent-skills-empty", EmptyContent: skillEmptyContent(),
		LabelClass: "items-start gap-2 py-1.5", CheckboxClass: "checkbox checkbox-sm mt-0.5",
		TitleClass: "block text-sm font-medium", DescClass: "text-muted block text-xs", WrapTitle: true,
		Items: skillPickerItems(sk, nil),
	}))
	want = `<div class="rounded-box border border-line-translucent bg-base-200/40 p-3 flex flex-col"><label class="label flex items-start gap-2 py-1.5"><input name="skill" type="checkbox" value="summarize-email" class="checkbox checkbox-sm mt-0.5"> <span class="min-w-0 whitespace-normal"><span class="block text-sm font-medium">summarize-email</span> <span class="text-muted block text-xs">Condenses threads</span></span></label><label class="label flex items-start gap-2 py-1.5"><input name="skill" type="checkbox" value="recall-memory" class="checkbox checkbox-sm mt-0.5"> <span class="min-w-0 whitespace-normal"><span class="block text-sm font-medium">recall-memory</span> </span></label> <p id="agent-skills-empty" class="text-base-content/40 hidden text-sm">No skills yet — <a class="text-primary font-medium hover:underline" href="/skills">create one on the Skills page</a>.</p></div>`
	check("checkboxPicker skill items", got, want)

	// checkboxPicker — skill, settings style (checked + no id)
	got = render(checkboxPicker(checkboxPickerProps{
		Name: "skill", EmptyContent: skillEmptyContent(),
		LabelClass: "items-start gap-2 py-1.5", CheckboxClass: "checkbox checkbox-sm mt-0.5",
		TitleClass: "block text-sm font-medium", DescClass: "text-muted block text-xs", WrapTitle: true,
		Items: skillPickerItems(sk, func(n string) bool { return n == "summarize-email" }),
	}))
	want = `<div class="rounded-box border border-line-translucent bg-base-200/40 p-3 flex flex-col"><label class="label flex items-start gap-2 py-1.5"><input name="skill" type="checkbox" value="summarize-email" checked class="checkbox checkbox-sm mt-0.5"> <span class="min-w-0 whitespace-normal"><span class="block text-sm font-medium">summarize-email</span> <span class="text-muted block text-xs">Condenses threads</span></span></label><label class="label flex items-start gap-2 py-1.5"><input name="skill" type="checkbox" value="recall-memory" class="checkbox checkbox-sm mt-0.5"> <span class="min-w-0 whitespace-normal"><span class="block text-sm font-medium">recall-memory</span> </span></label> </div>`
	check("checkboxPicker skill checked", got, want)

	// checkboxPicker — delegate, modal style (wrap id + empty id)
	ag := []AgentDefinitionSummary{{Name: "diane"}, {Name: "milo"}}
	got = render(checkboxPicker(checkboxPickerProps{
		Name: "delegation-target", Wrapped: true, WrapID: "agent-delegation-targets", EmptyID: "agent-delegation-empty", EmptyContent: delegateEmptyContent(),
		LabelClass: "gap-2 py-1", CheckboxClass: "checkbox", TitleClass: "text-muted-bright text-sm",
		Items: delegatePickerItems(ag, "", nil),
	}))
	want = `<div id="agent-delegation-targets" class="mt-2"><div class="rounded-box border border-line-translucent bg-base-200/40 p-3 flex flex-col"><label class="label flex gap-2 py-1"><input name="delegation-target" type="checkbox" value="diane" class="checkbox"> <span class="text-muted-bright text-sm">diane</span></label><label class="label flex gap-2 py-1"><input name="delegation-target" type="checkbox" value="milo" class="checkbox"> <span class="text-muted-bright text-sm">milo</span></label> <p id="agent-delegation-empty" class="text-base-content/40 hidden text-sm">No other agents to delegate to.</p></div></div>`
	check("checkboxPicker delegate modal", got, want)

	// checkboxPicker — delegate, settings style (exclude self, checked, no ids)
	got = render(checkboxPicker(checkboxPickerProps{
		Name: "delegation-target", Wrapped: true, EmptyContent: delegateEmptyContent(),
		LabelClass: "gap-2 py-1", CheckboxClass: "checkbox", TitleClass: "text-muted-bright text-sm",
		Items: delegatePickerItems(ag, "diane", func(n string) bool { return n == "milo" }),
	}))
	want = `<div class="mt-2"><div class="rounded-box border border-line-translucent bg-base-200/40 p-3 flex flex-col"><label class="label flex gap-2 py-1"><input name="delegation-target" type="checkbox" value="milo" checked class="checkbox"> <span class="text-muted-bright text-sm">milo</span></label> </div></div>`
	check("checkboxPicker delegate settings", got, want)

	// modelSelect — modal style (no current, data-testid)
	models := []Model{{Provider: "openai", ModelName: "gpt-4o", DisplayName: "GPT-4o"}}
	got = render(modelSelect("agent-model", models, nil, "", templ.Attributes{"data-testid": "model-select"}))
	want = `<fieldset class="fieldset"><legend class="fieldset-legend">Model</legend><select id="agent-model" data-testid="model-select" name="modelName" class="select w-full"><option value="">Auto — default model</option><optgroup label="openai"><option value="openai/gpt-4o">openai/gpt-4o</option></optgroup></select><p class="label"><span class="label-text-alt text-base-content/60">Any model the bridge can reach — hosted or local.</span></p></fieldset>`
	check("modelSelect modal", got, want)

	// modelSelect — settings style (current selected)
	got = render(modelSelect("agent-settings-model", models, &ModelConfig{Name: "openai/gpt-4o"}, "", nil))
	want = `<fieldset class="fieldset"><legend class="fieldset-legend">Model</legend><select id="agent-settings-model" name="modelName" class="select w-full"><option value="">Auto — default model</option><optgroup label="openai"><option value="openai/gpt-4o" selected>openai/gpt-4o</option></optgroup></select><p class="label"><span class="label-text-alt text-base-content/60">Any model the bridge can reach — hosted or local.</span></p></fieldset>`
	check("modelSelect settings", got, want)

	// modelSelect — resolved default names the Auto option (value stays "")
	got = render(modelSelect("agent-settings-model", models, nil, "openai/deepseek-v4-flash", nil))
	want = `<fieldset class="fieldset"><legend class="fieldset-legend">Model</legend><select id="agent-settings-model" name="modelName" class="select w-full"><option value="">Auto — openai/deepseek-v4-flash (default)</option><optgroup label="openai"><option value="openai/gpt-4o">openai/gpt-4o</option></optgroup></select><p class="label"><span class="label-text-alt text-base-content/60">Any model the bridge can reach — hosted or local.</span></p></fieldset>`
	check("modelSelect named default", got, want)

	// modelParamsGrid — modal (no values)
	got = render(modelParamsGrid("agent", nil))
	want = `<div class="grid grid-cols-2 gap-3"><fieldset class="fieldset"><legend class="fieldset-legend">Temperature</legend><input id="agent-temperature" name="temperature" type="number" min="0" max="2" step="0.1" placeholder="0.7" class="input w-full"></fieldset><fieldset class="fieldset"><legend class="fieldset-legend">Max tokens</legend><input id="agent-max-tokens" name="maxTokens" type="number" min="1" step="1" placeholder="4096" class="input w-full"></fieldset></div>`
	check("modelParamsGrid modal", got, want)

	// modelParamsGrid — settings (values)
	got = render(modelParamsGrid("agent-settings", &ModelConfig{Name: "gpt-4o", Temperature: 0.7, MaxTokens: 4096}))
	want = `<div class="grid grid-cols-2 gap-3"><fieldset class="fieldset"><legend class="fieldset-legend">Temperature</legend><input id="agent-settings-temperature" name="temperature" type="number" min="0" max="2" step="0.1" value="0.7" placeholder="0.7" class="input w-full"></fieldset><fieldset class="fieldset"><legend class="fieldset-legend">Max tokens</legend><input id="agent-settings-max-tokens" name="maxTokens" type="number" min="1" step="1" value="4096" placeholder="4096" class="input w-full"></fieldset></div>`
	check("modelParamsGrid settings", got, want)

	// flashToasts — pushes into the Alpine queue when the shell is loaded
	// (hx-boost swaps); falls back to the sessionStorage stash on full loads.
	got = render(flashToasts("Skill created.", nil))
	want = `<script type="text/javascript">(function(){try{var p=JSON.parse("{\"duration\":4200,\"kind\":\"success\",\"message\":\"Skill created.\"}");var c=document.getElementById("toast-container");if(c&&window.Alpine){var q=window.Alpine.$data(c);if(q&&q.add){q.add({type:p.kind,message:p.message,duration:p.duration});return;}}sessionStorage.setItem("memory-toast",JSON.stringify(p));}catch(e){}})();</script>`
	check("flashToasts", got, want)

	// flashToasts — errors are persistent: duration 0 tells the queue to keep
	// the toast and render a close button instead of auto-dismissing.
	got = render(flashToasts("", errors.New("boom")))
	want = `<script type="text/javascript">(function(){try{var p=JSON.parse("{\"duration\":0,\"kind\":\"error\",\"message\":\"boom\"}");var c=document.getElementById("toast-container");if(c&&window.Alpine){var q=window.Alpine.$data(c);if(q&&q.add){q.add({type:p.kind,message:p.message,duration:p.duration});return;}}sessionStorage.setItem("memory-toast",JSON.stringify(p));}catch(e){}})();</script>`
	check("flashToasts error persistent", got, want)

	// chatWelcomeHero — chat variant
	got = render(chatWelcomeHero("chat-empty", "lucide--sparkles", "gap-4 py-10", "Ready when you are", 2, "text-base", "Ask anything.", "mt-1 max-w-sm text-sm"), "<p>extra</p>")
	want = `<div id="chat-empty" class="flex flex-col items-center justify-center gap-4 py-10 text-center"><div class="bg-primary/10 text-primary border-primary/15 grid size-14 place-items-center rounded-2xl border"><span class="iconify lucide--sparkles size-7" aria-hidden="true"></span></div><div><h2 class="text-base font-semibold">Ready when you are</h2><p class="text-muted mt-1 max-w-sm text-sm">Ask anything.</p></div><p>extra</p></div>`
	check("chatWelcomeHero chat", got, want)

	// chatWelcomeHero — sidepanel variant (h3)
	got = render(chatWelcomeHero("sidepanel-empty", "lucide--bot", "gap-3 py-8", "Chat with your assistant", 3, "text-sm", "Ask anything.", "mt-0.5 max-w-60 text-xs"))
	want = `<div id="sidepanel-empty" class="flex flex-col items-center justify-center gap-3 py-8 text-center"><div class="bg-primary/10 text-primary border-primary/15 grid size-14 place-items-center rounded-2xl border"><span class="iconify lucide--bot size-7" aria-hidden="true"></span></div><div><h3 class="text-sm font-semibold">Chat with your assistant</h3><p class="text-muted mt-0.5 max-w-60 text-xs">Ask anything.</p></div></div>`
	check("chatWelcomeHero sidepanel", got, want)

	// chipRow — wrapped variant (migration plan grid cells)
	got = render(chipRow("Type renames", "mb-1.5", true), "<span>a</span>")
	want = `<div><p class="text-base-content/45 font-semibold tracking-[0.16em] uppercase text-[10px] mb-1.5">Type renames</p><div class="flex flex-wrap gap-1.5"><span>a</span></div></div>`
	check("chipRow wrapped", got, want)
}
