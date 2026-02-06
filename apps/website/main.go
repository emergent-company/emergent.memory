package main

import (
	"embed"
	"io/fs"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/emergentai/emergent/apps/website/internal/config"
	"github.com/emergentai/emergent/apps/website/internal/handlers"
)

//go:embed static
var staticFS embed.FS

func main() {
	cfg := config.Load()

	r := chi.NewRouter()

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	staticSub, err := fs.Sub(staticFS, "static")
	if err != nil {
		log.Fatal("Failed to access static files:", err)
	}
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))

	r.Get("/", handlers.LandingPage)
	r.Get("/health", handlers.Health)

	log.Printf("🚀 Server starting on http://localhost%s", cfg.Port)
	if err := http.ListenAndServe(cfg.Port, r); err != nil {
		log.Fatal("Server failed to start:", err)
	}
}
