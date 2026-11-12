package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// Type-declared object view (ui.view).
//
// A compiled object type can describe how its instances are presented by adding
// a `view` block to the same `ui` object that already carries its icon and
// colour — the block is authored with the data type, so a project that installs
// the `pm-assets` pack renders a Property the same way everywhere without any
// per-page configuration:
//
//	ui:
//	  icon: 🏠
//	  view:
//	    inline: [status, address]      # facts a preview of this type shows
//	    sections:                      # relationship groups, in reading order
//	      - title: Utilities
//	        relation: [serves]
//	        dir: in                    # in | out | both (default)
//	        limit: 8
//	      - title: Obligations
//	        relation: has_obligation   # a bare string is accepted too
//
// The vocabulary is deliberately closed: no expressions, no templating, no
// HTML. A view is data written by a schema pack, and the renderer must stay
// safe for packs it has never seen. Anything the parser does not recognise is
// rejected, and a rejected view is treated exactly like no view at all — the
// page falls back to the generic relationship list instead of failing.

// viewDefaultLimit is the rows a declared section shows before it truncates.
const viewDefaultLimit = 8

// viewMaxLimit bounds a declared limit so a pack cannot turn one page into an
// unbounded fan-out (the measured worst case in a real project is 187 edges on
// a single object).
const viewMaxLimit = 50

// typeUIView is a type's presentation plan, parsed from its `ui.view` block.
type typeUIView struct {
	// Inline names the properties a depth-1 preview of this type shows, in
	// order. Empty means "use the default fact selection" (status, then the
	// first scalar properties).
	Inline []string
	// Sections declares the relationship groups, in reading order.
	Sections []typeUISection
	// More renders the remaining, undeclared relationship types as one
	// trailing group. Defaults to true: a relationship type an author forgot
	// about must still be visible.
	More bool
}

// typeUISection is one declared relationship group.
type typeUISection struct {
	// Title is the section heading. Empty means "derive it from Relation".
	Title string
	// Relation lists the relationship types the section collects.
	Relation []string
	// Dir restricts the section to incoming or outgoing edges. Empty (or
	// "both") collects both directions.
	Dir string
	// Limit caps the rows rendered. Zero means viewDefaultLimit.
	Limit int
}

// direction is the effective direction filter ("in", "out" or "both").
func (s typeUISection) direction() string {
	if s.Dir == "" {
		return "both"
	}
	return s.Dir
}

// rowLimit is the effective row cap.
func (s typeUISection) rowLimit() int {
	if s.Limit == 0 {
		return viewDefaultLimit
	}
	return s.Limit
}

// matches reports whether an edge belongs to this section. A section's
// relation list is matched against the edge's type; the direction filter is
// applied by the caller, which knows which endpoint is the current object.
func (s typeUISection) matches(relType string) bool {
	for _, t := range s.Relation {
		if t == relType {
			return true
		}
	}
	return false
}

// rawUIView mirrors the wire shape. It is decoded with DisallowUnknownFields so
// a typo in a pack is rejected rather than silently ignored.
type rawUIView struct {
	Inline   []string       `json:"inline"`
	Sections []rawUISection `json:"sections"`
	More     *bool          `json:"more"`
}

// rawUISection mirrors one section. Relation accepts a string or a list.
type rawUISection struct {
	Title    string       `json:"title"`
	Relation relationList `json:"relation"`
	Dir      string       `json:"dir"`
	Limit    int          `json:"limit"`
}

// relationList decodes either `relation: serves` or `relation: [serves, ...]`,
// because both read naturally in a hand-written schema pack and forcing one
// spelling only produces typos.
type relationList []string

func (r *relationList) UnmarshalJSON(b []byte) error {
	var single string
	if err := json.Unmarshal(b, &single); err == nil {
		if s := strings.TrimSpace(single); s != "" {
			*r = relationList{s}
		}
		return nil
	}
	var list []string
	if err := json.Unmarshal(b, &list); err != nil {
		return fmt.Errorf("relation must be a string or a list of strings")
	}
	out := make(relationList, 0, len(list))
	for _, s := range list {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	*r = out
	return nil
}

// parseTypeUIView reads the `view` block out of a compiled type's raw `ui`
// object. A type that declares no view returns (nil, nil): "no view" is the
// normal case, not an error.
func parseTypeUIView(raw json.RawMessage) (*typeUIView, error) {
	// Only a ui block that names a view costs a decode. Every other type in a
	// project (the overwhelming majority) takes the two-token path below.
	if len(raw) == 0 || !bytes.Contains(raw, []byte(`"view"`)) {
		return nil, nil
	}
	var wrapper struct {
		View json.RawMessage `json:"view"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return nil, fmt.Errorf("ui block: %w", err)
	}
	if len(wrapper.View) == 0 || string(wrapper.View) == "null" {
		return nil, nil
	}

	dec := json.NewDecoder(bytes.NewReader(wrapper.View))
	dec.DisallowUnknownFields()
	var r rawUIView
	if err := dec.Decode(&r); err != nil {
		return nil, fmt.Errorf("ui.view: %w", err)
	}

	view := &typeUIView{More: true}
	if r.More != nil {
		view.More = *r.More
	}
	for _, name := range r.Inline {
		if name = strings.TrimSpace(name); name != "" {
			view.Inline = append(view.Inline, name)
		}
	}
	for i, rs := range r.Sections {
		if len(rs.Relation) == 0 {
			return nil, fmt.Errorf("ui.view.sections[%d]: relation is required", i)
		}
		switch rs.Dir {
		case "", "in", "out", "both":
		default:
			return nil, fmt.Errorf("ui.view.sections[%d]: dir must be in, out or both", i)
		}
		if rs.Limit < 0 || rs.Limit > viewMaxLimit {
			return nil, fmt.Errorf("ui.view.sections[%d]: limit must be between 0 and %d", i, viewMaxLimit)
		}
		title := strings.TrimSpace(rs.Title)
		if title == "" {
			title = relationshipTypeLabel(rs.Relation[0])
		}
		view.Sections = append(view.Sections, typeUISection{
			Title:    title,
			Relation: rs.Relation,
			Dir:      rs.Dir,
			Limit:    rs.Limit,
		})
	}
	if len(view.Sections) == 0 && len(view.Inline) == 0 {
		return nil, nil
	}
	return view, nil
}

// typeUIViewOf is the render-path wrapper: a malformed view is authored data,
// not a runtime fault, so it degrades to "no view" and the page keeps working.
// Nothing is captured here on purpose — a bad pack would otherwise emit one
// Sentry event per page view.
func typeUIViewOf(raw json.RawMessage) *typeUIView {
	view, err := parseTypeUIView(raw)
	if err != nil {
		return nil
	}
	return view
}
