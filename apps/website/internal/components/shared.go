package components

import (
	"fmt"
	"strings"

	g "maragu.dev/gomponents"
	. "maragu.dev/gomponents/html"
)

func Logo() g.Node {
	return Div(
		Class("flex items-center gap-2"),
		Span(
			Class("font-bold text-xl"),
			g.Text("Emergent"),
		),
	)
}

func convertIconName(iconClass string) string {
	parts := strings.Fields(iconClass)
	iconName := parts[0]
	return strings.Replace(iconName, "--", ":", 1)
}

func extractSizeClasses(iconClass string) string {
	parts := strings.Fields(iconClass)
	if len(parts) > 1 {
		return strings.Join(parts[1:], " ")
	}
	return ""
}

func Icon(iconClass, ariaLabel string) g.Node {
	iconName := convertIconName(iconClass)
	sizeClasses := extractSizeClasses(iconClass)
	classes := "iconify inline-block"
	if sizeClasses != "" {
		classes = fmt.Sprintf("iconify inline-block %s", sizeClasses)
	}

	if ariaLabel != "" {
		return Span(
			Class(classes),
			g.Attr("data-icon", iconName),
			g.Attr("role", "img"),
			g.Attr("aria-label", ariaLabel),
		)
	}

	return Span(
		Class(classes),
		g.Attr("data-icon", iconName),
		g.Attr("aria-hidden", "true"),
	)
}

func IconBadge(icon, color string) g.Node {
	containerClass := fmt.Sprintf("inline-flex items-center justify-center shrink-0 select-none size-8 rounded-box bg-%s/10 border border-%s/20 transition-colors", color, color)
	iconName := convertIconName(icon)
	sizeClass := fmt.Sprintf("text-%s size-4", color)

	return Span(
		Class(containerClass),
		Span(
			Class(fmt.Sprintf("iconify %s", sizeClass)),
			g.Attr("data-icon", iconName),
		),
	)
}

func ThemePicker() g.Node {
	themes := []struct {
		Value string
		Label string
	}{
		{"space-asteroid-belt", "Dark"},
		{"space-asteroid-belt-light", "Light"},
	}

	return Div(
		Class("dropdown dropdown-end"),
		Div(
			g.Attr("tabindex", "0"),
			g.Attr("role", "button"),
			Class("btn btn-ghost btn-sm gap-1"),
			Icon("lucide--palette", "Theme"),
			Span(Class("max-sm:hidden"), g.Text("Theme")),
		),
		Ul(
			g.Attr("tabindex", "-1"),
			Class("dropdown-content menu bg-base-100 rounded-box z-[1] mt-2 w-40 border border-base-300 p-2 shadow"),
			g.Group(g.Map(themes, func(theme struct {
				Value string
				Label string
			}) g.Node {
				return Li(
					Button(
						Class("theme-option"),
						g.Attr("data-theme", theme.Value),
						g.Text(theme.Label),
					),
				)
			})),
		),
	)
}
