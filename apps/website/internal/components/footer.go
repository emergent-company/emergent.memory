package components

import (
	"fmt"

	g "maragu.dev/gomponents"
	. "maragu.dev/gomponents/html"
)

func PageFooter() g.Node {
	currentYear := "2026"

	return Div(
		Class("relative"),

		Div(Class("z-0 absolute inset-0 opacity-20 grainy")),

		Div(
			Class("z-[2] relative pt-8 md:pt-12 2xl:pt-24 xl:pt-16 container"),

			Div(
				Class("gap-6 grid grid-cols-2 md:grid-cols-5"),

				Div(
					Class("col-span-2"),
					Logo(),

					P(
						Class("mt-3 max-sm:text-sm text-base-content/80"),
						g.Text("Transform your documents into living intelligence. Emergent automatically structures your knowledge, connects insights, and proactively surfaces what you need."),
					),

					Div(
						Class("flex items-center gap-2.5 mt-6 xl:mt-16"),
						A(Class("btn btn-sm btn-circle"), Href("#"), g.Attr("target", "_blank"),
							Icon("lucide--github", "GitHub"),
						),
						A(Class("btn btn-sm btn-circle"), Href("#"), g.Attr("target", "_blank"),
							Icon("lucide--twitter", "Twitter"),
						),
						A(Class("btn btn-sm btn-circle"), Href("#"),
							Icon("lucide--linkedin", "LinkedIn"),
						),
					),
				),

				Div(Class("max-md:hidden xl:col-span-1")),

				Div(
					Class("col-span-1"),
					P(Class("font-medium"), g.Text("Product")),
					Div(
						Class("flex flex-col space-y-1.5 mt-5 text-base-content/80"),
						A(Href("/"), g.Text("Vision")),
						A(Href("/emergent-core"), g.Text("emergent.core")),
						A(Href("/automation"), g.Text("emergent.automator")),
						A(Href("/admin"), g.Text("Dashboard")),
					),
				),

				Div(
					Class("col-span-1"),
					P(Class("font-medium"), g.Text("Resources")),
					Div(
						Class("flex flex-col space-y-1.5 mt-5 text-base-content/80"),
						A(Href("/admin/documents"), g.Text("Documents")),
						A(Href("/admin/chat-sdk"), g.Text("Chat")),
						A(Href("#"), g.Text("Documentation")),
						A(Href("#"), g.Text("Help Center")),
						A(Href("#"), g.Text("Support")),
					),
				),
			),

			Div(
				Class("flex flex-wrap justify-between items-center gap-3 mt-12 py-6 border-t border-base-300"),
				P(g.Text(fmt.Sprintf("© %s Emergent. All rights reserved.", currentYear))),
				ThemePicker(),
			),
		),

		P(
			Class("max-lg:hidden flex justify-center -mt-12 h-[195px] overflow-hidden font-black text-[200px] text-base-content/5 tracking-[12px] whitespace-nowrap select-none"),
			g.Text("EMERGENT"),
		),
	)
}
