package main

import (
	"github.com/a-h/templ"
	alpine "github.com/emergent-company/go-daisy/components/alpine"
	behaviors "github.com/emergent-company/go-daisy/components/behaviors"
	form "github.com/emergent-company/go-daisy/components/form"
	layout "github.com/emergent-company/go-daisy/components/layout"
	nav "github.com/emergent-company/go-daisy/components/nav"
	table "github.com/emergent-company/go-daisy/components/table"
	ui "github.com/emergent-company/go-daisy/components/ui"
)

// daisyFixtureSpec is the authored fixture for one on-demand L0 go-daisy entry.
type daisyFixtureSpec struct {
	PropsType      string
	Description    string
	Render         func() templ.Component
	Interactive    bool
	DefaultVariant string
	Stories        map[string]func() templ.Component
}

// daisyFixtures is the hand-authored fixture table for the go-daisy primitives
// the app renders but that have no curated seed entry. One entry per rendered
// id. Fixtures are inert: no live endpoints, no mutation wiring, and no
// hx-post/put/patch/delete. Structural components (table, popover, dropdown)
// render their full composed context so the preview is actually populated.
var daisyFixtures = map[string]daisyFixtureSpec{
	"AvatarFull": {
		PropsType:   "AvatarProps",
		Description: "A circular avatar showing image, initials, or an icon fallback.",
		Render: func() templ.Component {
			return ui.AvatarFull(ui.AvatarProps{Name: "Ada Lovelace", Size: ui.AvatarSize14})
		},
	},
	"Breadcrumbs": {
		PropsType:   "items []BreadcrumbItem, divider BreadcrumbsDivider",
		Description: "A breadcrumb navigation trail with icon labels and an active step.",
		Render: func() templ.Component {
			return nav.Breadcrumbs([]nav.BreadcrumbItem{
				{Label: "Projects", Href: "#", Icon: "lucide--folder"},
				{Label: "memory", Href: "#"},
				{Label: "Objects", Active: true},
			}, nav.BreadcrumbsDividerSlash)
		},
	},
	"ClipboardCopy": {
		PropsType:   "items []ClipboardCopyItem",
		Description: "A click-to-copy field — a readonly input or a mono code badge — with a success flash.",
		Render: func() templ.Component {
			return form.ClipboardCopy([]form.ClipboardCopyItem{
				{ID: "gallery-copy-secret", Label: "Secret", Value: "emt_inert_sample", ButtonLabel: "Copy"},
				{Label: "Endpoint", Value: "https://example.invalid/mcp", ButtonLabel: "Copy", Mono: true},
			})
		},
	},
	"ColorPicker": {
		PropsType:   "ColorPickerProps",
		Description: "A colour field pairing a text input with a native swatch and presets.",
		Render: func() templ.Component {
			return ui.ColorPicker(ui.ColorPickerProps{
				ID:      "gallery-color",
				Name:    "color",
				Value:   "#4F46E5",
				Presets: []string{"#4F46E5", "#10B981", "#F59E0B"},
			})
		},
	},
	"CommandPalette": {
		PropsType:      "CommandPaletteProps",
		Description:    "A ⌘K command palette modal listing searchable actions.",
		Interactive:    true,
		DefaultVariant: "open",
		Render: func() templ.Component {
			return ui.CommandPalette(ui.CommandPaletteProps{
				ID:   "gallery-palette",
				Open: true,
				Items: []ui.CommandPaletteItem{
					{ID: "new", Label: "New object", Icon: "lucide--plus", Shortcut: "⌘N", Href: "#"},
					{ID: "search", Label: "Search", Icon: "lucide--search", Shortcut: "⌘K", Href: "#"},
					{ID: "settings", Label: "Settings", Icon: "lucide--settings", Href: "#"},
				},
			})
		},
	},
	"ConfirmDialog": {
		PropsType:   "props ConfirmDialogProps, description templ.Component",
		Description: "A destructive confirm dialog with an error icon and a description slot.",
		Render: func() templ.Component {
			return withChildren(
				ui.ConfirmDialog(ui.ConfirmDialogProps{ID: "gallery-confirm", Noun: "agent"}, templ.Raw(`<p class="text-sm text-base-content/60">This deletes the agent and its content. This cannot be undone.</p>`)),
				join(
					withChildren(ui.Button(ui.ButtonProps{Type: ui.ButtonTypeButton, Variant: ui.ButtonGhost, Size: ui.ButtonSM}), templ.Raw("Cancel")),
					withChildren(ui.Button(ui.ButtonProps{Type: ui.ButtonTypeButton, Variant: ui.ButtonError, Size: ui.ButtonSM}), templ.Raw("Delete")),
				),
			)
		},
	},
	"Container": {
		PropsType:   "size ContainerSize, attrs templ.Attributes",
		Description: "A centered max-width container with responsive padding.",
		Render: func() templ.Component {
			return withChildren(layout.Container(layout.ContainerMD, nil), templ.Raw(`<p class="text-sm">Page content inside a centered container.</p>`))
		},
	},
	"Dialog": {
		PropsType:      "DialogProps",
		Description:    "A native modal dialog shell with a backdrop and close control.",
		Interactive:    true,
		DefaultVariant: "open",
		Render: func() templ.Component {
			return withChildren(ui.Dialog(ui.DialogProps{ID: "gallery-dialog", Attrs: templ.Attributes{"data-dialog-autoopen": "true"}}),
				templ.Raw(`<h3 class="text-lg font-semibold">Session ended</h3><p class="py-2 text-sm text-base-content/60">Your session timed out. Sign in again to continue.</p>`))
		},
	},
	"DialogTitle": {
		PropsType:   "props ...DialogTitleProps",
		Description: "A dialog, panel or card heading with level, size, and tracking controls.",
		Render: func() templ.Component {
			return withChildren(ui.DialogTitle(ui.DialogTitleProps{ID: "gallery-dialog-title"}), templ.Raw("Edit agent"))
		},
	},
	"Disclosure": {
		PropsType:   "props DisclosureProps, header templ.Component, trailing ...templ.Component",
		Description: "A native details/summary collapsible section with a rotating chevron.",
		Render: func() templ.Component {
			return withChildren(
				ui.Disclosure(ui.DisclosureProps{Open: true}, templ.Raw(`<span class="text-sm font-medium">Advanced options</span>`)),
				templ.Raw(`<p class="text-sm text-base-content/60">Disclosed body content shown when open.</p>`),
			)
		},
	},
	"Dropdown": {
		PropsType:   "align DropdownAlign, attrs templ.Attributes",
		Description: "A CSS focus-within dropdown shell with a visible trigger and menu.",
		Render: func() templ.Component {
			return withChildren(ui.Dropdown(ui.DropdownEnd, nil),
				join(
					withChildren(ui.Button(ui.ButtonProps{Type: ui.ButtonTypeButton, Variant: ui.ButtonGhost, Size: ui.ButtonSM}), templ.Raw("Actions")),
					withChildren(ui.DropdownMenu(nil), templ.Raw(`<li><a>Rename</a></li><li><a>Duplicate</a></li><li><a class="text-error">Delete</a></li>`)),
				),
			)
		},
	},
	"DropdownMenu": {
		PropsType:   "attrs templ.Attributes",
		Description: "The dropdown-content menu list, shown in its composed dropdown.",
		Render: func() templ.Component {
			return withChildren(ui.Dropdown(ui.DropdownEnd, nil),
				join(
					withChildren(ui.Button(ui.ButtonProps{Type: ui.ButtonTypeButton, Variant: ui.ButtonGhost, Size: ui.ButtonSM}), templ.Raw("More")),
					withChildren(ui.DropdownMenu(nil), templ.Raw(`<li><a>Export</a></li><li><a>Archive</a></li>`)),
				),
			)
		},
	},
	"EmptyNote": {
		PropsType:   "props ...EmptyNoteProps",
		Description: "A muted one-line note for empty-state or helper copy.",
		Render: func() templ.Component {
			return join(
				withChildren(ui.EmptyNote(ui.EmptyNoteProps{}), templ.Raw("No other agents to delegate to.")),
				withChildren(ui.EmptyNote(ui.EmptyNoteProps{Tone: ui.EmptyNoteToneStrong}), templ.Raw("Controls who can discover this agent.")),
				withChildren(ui.EmptyNote(ui.EmptyNoteProps{Tone: ui.EmptyNoteToneQuiet, Size: ui.EmptyNoteSizeXS}), templ.Raw("Updated 2h ago.")),
			)
		},
	},
	"EmptyState": {
		PropsType:   "EmptyStateProps",
		Description: "A centered empty-state placeholder with icon, title, description, and action.",
		Render: func() templ.Component {
			return ui.EmptyState(ui.EmptyStateProps{
				Icon:        "lucide--inbox",
				Title:       "No objects yet",
				Description: "Create your first object to get started.",
				ActionHref:  "#",
				ActionLabel: "New object",
				ActionIcon:  "lucide--plus",
			})
		},
	},
	"FormActions": {
		PropsType:   "submitText string, indicatorID string",
		Description: "A right-aligned footer for form action buttons.",
		Render: func() templ.Component {
			return withChildren(form.FormActions("Save changes", ""),
				withChildren(ui.Button(ui.ButtonProps{Type: ui.ButtonTypeButton, Variant: ui.ButtonGhost, Size: ui.ButtonSM}), templ.Raw("Cancel")))
		},
	},
	"FormControl": {
		PropsType:   "name, label string, labelPosition LabelPosition, hint, errMsg string, attrs templ.Attributes",
		Description: "A fieldset wrapper pairing a label with an input and optional hint.",
		Render: func() templ.Component {
			return withChildren(form.FormControl("gallery-name", "Project name", form.LabelAbove, "Used across the workspace.", "", nil),
				templ.Raw(`<input id="gallery-name" name="gallery-name" type="text" class="input w-full" placeholder="memory"/>`))
		},
	},
	"FormField": {
		PropsType:   "FormFieldProps",
		Description: "A unified form field that picks its input by type.",
		Render: func() templ.Component {
			return form.FormField(form.FormFieldProps{Type: form.FieldEmail, Name: "gallery-email", Label: "Email", Value: "ada@example.com", Placeholder: "you@example.com"})
		},
	},
	"IconPicker": {
		PropsType:   "IconPickerProps",
		Description: "A searchable icon listbox with a hidden committed value.",
		Render: func() templ.Component {
			return ui.IconPicker(ui.IconPickerProps{
				ID:           "gallery-icon",
				Name:         "icon",
				Value:        "lucide--box",
				DefaultValue: "lucide--box",
				DefaultLabel: "Box",
				DefaultClass: "lucide--box",
				Options: []ui.IconPickerOption{
					{Name: "lucide--box", Label: "Box", Class: "lucide--box"},
					{Name: "lucide--star", Label: "Star", Class: "lucide--star"},
					{Name: "lucide--key-round", Label: "Key", Class: "lucide--key-round"},
				},
			})
		},
	},
	"IndicatorWrapper": {
		PropsType:   "—",
		Description: "The indicator container that positions a badge overlay on its content.",
		Render: func() templ.Component {
			return withChildren(ui.IndicatorWrapper(),
				join(
					templ.Raw(`<span class="indicator-item indicator-bottom indicator-center badge badge-primary">3</span>`),
					templ.Raw(`<div class="grid h-16 w-16 place-items-center rounded-box bg-base-300 text-base-content/60">Inbox</div>`),
				),
			)
		},
	},
	"List": {
		PropsType:   "ListProps",
		Description: "A DaisyUI list container with an optional header row.",
		Render: func() templ.Component {
			return withChildren(ui.List(ui.ListProps{Header: "Recent"}),
				templ.Raw(`<li class="list-row"><div class="list-col-grow"><div class="font-medium text-sm">Memory</div><div class="text-xs text-base-content/50">project</div></div></li><li class="list-row"><div class="list-col-grow"><div class="font-medium text-sm">Voice key</div><div class="text-xs text-base-content/50">credential</div></div></li>`))
		},
	},
	"ListRow": {
		PropsType:   "ListRowProps",
		Description: "A single row inside a go-daisy List: leading slot, title/subtitle block, and trailing components.",
		Render: func() templ.Component {
			return ui.ListRow(ui.ListRowProps{
				Title:    "Scheduled run",
				Subtitle: "Agent name · 2h ago",
				Leading:  ui.IconSpan("lucide--messages-square", "size-4 text-muted-subtle"),
				Trailing: []templ.Component{ui.IconSpan("lucide--chevron-right", "size-4 text-muted-subtle")},
			})
		},
	},
	"Navbar": {
		PropsType:   "appName string, logo ...templ.Component",
		Description: "The top navigation bar with an app name and a right-side content slot.",
		Render: func() templ.Component {
			return withChildren(layout.Navbar("memory"), templ.Raw(`<span class="text-sm text-base-content/60">signed in as ada</span>`))
		},
	},
	"PageHeading": {
		PropsType:   "PageHeadingProps",
		Description: "A page heading with breadcrumbs, title, and subtitle.",
		Render: func() templ.Component {
			return nav.PageHeading(nav.PageHeadingProps{
				Title:    "Project settings",
				Subtitle: "Manage the project's configuration.",
				Breadcrumbs: []nav.BreadcrumbItem{
					{Label: "Projects", Href: "#"},
					{Label: "memory", Active: true},
				},
			})
		},
	},
	"PasswordField": {
		PropsType:   "PasswordFieldProps",
		Description: "A password input with an optional show/hide toggle.",
		Render: func() templ.Component {
			return form.PasswordField(form.PasswordFieldProps{Name: "gallery-password", Label: "API key", Placeholder: "Enter a secret", ShowToggle: true})
		},
	},
	"Popover": {
		PropsType:   "props ...PopoverRootProps",
		Description: "A popover root scoping the trigger and content; shown in composition.",
		Render:      galleryPopoverFixture,
	},
	"PopoverContent": {
		PropsType:   "props ...PopoverContentProps",
		Description: "The floating popover panel; shown in composition.",
		Render:      galleryPopoverFixture,
	},
	"PopoverScript": {
		PropsType:   "—",
		Description: "The popover runtime script, shown with a composed popover.",
		Render: func() templ.Component {
			return join(galleryPopoverFixture(), ui.PopoverScript())
		},
	},
	"PopoverTrigger": {
		PropsType:   "props ...PopoverTriggerProps",
		Description: "The popover trigger button; shown in composition.",
		Render:      galleryPopoverFixture,
	},
	"Rail": {
		PropsType:   "nav templ.Component, attrs templ.Attributes",
		Description: "A two-column settings layout with a sticky navigation rail.",
		Render: func() templ.Component {
			return withChildren(layout.Rail(templ.Raw(`<ul class="menu w-full gap-0.5"><li><a class="menu-item active">General</a></li><li><a class="menu-item">Voice</a></li></ul>`), nil),
				templ.Raw(`<p class="text-sm">Main content column.</p>`))
		},
	},
	"Section": {
		PropsType:   "title string, props SectionProps",
		Description: "A titled card section wrapping a body of content.",
		Render: func() templ.Component {
			return withChildren(ui.Section("Overview", ui.SectionProps{}), templ.Raw(`<p class="text-sm">Section body content.</p>`))
		},
	},
	"SidebarNavItem": {
		PropsType:   "item SidebarItem",
		Description: "A single flat navigation link in the sidebar.",
		Render: func() templ.Component {
			return layout.SidebarNavItem(layout.SidebarItem{Label: "Dashboard", Href: "#", Icon: "lucide--layout-dashboard", Active: true})
		},
	},
	"SoftBox": {
		PropsType:   "props ...SoftBoxProps",
		Description: "A muted rounded panel wrapping its children.",
		Render: func() templ.Component {
			return withChildren(ui.SoftBox(ui.SoftBoxProps{}), templ.Raw(`<p class="text-sm">A soft callout panel.</p>`))
		},
	},
	"StatCard": {
		PropsType:   "StatCardProps",
		Description: "A compact summary widget with an icon, value, and label.",
		Render: func() templ.Component {
			return ui.StatCard(ui.StatCardProps{Label: "Active sessions", Value: "12", Icon: "lucide--activity", IconColor: "bg-primary/10 text-primary"})
		},
	},
	"TableBody": {
		PropsType:   "—",
		Description: "The table body wrapper; the table family, shown in its composed context.",
		Render:      galleryTableFixture,
	},
	"TableCell": {
		PropsType:   "class string, attrs templ.Attributes",
		Description: "A table cell with an optional extra class; shown in its composed context.",
		Render:      galleryTableFixture,
	},
	"TableHead": {
		PropsType:   "—",
		Description: "The table head wrapper; shown in its composed context.",
		Render:      galleryTableFixture,
	},
	"TableHeadCell": {
		PropsType:   "label string",
		Description: "A plain column header cell; shown in its composed context.",
		Render:      galleryTableFixture,
	},
	"TableHeadRow": {
		PropsType:   "—",
		Description: "A header row; shown in its composed context.",
		Render:      galleryTableFixture,
	},
	"TableRow": {
		PropsType:   "id string, hover bool, attrs templ.Attributes",
		Description: "A table row with an optional id and hover; shown in its composed context.",
		Render:      galleryTableFixture,
	},
	"TableWithProps": {
		PropsType:   "TableProps",
		Description: "A DaisyUI table with a canonical wrapper; the table family, shown in its composed context.",
		Render:      galleryTableFixture,
	},
	"go-daisy/alpine#Tag": {
		PropsType:   "—",
		Description: "The Alpine.js runtime script tag, included in the page head.",
		Render: func() templ.Component {
			return alpine.Tag()
		},
	},
	"go-daisy/behaviors#Tag": {
		PropsType:   "—",
		Description: "The go-daisy behaviour-init runtime script tag, included in the page head before component scripts.",
		Render: func() templ.Component {
			return behaviors.Tag()
		},
	},
	"TagList": {
		PropsType:   "TagListProps",
		Description: "An Alpine-managed tags input with chips and suggestions.",
		Render: func() templ.Component {
			return form.TagList(form.TagListProps{
				ID:          "gallery-tags",
				Name:        "tags",
				Values:      []string{"golang", "templ"},
				Suggestions: []string{"htmx", "alpine", "postgres"},
				Placeholder: "Add a tag...",
			})
		},
	},
	"ToastQueueWithProps": {
		PropsType:   "p ToastQueueProps, seed ...alpine.ToastItem",
		Description: "A fixed-position toast queue seeded with sample notifications.",
		Render: func() templ.Component {
			return ui.ToastQueueWithProps(ui.ToastQueueProps{},
				alpine.ToastItem{ID: "1", Type: "success", Message: "Saved changes.", Duration: 4000},
				alpine.ToastItem{ID: "2", Type: "error", Message: "Something failed.", Duration: 0},
			)
		},
	},
	"ToggleInput": {
		PropsType:   "ToggleInputProps",
		Description: "A bare DaisyUI toggle switch input.",
		Render: func() templ.Component {
			return form.ToggleInput(form.ToggleInputProps{Name: "gallery-enabled", Checked: true, Class: "toggle toggle-primary"})
		},
	},
}

