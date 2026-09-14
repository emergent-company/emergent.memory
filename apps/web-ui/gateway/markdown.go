package main

import (
	"bytes"
	"html"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

var mdRenderer = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
)

var mdSanitizer = bluemonday.UGCPolicy()

// renderMarkdown converts CommonMark/GFM text to sanitized HTML.
// Raw HTML is escaped; dangerous link schemes (javascript:, data:, etc.) are
// stripped by the UGC sanitizer. On render failure it falls back to escaped
// plain text.
func renderMarkdown(text string) string {
	var buf bytes.Buffer
	if err := mdRenderer.Convert([]byte(text), &buf); err != nil {
		return html.EscapeString(text)
	}
	return mdSanitizer.Sanitize(buf.String())
}
