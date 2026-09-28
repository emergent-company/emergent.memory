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
	// Key is the object's human key (e.g. "lov/2005-06-17-90") when the answer
	// referenced it by key; the id/url remain canonical. Empty for id refs.
	Key string `json:"key,omitempty"`
}

// neutralizeCitationLinks applies the citation link rule to sanitized answer
// HTML: a `/objects/<ref>` anchor whose ref is not a validated citation is
// unwrapped to its label text, and an unvalidated `#relationship-<rel>`
// fragment is dropped (the remaining object link then follows the same rule).
// A relationship link stays a link when its relationship fragment is a
// citation, even if its source object is not separately cited.
//
// A ref may be a canonical id OR an object's human key (e.g.
// "lov/2005-06-17-90"). A key ref that resolves to a cited object is KEPT and
// re-targeted to that object's canonical `/objects/<id>` page, so the rendered
// link resolves; a ref that matches neither a cited id nor a cited key is
// demoted like any unknown reference.
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
	keys := make(map[string]string, len(citations))
	for _, c := range citations {
		if c.ID == "" {
			continue
		}
		cited[c.ID] = struct{}{}
		if c.Key != "" {
			keys[c.Key] = c.ID
		}
	}
	ctx := &xhtml.Node{Type: xhtml.ElementNode, Data: "div", DataAtom: atom.Div}
	nodes, err := xhtml.ParseFragment(strings.NewReader(rendered), ctx)
	if err != nil {
		return rendered
	}
	changed := false
	for _, n := range nodes {
		if neutralizeNode(n, cited, keys) {
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
func neutralizeNode(n *xhtml.Node, cited map[string]struct{}, keys map[string]string) bool {
	changed := false
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		if c.Type == xhtml.ElementNode && c.Data == "a" {
			if href, ok := attrVal(c, "href"); ok {
				if ref, relID, isObj := parseObjectRef(href); isObj {
					canonical, known := "", false
					if isCitation(cited, ref) {
						canonical, known = ref, true
					} else if id, ok := keys[ref]; ok {
						// A key ref resolves to its object's canonical id.
						canonical, known = id, true
					}
					relValid := relID != "" && isCitation(cited, relID)
					frag := ""
					if relValid {
						frag = "#relationship-" + relID
					}
					if !known && !relValid {
						unwrapNode(n, c)
						changed = true
					} else {
						// Re-target a known ref to its canonical id; an unknown
						// ref (kept only because its relationship is cited) stays
						// as-is.
						target := ref
						if known {
							target = canonical
						}
						if newHref := "/objects/" + target + frag; newHref != href {
							setAttr(c, "href", newHref)
							changed = true
						}
					}
				}
			}
		} else if neutralizeNode(c, cited, keys) {
			changed = true
		}
		c = next
	}
	return changed
}

// parseObjectRef splits a `/objects/<ref>[#relationship-<rel>]` href into its
// object ref (a canonical id or a human key) and optional relationship id.
// isObj reports whether the href is an object reference; relID is "" when no
// relationship fragment is present. The ref may contain "/" (key refs like
// `lov/2005-06-17-90`); a query string or an empty ref is rejected.
func parseObjectRef(href string) (ref, relID string, isObj bool) {
	path, frag, _ := strings.Cut(href, "#")
	ref, ok := strings.CutPrefix(path, "/objects/")
	if !ok || ref == "" || strings.ContainsRune(ref, '?') {
		return "", "", false
	}
	if rel, ok := strings.CutPrefix(frag, "relationship-"); ok {
		relID = rel
	}
	return ref, relID, true
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
