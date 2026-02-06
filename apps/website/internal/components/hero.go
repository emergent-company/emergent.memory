package components

import (
	g "maragu.dev/gomponents"
	. "maragu.dev/gomponents/html"
)

func Hero() g.Node {
	return g.Group([]g.Node{
		Div(
			Class("relative z-2 overflow-hidden lg:h-screen"),
			ID("hero"),

			Div(Class("absolute inset-0 -z-1 opacity-20 grainy")),

			Div(
				Class("absolute inset-0 -z-1 overflow-hidden opacity-30 graph3d-background"),
				g.El("svg",
					Class("w-full h-full"),
					g.Attr("style", "display: block"),
				),
			),

			Div(
				Class("container flex items-center justify-center pt-20 md:pt-28 xl:pt-36 2xl:pt-48 pb-20 md:pb-28 xl:pb-36 2xl:pb-48"),
				Div(
					Class("w-100 text-center md:w-120 xl:w-160 2xl:w-200"),

					Div(
						Class("flex justify-center"),
						A(
							Class("inline-flex items-center rounded-full border border-white/60 dark:border-white/5 bg-white/40 dark:bg-white/5 hover:bg-white/60 dark:hover:bg-white/10 py-0.5 ps-1 pe-2 text-sm transition-all"),
							Href("/emergent-core"),

							Div(
								Class("flex justify-center items-center bg-primary/10 dark:bg-white/5 px-1.5 py-0 border border-primary/10 dark:border-white/5 rounded-full font-medium text-primary dark:text-white text-xs"),
								g.Text("NEW"),
							),
							g.Text(" Introducing emergent.core"),
						),
					),

					P(
						Class("mt-3 text-2xl leading-tight font-extrabold tracking-[-0.5px] transition-all duration-1000 md:text-4xl xl:text-5xl 2xl:text-6xl starting:scale-110 starting:blur-md"),
						g.Text("Systems That Learn,"),
						Br(),
						Span(
							Class("animate-background-shift from-secondary via-accent to-primary dark:from-secondary dark:via-accent dark:to-primary bg-linear-to-r bg-[400%,400%] bg-clip-text text-transparent"),
							g.Text("Adapt, and Evolve"),
						),
					),

					P(
						Class("text-base-content/80 mt-5 xl:text-lg"),
						g.Text("Build AI applications on adaptive infrastructure—where knowledge graphs meet intelligent agents, creating systems that understand context, anticipate needs, and evolve with your domain."),
					),

					Div(
						Class("mt-8 inline-flex justify-center gap-3 transition-all duration-1000 starting:scale-110"),
						A(
							Href("/emergent-core"),
							Class("btn btn-primary shadow-primary/20 shadow-xl"),
							Span(Class("iconify size-4"), g.Attr("data-icon", "lucide:boxes")),
							g.Text("Explore Core"),
						),
						A(
							Href("#features"),
							Class("btn btn-ghost"),
							Span(Class("iconify size-4"), g.Attr("data-icon", "lucide:arrow-down")),
							g.Text("Learn More"),
						),
					),
				),
			),
		),

		Div(Class("from-secondary via-accent to-primary dark:from-secondary dark:via-accent dark:to-primary mb-8 h-1 w-full bg-linear-to-r max-xl:mt-6 md:mb-12 xl:mb-16 2xl:mb-28")),
	})
}