// daisyFixture returns the authored fixture for a go-daisy id, and whether one
// exists. A miss means the id has no curated fixture and daisyEntry falls back
// to its empty render.
func daisyFixture(id string) (daisyFixtureSpec, bool) {
	spec, ok := daisyFixtures[id]
	return spec, ok
}

// galleryPopoverFixture renders the composed popover markup: a root scoping a
// visible trigger and content panel. It is shared by the four popover entries so
// no preview is blank, with PopoverScript adding the runtime script on top.
func galleryPopoverFixture() templ.Component {
	return withChildren(ui.Popover(ui.PopoverRootProps{}),
		join(
			withChildren(ui.PopoverTrigger(ui.PopoverTriggerProps{}), templ.Raw(`<span class="iconify lucide--info size-4"></span> More info`)),
			withChildren(ui.PopoverContent(ui.PopoverContentProps{Placement: ui.PopoverBottom}), templ.Raw(`<p class="px-3 py-2 text-sm">A popover explaining this field.</p>`)),
		),
	)
}

// galleryTableFixture renders the full table example in its composed context:
// TableWithProps -> TableHead -> TableHeadRow -> TableHeadCell, then TableBody
// -> TableRow -> TableCell with real-looking rows. It is shared by the seven
// table entries so a bare <tbody/> never shows as a blank preview.
func galleryTableFixture() templ.Component {
	cell := func(s string) templ.Component {
		return withChildren(table.TableCell("", nil), templ.Raw(s))
	}
	row := func(name, typ, updated string) templ.Component {
		return withChildren(table.TableRow("", false, nil), join(cell(name), cell(typ), cell(updated)))
	}
	head := withChildren(table.TableHead(),
		withChildren(table.TableHeadRow(),
			join(table.TableHeadCell("Name"), table.TableHeadCell("Type"), table.TableHeadCell("Updated"))))
	body := withChildren(table.TableBody(),
		join(row("memory", "project", "2h ago"), row("voice-key", "credential", "1d ago")))
	return withChildren(table.TableWithProps(table.TableProps{Striped: true, Bordered: true}), join(head, body))
}
