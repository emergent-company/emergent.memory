package cmd

// maskedSecretPlaceholder stands in for a secret that is too short to mask
// safely. It discloses no characters — not even the length — because a short
// value plus a few of its own characters is not a mask.
const maskedSecretPlaceholder = "***"

// secretMaskEdge is the number of characters kept at each end of a masked
// secret: 2*secretMaskEdge = 8 characters are revealed in total.
const secretMaskEdge = 4

// secretMaskMinLen is the shortest secret maskSecret will show any character
// of. Anything shorter is replaced wholesale by maskedSecretPlaceholder.
//
// The mask shows 4 leading and 4 trailing characters, which is what makes a
// configured key recognisable. The threshold is chosen so those 8 characters
// are at most a third of the value: 8/24 = 1/3, leaving len-8 >= 16
// characters — twice what is shown — unknown. Above the threshold the
// disclosed share only shrinks (8/32 = 25%, 8/64 = 12.5%).
//
// The previous shape was first 8 + "..." + last 4 = 12 characters, which
// reached 100% disclosure at 12 characters of input (the slices covered the
// whole value) and 92% at 13. Any boundary near that size is not a mask: a
// 24-character minimum is where "no proper substring of the secret is ever
// printed" holds with the hidden span at least twice the visible one (#2127).
const secretMaskMinLen = 24

// maskSecret renders a credential for human-readable output.
//
// Long enough to redact meaningfully: keep a secretMaskEdge-character prefix
// and suffix so the user can still tell which key is configured. The generic
// split subsumes the old `emt_` special case — the first four characters of an
// `emt_...` token are `emt_`, so the output is still `emt_...abcd`.
//
// Too short to redact safely, including the empty string: disclose nothing and
// return maskedSecretPlaceholder. Callers that want a distinct "(not set)"
// state must test for the empty value themselves before calling, as
// `config show` does.
func maskSecret(secret string) string {
	if len(secret) < secretMaskMinLen {
		return maskedSecretPlaceholder
	}
	return secret[:secretMaskEdge] + "..." + secret[len(secret)-secretMaskEdge:]
}
