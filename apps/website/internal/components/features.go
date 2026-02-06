package components

import (
	g "maragu.dev/gomponents"
	. "maragu.dev/gomponents/html"
)

type Feature struct {
	Icon        string
	Title       string
	Description string
	Color       string
}

func Features() g.Node {
	principles := []Feature{
		{"lucide--network", "Interconnected Context", "Knowledge graphs connect intent to execution—trace decisions, understand dependencies, and maintain coherent understanding across your entire system", "primary"},
		{"lucide--brain", "Intelligent Agency", "AI agents that reason over structured knowledge, anticipate needs, and take autonomous action—from strategic synthesis to artifact generation", "secondary"},
		{"lucide--refresh-cw", "Continuous Adaptation", "Systems that learn from outcomes, evolve with your domain, and refine understanding over time—turning feedback into intelligence", "accent"},
	}

	return Div(
		Class("py-8 md:py-12 2xl:py-24 xl:py-16 container"),

		Div(
			Class("text-center"),
			IconBadge("lucide--sparkles", "primary"),
			P(
				ID("features"),
				Class("mt-4 font-semibold text-2xl sm:text-3xl custom-fade-in"),
				g.Text("Three Principles of Adaptive Systems"),
			),
			P(
				Class("inline-block mt-3 max-w-2xl max-sm:text-sm text-base-content/70"),
				g.Text("Building truly intelligent applications requires more than LLMs—it demands infrastructure that mirrors how knowledge actually works: connected, contextual, and continuously evolving."),
			),
		),

		Div(
			Class("gap-6 2xl:gap-8 grid grid-cols-1 md:grid-cols-3 mt-12 2xl:mt-24 xl:mt-16"),
			g.Group(g.Map(principles, func(p Feature) g.Node {
				return Div(
					Class("hover:bg-base-200/40 border border-base-300 hover:border-base-300/60 transition-all duration-300 card"),
					Div(
						Class("card-body"),
						IconBadge(p.Icon, p.Color),
						P(Class("mt-4 font-semibold text-xl"), g.Text(p.Title)),
						P(Class("mt-2 text-sm text-base-content/80 leading-relaxed"), g.Text(p.Description)),
					),
				)
			})),
		),

		Div(
			Class("mt-16 md:mt-24 2xl:mt-32"),
			Div(
				Class("text-center mb-12"),
				P(Class("font-semibold text-xl sm:text-2xl"), g.Text("From Vision to Reality")),
				P(Class("mt-3 text-base-content/70 max-w-2xl mx-auto"), g.Text("We're building the infrastructure and products that embody these principles")),
			),

			Div(
				Class("grid grid-cols-1 md:grid-cols-2 gap-6"),

				Div(
					Class("card border border-base-300 hover:border-primary/50 transition-all"),
					Div(
						Class("card-body"),
						Div(
							Class("flex items-start justify-between"),
							Div(
								H3(Class("font-semibold text-lg"), g.Text("emergent.core")),
								P(Class("text-sm text-base-content/60"), g.Text("Infrastructure Layer")),
							),
							Span(Class("badge badge-accent badge-sm"), g.Text("Infrastructure Layer")),
						),
						P(Class("mt-3 text-sm text-base-content/80"), g.Text("Production-grade knowledge infrastructure with graph modeling, semantic vectors, RAG pipelines, and agent frameworks—ready to deploy.")),
						Div(
							Class("card-actions mt-4"),
							A(
								Href("/emergent-core"),
								Class("btn btn-primary btn-sm gap-2"),
								Icon("lucide--arrow-right", "Explore"),
								g.Text("Explore Core"),
							),
						),
					),
				),

				Div(
					Class("card border border-base-300 hover:border-secondary/50 transition-all"),
					Div(
						Class("card-body"),
						Div(
							Class("flex items-start justify-between"),
							Div(
								H3(Class("font-semibold text-lg"), g.Text("emergent.product")),
								P(Class("text-sm text-base-content/60"), g.Text("Solution Layer")),
							),
							Span(Class("badge badge-secondary badge-sm"), g.Text("Solution Layer")),
						),
						P(Class("mt-3 text-sm text-base-content/80"), g.Text("Living product bible built on emergent.core—strategic agents that connect intent to execution using knowledge graphs and scientific de-risking.")),
						Div(
							Class("card-actions mt-4"),
							A(
								Href("/solutions"),
								Class("btn btn-ghost btn-sm gap-2"),
								Icon("lucide--info", "Learn More"),
								g.Text("Learn More"),
							),
						),
					),
				),
			),
		),
	)
}
