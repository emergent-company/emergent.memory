package main

import "github.com/a-h/templ"

// Object headers are declared in Go rather than in object_header.templ: templ's
// parser rejects a named scalar type declared in a .templ file, and the variant
// enum needs one.

// objectHeaderVariant selects the scale of an object header.
type objectHeaderVariant int

const (
	// objectHeaderLead is the page-head scale: the lead tile, the identity line,
	// and the object's name as the page's h1.
	objectHeaderLead objectHeaderVariant = iota
	// objectHeaderRow is the list scale: a row tile, the identity line, the name
	// as a link (or plain text), and an optional one-line summary under it.
	objectHeaderRow
)

// objectHeaderProps describes one object header. Type, Icon, Color and Ref feed
// the identity line and the tile; Label is the object's own name.
type objectHeaderProps struct {
	Variant objectHeaderVariant
	Type    string
	Icon    string
	Color   string
	Ref     string
	Label   string
	// Href makes the name a link. Empty renders plain text, which is what a
	// caller that already wraps the header in its own anchor wants.
	Href string
	// Unresolved dims a neighbour the graph could not resolve and drops the link.
	Unresolved bool
	// Direction, "in" or "out", adds the edge's arrow at the end of the identity
	// line. Only meaningful on a row.
	Direction string
	// Excerpt is an optional summary line under the name. Only used on a row:
	// at lead scale the page's own prose follows the head.
	Excerpt string
	// ExcerptClass replaces the excerpt's default clamp (`line-clamp-2`) with a
	// surface's own density — a browse card shows three lines, a relationship
	// row two. Empty keeps the default.
	ExcerptClass string
	// TestID sets data-testid on the root, so a surface-specific test can still
	// address its own header.
	TestID string
	// IdentityTestID sets data-testid on the identity line, for a surface that
	// asserts the type · ref line itself.
	IdentityTestID string
	// NameClass appends surface-specific classes to the name element, for a
	// caller that already owns the hover affordance (an enclosing anchor's
	// `group`). Empty leaves the component's own classes alone.
	NameClass string
	// Trailing renders at the end of the name line — an edge badge, a state chip.
	Trailing templ.Component
}
