package webcrawl

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
)

// dropTags are element subtrees removed before HTML→markdown conversion: they
// carry navigation, chrome, or embedded content rather than the document body.
var dropTags = map[string]bool{
	"script": true,
	"style":  true,
	"nav":    true,
	"header": true,
	"footer": true,
	"aside":  true,
	"form":   true,
	"iframe": true,
	"object": true,
	"embed":  true,
	"svg":    true,
}

// ToMarkdown converts a fetched body into content for ingestion:
//
//   - text/plain and text/markdown are returned as-is with their media type;
//   - HTML is charset-decoded (via the HTML charset sniffing unless charsetHint
//     is given), narrowed to <main>/<article>/[role=main] (else <body>), has the
//     dropTags subtrees removed, and is converted to Markdown;
//   - anything else (PDF, image, …) yields an empty text and its original media
//     type, so the caller can hand the raw body and that media type to xberg.
//
// The returned content type is what the caller should pass on: "text/markdown"
// for converted HTML, the original media type otherwise.
func ToMarkdown(body []byte, contentType, charsetHint string) (string, string, error) {
	mediaType := strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))

	switch mediaType {
	case "text/plain", "text/markdown", "text/x-markdown":
		return string(body), mediaType, nil
	case "text/html", "application/xhtml+xml":
		decoded, err := decodeHTML(body, contentType, charsetHint)
		if err != nil {
			return "", "", err
		}
		md, err := convertHTMLToMarkdown(decoded)
		if err != nil {
			return "", "", err
		}
		return md, "text/markdown", nil
	default:
		// Non-HTML, non-text content has no Markdown representation.
		return "", mediaType, nil
	}
}

// decodeHTML converts a byte body to UTF-8. When charsetHint is provided it is
// used as the authoritative charset label; otherwise the charset package sniffs
// the content-type charset parameter, BOM, and HTML <meta> elements.
func decodeHTML(body []byte, contentType, charsetHint string) ([]byte, error) {
	src := bytes.NewReader(body)
	if hint := strings.TrimSpace(charsetHint); hint != "" {
		if r, err := charset.NewReaderLabel(hint, src); err == nil {
			return io.ReadAll(r)
		}
		src = bytes.NewReader(body) // reset after an unsupported label
	}
	r, err := charset.NewReader(src, contentType)
	if err != nil {
		return nil, fmt.Errorf("decode html: %w", err)
	}
	return io.ReadAll(r)
}

// convertHTMLToMarkdown parses, narrows, prunes, and converts an HTML document.
func convertHTMLToMarkdown(decoded []byte) (string, error) {
	doc, err := html.Parse(bytes.NewReader(decoded))
	if err != nil {
		return "", fmt.Errorf("parse html: %w", err)
	}
	root := narrowContentRoot(doc)
	dropSubtrees(root)

	out, err := htmltomarkdown.ConvertNode(root)
	if err != nil {
		return "", fmt.Errorf("html-to-markdown: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// narrowContentRoot picks the node whose children should be converted: the
// first <main>, <article>, or [role=main] element, else <body>, else the whole
// document.
func narrowContentRoot(doc *html.Node) *html.Node {
	if n := findFirst(doc, func(n *html.Node) bool {
		if n.Type != html.ElementNode {
			return false
		}
		if n.Data == "main" || n.Data == "article" {
			return true
		}
		for _, a := range n.Attr {
			if a.Key == "role" && a.Val == "main" {
				return true
			}
		}
		return false
	}); n != nil {
		return n
	}
	if b := findFirst(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "body"
	}); b != nil {
		return b
	}
	return doc
}

// dropSubtrees removes every dropTags element from the tree, in place.
func dropSubtrees(root *html.Node) {
	for c := root.FirstChild; c != nil; {
		next := c.NextSibling
		if c.Type == html.ElementNode && dropTags[c.Data] {
			root.RemoveChild(c)
		} else {
			dropSubtrees(c)
		}
		c = next
	}
}

// findFirst does a depth-first search for the first node satisfying match.
func findFirst(n *html.Node, match func(*html.Node) bool) *html.Node {
	if match(n) {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findFirst(c, match); found != nil {
			return found
		}
	}
	return nil
}
