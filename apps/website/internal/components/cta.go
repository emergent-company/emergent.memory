package components

import (
	g "maragu.dev/gomponents"
	. "maragu.dev/gomponents/html"
)

func CTA() g.Node {
	benefits := []string{
		"Production-ready infrastructure (emergent.core)",
		"Open-source and self-hosted",
		"Built on proven patterns and modern tech",
	}

	return Div(
		Class("sm:px-16 container"),
		Div(
			Class("relative py-8 md:py-12 xl:py-16 2xl:pt-24 2xl:pb-48 sm:rounded-[60px] overflow-hidden"),

			Div(Class("max-sm:hidden -bottom-40 absolute bg-secondary blur-[180px] w-72 h-64 start-16")),
			Div(Class("max-sm:hidden -bottom-40 absolute bg-accent blur-[180px] w-72 h-64 -translate-x-1/2 start-1/2")),
			Div(Class("max-sm:hidden -bottom-40 absolute bg-primary blur-[180px] w-72 h-64 end-16")),
			Div(Class("max-sm:hidden z-0 absolute inset-0 opacity-20 grainy")),
			Div(Class("absolute inset-x-0 top-0 h-160 bg-linear-to-b from-(--root-bg) to-transparent max-sm:hidden")),

			Div(
				Class("relative"),
				Div(
					Class("text-center"),
					Div(
						Class("inline-flex items-center bg-linear-to-tr from-secondary to-accent p-2.5 rounded-full text-primary-content"),
						Span(Class("iconify size-5"), g.Attr("data-icon", "lucide:sparkles"), g.Attr("role", "img"), g.Attr("aria-label", "Intelligence")),
					),
					P(Class("mt-4 font-bold text-xl sm:text-2xl lg:text-4xl"), g.Text("Start Building Adaptive Systems")),
					P(Class("inline-block mt-3 max-w-2xl max-sm:text-sm"), g.Text("Whether you're building infrastructure or complete solutions, we provide the tools and patterns for truly intelligent applications.")),
				),

				Div(
					Class("flex justify-center mt-6 xl:mt-8"),
					Ul(
						Class("space-y-3 max-w-md text-center"),
						g.Group(g.Map(benefits, func(benefit string) g.Node {
							return Li(
								Class("flex items-center gap-2 max-sm:text-sm"),
								Span(Class("iconify size-6 text-success"), g.Attr("data-icon", "lucide:badge-check"), g.Attr("role", "img"), g.Attr("aria-label", "Check")),
								g.Text(benefit),
							)
						})),
					),
				),

				Div(
					Class("flex justify-center items-center gap-3 sm:gap-5 mt-6 xl:mt-8"),
					A(
						Href("/emergent-core"),
						Class("group relative gap-3 bg-linear-to-r from-secondary to-accent border-0 text-primary-content text-base btn"),
						Span(Class("iconify size-4 sm:size-5"), g.Attr("data-icon", "lucide:boxes"), g.Attr("role", "img"), g.Attr("aria-label", "Explore")),
						g.Text("Explore Infrastructure"),
					),
					A(
						Href("/admin"),
						Class("btn btn-ghost"),
						g.Text("View Live Demo"),
						Span(Class("iconify size-3.5"), g.Attr("data-icon", "lucide:arrow-right"), g.Attr("role", "img"), g.Attr("aria-label", "Demo")),
					),
				),
			),
		),
	)
}
