package main

import (
	"bytes"
	"html"
	"strings"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
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

// citation is one grounded reference an answer makes to retrieved graph data.
// The wire shape is shared by the live `citations` SSE event and each history
// item's `citations` field.
type citation struct {
	Kind  string `json:"kind"`
	ID    string `json:"id"`
	Type  string `json:"type"`
	Label string `json:"label"`
	URL   string `json:"url"`
}

// neutralizeCitationLinks applies the citation link rule to sanitized answer
// HTML: a `/objects/<id>` anchor whose id is not a validated citation is
// unwrapped to its label text, and an unvalidated `#relationship-<rel>`
// fragment is dropped (the remaining object link then follows the same rule).
// A relationship link stays a link when its relationship fragment is a
// citation, even if its source object is not separately cited.
//
// Only anchors are parsed and rewritten; when nothing changes the original
// string is returned byte-for-byte, so renderMarkdown output is untouched for
// answers with no `/objects/` links. This keeps a single sanitization boundary:
// the caller always feeds in already-bluemonday-sanitized HTML.
func neutralizeCitationLinks(rendered string, citations []citation) string {
	if !strings.Contains(rendered, "/objects/") {
		return rendered
	}
	cited := make(map[string]struct{}, len(citations))
	for _, c := range citations {
		if c.ID != "" {
			cited[c.ID] = struct{}{}
		}
	}
	ctx := &xhtml.Node{Type: xhtml.ElementNode, Data: "div", DataAtom: atom.Div}
	nodes, err := xhtml.ParseFragment(strings.NewReader(rendered), ctx)
	if err != nil {
		return rendered
	}
	changed := false
	for _, n := range nodes {
		if neutralizeNode(n, cited) {
			changed = true
		}
	}
	if !changed {
		return rendered
	}
	var buf bytes.Buffer
	for _, n := range nodes {
		if err := xhtml.Render(&buf, n); err != nil {
			return rendered
		}
	}
	return buf.String()
}

// neutralizeNode walks n's subtree and rewrites every `/objects/` anchor in
// place, reporting whether anything changed. An unwrapped anchor is replaced by
// its children (label text/inline markup) so formatting survives.
func neutralizeNode(n *xhtml.Node, cited map[string]struct{}) bool {
	changed := false
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		if c.Type == xhtml.ElementNode && c.Data == "a" {
			if href, ok := attrVal(c, "href"); ok {
				if objID, relID, isObj := parseObjectRef(href); isObj {
					relValid := relID != "" && isCitation(cited, relID)
					if relID != "" && !relValid {
						setAttr(c, "href", "/objects/"+objID)
						changed = true
					}
					if !isCitation(cited, objID) && !relValid {
						unwrapNode(n, c)
						changed = true
					}
				}
			}
		} else if neutralizeNode(c, cited) {
			changed = true
		}
		c = next
	}
	return changed
}

// parseObjectRef splits a `/objects/<id>[#relationship-<rel>]` href into its
// object id and optional relationship id. isObj reports whether the href is an
// object reference; relID is "" when no relationship fragment is present.
func parseObjectRef(href string) (objID, relID string, isObj bool) {
	path, frag, _ := strings.Cut(href, "#")
	id, ok := strings.CutPrefix(path, "/objects/")
	if !ok || id == "" || strings.ContainsAny(id, "/?#") {
		return "", "", false
	}
	if rel, ok := strings.CutPrefix(frag, "relationship-"); ok {
		relID = rel
	}
	return id, relID, true
}

func isCitation(cited map[string]struct{}, id string) bool {
	_, ok := cited[id]
	return ok
}

func attrVal(n *xhtml.Node, key string) (string, bool) {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val, true
		}
	}
	return "", false
}

func setAttr(n *xhtml.Node, key, val string) {
	for i := range n.Attr {
		if n.Attr[i].Key == key {
			n.Attr[i].Val = val
			return
		}
	}
	n.Attr = append(n.Attr, xhtml.Attribute{Key: key, Val: val})
}

func unwrapNode(parent, child *xhtml.Node) {
	for ch := child.FirstChild; ch != nil; {
		next := ch.NextSibling
		child.RemoveChild(ch)
		parent.InsertBefore(ch, child)
		ch = next
	}
	parent.RemoveChild(child)
}
