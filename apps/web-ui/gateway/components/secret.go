package components

import "cmp"

// SecretRevealProps helper accessors. Kept in a .go file so the templ bodies
// stay declarative.

// noun is the noun used in the one-time notice.
func (p SecretRevealProps) noun() string {
	return cmp.Or(p.Noun, "key")
}

// copyLabel is the secret copy control's label.
func (p SecretRevealProps) copyLabel() string {
	return cmp.Or(p.CopyLabel, "Copy key")
}
