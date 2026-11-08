package main

import (
	"os"
	"strings"
	"testing"
)

// TestNeutralIconTileGlyphUsesThemeForeground pins #463. go-daisy's ui/IconTile
// paints a neutral-tone tile's glyph with --color-neutral, and this dark-only
// theme sets that token to a near-black step (oklch 0.265) — so the key, lock
// and history glyphs vanished into the card they sat on. The theme layer lifts
// the tile's foreground to --color-neutral-content and tints its border from
// that same token.
//
// The rule must keep using theme tokens (a raw colour would not follow the
// theme), and it keys on the exact class triple IconTile emits for
// Tone: "neutral" so no other neutral surface inherits the lift.
func TestNeutralIconTileGlyphUsesThemeForeground(t *testing.T) {
	raw, err := os.ReadFile("webui/css/app.css")
	if err != nil {
		t.Fatalf("read app.css: %v", err)
	}

	const selector = `.text-neutral.bg-neutral\/10.border-neutral\/15`
	block := cssRuleBlock(t, string(raw), selector)

	if !strings.Contains(block, "color: var(--color-neutral-content)") {
		t.Errorf("the neutral tile's glyph must be painted with --color-neutral-content; got:\n%s", block)
	}
	if !strings.Contains(block, "border-color: color-mix(in oklch, var(--color-neutral-content)") {
		t.Errorf("the neutral tile's border must be tinted from the same foreground token; got:\n%s", block)
	}
	if strings.Contains(block, "oklch(0.") || strings.Contains(block, "#") {
		t.Errorf("the neutral tile rule must not hardcode a colour — theme tokens only; got:\n%s", block)
	}
}
