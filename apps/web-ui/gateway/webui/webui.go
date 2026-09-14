// Package webui embeds Memory's web assets (brand CSS + client JS) so the
// gateway binary serves the full UI with no runtime filesystem dependency.
//
// The package's assets live under static/ and are served at /assets/* by the
// gateway's echo server. go-daisy's own static files keep their /static/*
// mount — no path collision.
//
// Asset URLs carry a content hash (?v=<hash>) derived from the embedded files,
// so a rebuilt binary with changed assets produces new URLs and browsers
// re-fetch instead of serving a stale cached copy.
package webui

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"net/http"
	"path"
	"sync"
)

//go:embed all:static
var files embed.FS

// assetHash caches a content hash of the embedded static/ tree, computed
// lazily and once per process.
var (
	hashOnce sync.Once
	hash     string
)

// version returns a short content hash of the embedded assets. The hash covers
// every file's path and bytes, so any asset change yields a different value
// (and therefore a different ?v= cache-busting URL).
func version() string {
	hashOnce.Do(func() {
		h := sha256.New()
		_ = fs.WalkDir(files, ".", func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			data, err := fs.ReadFile(files, p)
			if err != nil {
				return nil
			}
			h.Write([]byte(p))
			h.Write(data)
			return nil
		})
		hash = hex.EncodeToString(h.Sum(nil))[:16]
	})
	return hash
}

// FS returns the embedded file system rooted at "static/".
func FS() fs.FS {
	sub, err := fs.Sub(files, "static")
	if err != nil {
		panic("webui: " + err.Error())
	}
	return sub
}

// Handler returns an http.Handler serving the embedded assets, stripping the
// given URL prefix (e.g. "/assets/").
func Handler(prefix string) http.Handler {
	fileServer := http.FileServer(http.FS(FS()))
	return http.StripPrefix(prefix, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Asset URLs are content-hashed (AssetPath), so they are immutable:
		// cache for a year and let a changed ?v= hash bust the cache rather
		// than revalidating on every request. Overrides the global no-cache
		// middleware for this mount only.
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		// Force a correct MIME type for .js on OSes whose mime.types map
		// .js to text/plain (Arch Linux containers), which breaks scripts
		// under CORB/MIME checking.
		switch path.Ext(path.Clean(r.URL.Path)) {
		case ".js":
			w.Header().Set("Content-Type", "application/javascript")
		case ".css":
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
		case ".svg":
			w.Header().Set("Content-Type", "image/svg+xml")
		case ".webmanifest":
			// Browsers reject manifests not served as manifest+json.
			w.Header().Set("Content-Type", "application/manifest+json")
		case ".png":
			w.Header().Set("Content-Type", "image/png")
		}
		fileServer.ServeHTTP(w, r)
	}))
}

// AssetPath returns the full URL for an embedded asset with cache-busting,
// e.g. AssetPath("/js/chat.js") → "/assets/js/chat.js?v=<content-hash>".
func AssetPath(p string) string {
	return "/assets" + p + "?v=" + version()
}
