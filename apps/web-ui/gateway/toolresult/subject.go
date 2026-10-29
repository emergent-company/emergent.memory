package toolresult

import (
	"sort"
	"strings"
)

// subjectKeys are the argument keys that hold what a tool was asked to do, in the
// order a reader would rather see them. A tool's call is usually one string — a
// query, a name, a path — and the rest is plumbing.
var subjectKeys = []string{
	"query", "q", "question", "statement", "prompt", "command",
	"name", "title", "label", "key", "id",
	"path", "file", "filename", "url", "urls", "uri",
	"content", "text", "message", "body", "value",
}

// subjectWidth bounds the collapsed subject so one long argument cannot push the
// outcome off the row.
const subjectWidth = 80

// Subject reduces a tool call's arguments to the one line that says what it was
// asked to do. It returns "" when the arguments hold nothing readable, so the
// step omits the subject rather than printing braces or a JSON fragment.
//
// One implementation for both web renderers: the live chip and the recorded
// session row must not word the same call differently, and it is the same rule
// the category and the result presentation follow — read the payload, do not
// guess from the tool's name.
func Subject(input []byte) string {
	trimmed := strings.TrimSpace(string(input))
	if trimmed == "" || trimmed == "null" {
		return ""
	}

	value, ok := decode(trimmed)
	if !ok {
		return ""
	}

	switch v := value.(type) {
	case string:
		// Some tools take the whole argument as one string.
		return boundedSubject(v)
	case map[string]any:
		for _, key := range subjectKeys {
			if text, ok := v[key].(string); ok {
				if subject := boundedSubject(text); subject != "" {
					return subject
				}
			}
		}
		// Nothing the table names: a single readable string is still the subject,
		// but a grab-bag of several strings is not one thing to name.
		return onlyString(v)
	default:
		return ""
	}
}

// onlyString returns the record's only non-blank string value, if it has exactly
// one. Two candidates mean the arguments do not name a single thing.
func onlyString(record map[string]any) string {
	keys := make([]string, 0, len(record))
	for key := range record {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	found := ""
	for _, key := range keys {
		text, ok := record[key].(string)
		if !ok {
			continue
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		if found != "" {
			return ""
		}
		found = text
	}
	return boundedSubject(found)
}

// boundedSubject trims a subject and bounds it to one line: a multi-line argument
// (a prompt, a document body) names the call by its first line.
func boundedSubject(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if line, _, _ := strings.Cut(text, "\n"); line != text {
		text = strings.TrimSpace(line)
	}
	text = strings.Join(strings.Fields(text), " ")
	if runes := []rune(text); len(runes) > subjectWidth {
		return string(runes[:subjectWidth]) + "…"
	}
	return text
}
