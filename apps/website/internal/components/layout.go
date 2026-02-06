package components

import (
	g "maragu.dev/gomponents"
	. "maragu.dev/gomponents/html"
)

type PageConfig struct {
	Title       string
	Description string
	Theme       string
	OGImage     string
}

func Layout(config PageConfig, content ...g.Node) g.Node {
	if config.Theme == "" {
		config.Theme = "space-asteroid-belt"
	}

	if config.Title == "" {
		config.Title = "Emergent - Adaptive Systems for AI"
	}

	if config.Description == "" {
		config.Description = "Build AI applications on adaptive infrastructure—where knowledge graphs meet intelligent agents."
	}

	return g.Group([]g.Node{
		g.Raw("<!DOCTYPE html>"),
		HTML(
			Lang("en"),
			g.Attr("data-theme", config.Theme),
			Head(
				Meta(Charset("utf-8")),
				Meta(Name("viewport"), Content("width=device-width, initial-scale=1.0")),
				TitleEl(g.Text(config.Title)),
				Meta(Name("description"), Content(config.Description)),

				Meta(g.Attr("property", "og:title"), Content(config.Title)),
				Meta(g.Attr("property", "og:description"), Content(config.Description)),
				Meta(g.Attr("property", "og:type"), Content("website")),
				Meta(g.Attr("property", "og:image"), Content("/static/images/og-image.jpg")),

				Link(Rel("icon"), Href("/static/images/favicon-dark.png"), g.Attr("media", "(prefers-color-scheme: dark)")),
				Link(Rel("icon"), Href("/static/images/favicon-light.png"), g.Attr("media", "(prefers-color-scheme: light)")),

				Link(Rel("stylesheet"), Href("/static/styles.css")),

				Script(Src("https://code.iconify.design/1/1.0.7/iconify.min.js")),
			),
			Body(
				Class("bg-base-100 text-base-content"),
				g.Group(content),

				Script(Type("module"), Src("/static/js/theme.js")),
				Script(Type("module"), Src("/static/js/topbar-scroll.js")),
				Script(Type("module"), Src("/static/js/mobile-menu.js")),
				Script(Src("/static/js/graph3d-background.js")),
			),
		),
	})
}
