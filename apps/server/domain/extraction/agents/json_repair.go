package agents

import (
	"encoding/json"
	"strings"
)

// repairTruncatedJSON attempts to recover a valid JSON object from a string that
// was truncated mid-stream (e.g. due to LLM token-limit cutoff).
//
// Strategy: walk the string tracking open braces/brackets. If the final parse
// fails with "unexpected end of JSON input", append the minimum closing tokens
// needed to produce parseable JSON, then unmarshal into dest.
//
// Only keys already present in the partial output are populated; no data is
// fabricated. Returns an error only if the repaired string still cannot be
// parsed.
func repairTruncatedJSON(s string, dest interface{}) error {
	// Fast path: already valid.
	if err := json.Unmarshal([]byte(s), dest); err == nil {
		return nil
	}

	// Strip trailing incomplete string literals or partial field names so the
	// closing tokens land at a clean boundary.
	s = trimIncompleteToken(s)

	// Build the closing suffix by counting unmatched openers.
	var stack []byte
	inString := false
	escaped := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if escaped {
			escaped = false
			continue
		}
		if c == '\\' && inString {
			escaped = true
			continue
		}
		if c == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		switch c {
		case '{':
			stack = append(stack, '}')
		case '[':
			stack = append(stack, ']')
		case '}', ']':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}

	// Append closing tokens in reverse order.
	var sb strings.Builder
	sb.WriteString(s)
	for i := len(stack) - 1; i >= 0; i-- {
		sb.WriteByte(stack[i])
	}

	return json.Unmarshal([]byte(sb.String()), dest)
}

// trimIncompleteToken removes trailing bytes that would prevent a clean close:
// an incomplete string literal, a dangling comma, or a partial key/value.
func trimIncompleteToken(s string) string {
	// Walk backwards to find a safe truncation point.
	// Safe points: after '}', ']', '"' (end of string value), or a digit.
	inString := false
	escaped := false

	// Find the last position that ends a complete value.
	lastSafe := -1
	for i := 0; i < len(s); i++ {
		c := s[i]
		if escaped {
			escaped = false
			continue
		}
		if c == '\\' && inString {
			escaped = true
			continue
		}
		if c == '"' {
			if inString {
				// End of a string value.
				lastSafe = i
			}
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		switch c {
		case '}', ']':
			lastSafe = i
		}
	}

	// If we're mid-string or ended on a non-safe char, truncate to lastSafe.
	if inString && lastSafe >= 0 {
		return s[:lastSafe+1]
	}
	// Strip trailing commas and whitespace.
	result := strings.TrimRight(s, " \t\n\r,")
	return result
}
