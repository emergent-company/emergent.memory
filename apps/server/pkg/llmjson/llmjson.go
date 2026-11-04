// Package llmjson parses JSON produced by a language model.
//
// Models occasionally wrap the object in prose or markdown fences, emit raw
// control characters inside strings, add a parenthetical after a value, or get
// truncated at the output limit. Decode applies the cheap, non-fabricating
// repairs in order and returns the original parse error when none recover.
package llmjson

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Decode unmarshals raw model output into dest, repairing common malformations.
// It never invents field values: repairs only remove invalid tokens or close
// structures that the model left open.
func Decode(raw string, dest interface{}) error {
	_, _, err := DecodeWithMeta(raw, dest)
	return err
}

// DecodeWithMeta is Decode plus the repair flags callers need to distinguish a
// clean response from one that was salvaged. Truncated means the model stopped
// mid-value; the destination may then contain only the complete prefix.
func DecodeWithMeta(raw string, dest interface{}) (repaired, truncated bool, err error) {
	normalized := Normalize(raw)
	repaired = normalized != strings.TrimSpace(raw)

	if err := json.Unmarshal([]byte(normalized), dest); err == nil {
		return repaired, false, nil
	} else {
		originalErr := err
		truncated = isTruncationError(originalErr)

		if balanced := balanceStructure(normalized); balanced != normalized {
			repaired = true
			if err := json.Unmarshal([]byte(balanced), dest); err == nil {
				return repaired, truncated, nil
			}
			normalized = balanced
		}

		if err := repairTruncated(normalized, dest); err == nil {
			return true, truncated, nil
		}
		if truncated {
			return repaired, truncated, fmt.Errorf("truncated JSON: %w", originalErr)
		}
		return repaired, truncated, originalErr
	}
}

func isTruncationError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unexpected end of json") || strings.Contains(msg, "unexpected eof")
}

// Preview returns a bounded single-line rendering of model output for error
// messages and logs.
func Preview(raw string, limit int) string {
	s := strings.Join(strings.Fields(raw), " ")
	if limit <= 0 || len(s) <= limit {
		return s
	}
	return s[:limit] + "…"
}

// Normalize strips markdown fences and surrounding prose, isolates the
// outermost JSON value, and escapes raw control characters that appear inside
// string literals (a common cause of "invalid character '\n' in string
// literal").
func Normalize(raw string) string {
	s := strings.TrimSpace(raw)

	if strings.HasPrefix(s, "```") {
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			s = s[i+1:]
		} else {
			s = strings.TrimPrefix(s, "```json")
			s = strings.TrimPrefix(s, "```")
		}
		if i := strings.LastIndex(s, "```"); i >= 0 {
			s = s[:i]
		}
		s = strings.TrimSpace(s)
	}

	// Drop leading prose before the first object/array and trailing prose after
	// the last one. Truncated JSON has no final closer, so keep the tail then.
	if start := strings.IndexAny(s, "{["); start >= 0 {
		s = s[start:]
	}
	if end := strings.LastIndexAny(s, "}]"); end >= 0 {
		s = s[:end+1]
	}

	return sanitizeControlChars(s)
}

// sanitizeControlChars escapes literal control characters found inside a JSON
// string. Valid escape sequences are left untouched.
func sanitizeControlChars(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inString := false
	escaped := false

	for i := 0; i < len(s); i++ {
		c := s[i]
		if escaped {
			b.WriteByte(c)
			escaped = false
			continue
		}
		if inString && c == '\\' {
			b.WriteByte(c)
			escaped = true
			continue
		}
		if c == '"' {
			inString = !inString
			b.WriteByte(c)
			continue
		}
		if inString && c < 0x20 {
			switch c {
			case '\n':
				b.WriteString(`\n`)
			case '\r':
				b.WriteString(`\r`)
			case '\t':
				b.WriteString(`\t`)
			default:
				fmt.Fprintf(&b, `\u%04x`, c)
			}
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// balanceStructure removes tokens that make otherwise-recoverable JSON
// invalid: mismatched closing brackets and parenthetical notes after a value.
// Unmatched openers are left for repairTruncated to close.
func balanceStructure(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	var stack []byte
	inString := false
	escaped := false

	for i := 0; i < len(s); i++ {
		c := s[i]
		if escaped {
			b.WriteByte(c)
			escaped = false
			continue
		}
		if inString && c == '\\' {
			b.WriteByte(c)
			escaped = true
			continue
		}
		if c == '"' {
			inString = !inString
			b.WriteByte(c)
			continue
		}
		if inString {
			b.WriteByte(c)
			continue
		}

		switch c {
		case '{':
			stack = append(stack, '}')
			b.WriteByte(c)
		case '[':
			stack = append(stack, ']')
			b.WriteByte(c)
		case '}', ']':
			if len(stack) > 0 && stack[len(stack)-1] == c {
				stack = stack[:len(stack)-1]
				b.WriteByte(c)
			}
			// A closer that does not match the current opener is stray; drop it.
		case '(':
			// Drop a balanced parenthetical note, e.g. "value" (estimated).
			depth := 1
			j := i + 1
			inStr := false
			esc := false
			for ; j < len(s); j++ {
				cc := s[j]
				if esc {
					esc = false
					continue
				}
				if inStr && cc == '\\' {
					esc = true
					continue
				}
				if cc == '"' {
					inStr = !inStr
					continue
				}
				if inStr {
					continue
				}
				if cc == '(' {
					depth++
				} else if cc == ')' {
					depth--
					if depth == 0 {
						break
					}
				}
			}
			if depth == 0 {
				i = j
			} else {
				// Unbalanced — leave it so the real parse error is preserved.
				b.WriteByte(c)
			}
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// repairTruncated closes a JSON value that was cut off mid-stream (for example
// when the model hit its output-token limit).
func repairTruncated(s string, dest interface{}) error {
	if err := json.Unmarshal([]byte(s), dest); err == nil {
		return nil
	}

	s = trimIncompleteToken(s)

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

	var b strings.Builder
	b.WriteString(s)
	for i := len(stack) - 1; i >= 0; i-- {
		b.WriteByte(stack[i])
	}

	return json.Unmarshal([]byte(b.String()), dest)
}

// trimIncompleteToken removes a trailing incomplete string literal, dangling
// comma, or partial key/value so closing tokens land at a clean boundary.
func trimIncompleteToken(s string) string {
	inString := false
	escaped := false
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
				// A closing quote ends a value only when it is followed by a
				// separator or a closing bracket (or EOF). A key is followed
				// by ':'; truncating there would leave a dangling "key" that
				// no amount of closing brackets can repair.
				j := i + 1
				for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\n' || s[j] == '\r') {
					j++
				}
				if j >= len(s) || s[j] == ',' || s[j] == '}' || s[j] == ']' {
					lastSafe = i
				}
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

	if inString && lastSafe >= 0 {
		return s[:lastSafe+1]
	}
	return strings.TrimRight(s, " \t\n\r,")
}
