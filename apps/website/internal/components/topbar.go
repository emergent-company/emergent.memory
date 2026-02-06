package components

import (
	g "maragu.dev/gomponents"
	. "maragu.dev/gomponents/html"
)

func ProductTopbar() g.Node {
	return Div(
		g.Attr("data-scrolling", ""),
		g.Attr("data-at-top", "true"),
		Class("group fixed inset-x-0 z-[60] flex justify-center transition-[top] duration-500 data-[scrolling=down]:-top-full sm:container [&:not([data-scrolling=down])]:top-0 [&:not([data-scrolling=down])]:sm:top-4"),

		Div(
			Class("flex justify-between items-center group-data-[at-top=false]:bg-base-100 group-data-[at-top=false]:dark:bg-base-200 group-data-[at-top=false]:shadow px-3 sm:px-6 py-3 lg:py-1.5 sm:rounded-full w-full group-data-[at-top=false]:w-[800px] transition-all duration-500"),

			Div(
				Class("flex items-center gap-2"),

				Div(
					Class("lg:hidden flex-none"),
					Div(
						Class("drawer"),
						Input(
							ID("landing-menu-drawer"),
							Type("checkbox"),
							Class("drawer-toggle"),
						),
						Div(
							Class("drawer-content"),
							Label(
								g.Attr("for", "landing-menu-drawer"),
								Class("btn drawer-button btn-ghost btn-square btn-sm"),
								Span(
									Class("size-4.5"),
									Span(Class("iconify size-4.5"), g.Attr("data-icon", "lucide:menu"), g.Attr("aria-hidden", "true")),
								),
							),
						),
						Div(
							Class("z-[50] drawer-side"),
							Label(
								g.Attr("for", "landing-menu-drawer"),
								g.Attr("aria-label", "close sidebar"),
								Class("drawer-overlay"),
							),
							Ul(
								Class("bg-base-100 p-4 w-80 min-h-full text-base-content menu"),
								Li(
									A(Href("/admin"), g.Text("Dashboard")),
								),
							),
						),
					),
				),

				A(
					Href("/admin"),
					Logo(),
				),
			),

			Ul(
				Class("hidden lg:inline-flex gap-2 px-0 menu menu-horizontal"),
				Li(
					A(Href("/admin"), g.Text("Dashboard")),
				),
			),

			Div(
				Class("inline-flex items-center gap-3"),

				ThemePicker(),

				A(
					Href("https://daisyui.com/store/244268?aff=Db6q2"),
					g.Attr("target", "_blank"),
					Class("group/purchase relative gap-2 bg-linear-to-r from-primary to-secondary border-0 text-primary-content text-sm btn btn-sm max-sm:btn-square"),
					Span(Class("iconify size-4"), g.Attr("data-icon", "lucide:shopping-cart"), g.Attr("aria-hidden", "true")),
					Span(Class("max-sm:hidden"), g.Text("Buy Now")),
					Div(Class("top-1 -z-1 absolute inset-x-0 bg-linear-to-r from-primary to-secondary opacity-40 group-hover/purchase:opacity-60 blur-md group-hover/purchase:blur-lg h-8 transition-all duration-500")),
				),
			),
		),
	)
}
