package handlers

import (
	"net/http"

	"github.com/emergentai/emergent/apps/website/internal/components"
)

func LandingPage(w http.ResponseWriter, r *http.Request) {
	page := components.Layout(
		components.PageConfig{
			Title:       "Emergent - Adaptive Systems for AI",
			Description: "Transform your documents into living intelligence. Emergent automatically structures your knowledge, connects insights, and proactively surfaces what you need.",
			OGImage:     "/static/images/og-image.jpg",
		},
		components.ProductTopbar(),
		components.Hero(),
		components.Features(),
		components.CTA(),
		components.PageFooter(),
	)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = page.Render(w)
}

func Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}
