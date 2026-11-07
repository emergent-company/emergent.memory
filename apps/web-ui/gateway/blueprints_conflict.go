package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// BlueprintTypeConflictData carries the state for the merge review page. The
// source is generic: an install resolves by BlueprintID (hidden `id`), a GitHub
// import by URL/Ref/Token, and an update by the route's Name. Action is the
// confirm endpoint either way. ModelOverrides preserves any per-agent choices
// already made so the merge confirm applies the same models. Token is an
// optional private-repo credential; it is carried through a hidden input and is
// never placed in a redirect target or flash.
type BlueprintTypeConflictData struct {
	Action         string
	BlueprintID    string
	URL            string
	Ref            string
	Token          string
	Name           string
	ModelOverrides map[string]string
	Conflicts      []BlueprintTypeConflict

	Title       string
	Subtitle    string
	SubmitLabel string

	// CurrentVersion/LatestVersion are set for an update preview so the page can
	// name the versions being merged.
	CurrentVersion string
	LatestVersion  string
}

// blueprintConflictSummary is the one-line headline shown above the type list.
func blueprintConflictSummary(n int) string {
	if n == 1 {
		return "1 object type already exists in this project. Review the merge before continuing."
	}
	return fmt.Sprintf("%d object types already exist in this project. Review the merge before continuing.", n)
}

// blueprintConflictAddedSummary describes the added properties for one type.
func blueprintConflictAddedSummary(n int) string {
	if n == 1 {
		return "1 new property will be added"
	}
	return fmt.Sprintf("%d new properties will be added", n)
}

// blueprintConflictOverriddenSummary describes the properties whose definitions
// differ from the project's existing ones.
func blueprintConflictOverriddenSummary(n int) string {
	if n == 1 {
		return "1 property keeps your project's existing definition"
	}
	return fmt.Sprintf("%d properties keep your project's existing definitions", n)
}

// blueprintModelOverride is one sorted hidden `model.<agent>` input.
type blueprintModelOverride struct {
	Name  string
	Value string
}

// blueprintConflictOverrides returns the model overrides in stable name order so
// the rendered form is deterministic.
func blueprintConflictOverrides(m map[string]string) []blueprintModelOverride {
	if len(m) == 0 {
		return nil
	}
	out := make([]blueprintModelOverride, 0, len(m))
	for name, value := range m {
		out = append(out, blueprintModelOverride{Name: name, Value: value})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// blueprintConflictSlug turns a type name into a safe HTML id fragment.
func blueprintConflictSlug(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	if b.Len() == 0 {
		return "type"
	}
	return b.String()
}

// blueprintConflictExistingLabel names the project's existing schema for a
// conflicting type: "<pack> v<version>", falling back to whichever is present.
func blueprintConflictExistingLabel(c BlueprintTypeConflict) string {
	switch {
	case c.ExistingSchemaName != "" && c.ExistingSchemaVersion != "":
		return c.ExistingSchemaName + " v" + c.ExistingSchemaVersion
	case c.ExistingSchemaName != "":
		return c.ExistingSchemaName
	case c.ExistingSchemaVersion != "":
		return "v" + c.ExistingSchemaVersion
	default:
		return c.TypeName
	}
}

// blueprintConflictDef renders one property definition for display: indented
// JSON when it parses, else the raw text. Empty input renders an em dash.
func blueprintConflictDef(raw json.RawMessage) string {
	if len(bytes.TrimSpace(raw)) == 0 {
		return "—"
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, raw, "", "  "); err != nil {
		return string(raw)
	}
	return pretty.String()
}

// blueprintDefRow is one differing key in a property-definition diff table.
// ExistingMono/IncomingMono mark values that are rendered as compact JSON in a
// monospace cell (nested objects/arrays and long strings).
type blueprintDefRow struct {
	Key          string
	Existing     string
	Incoming     string
	Changed      bool
	ExistingMono bool
	IncomingMono bool
}

// blueprintDefInlineScalarMax is the longest string shown inline. Anything
// longer is rendered as compact JSON in a monospace cell, matching how nested
// values are shown.
const blueprintDefInlineScalarMax = 80

// blueprintConflictDefRows diffs two property definitions key by key. The shape
// is known (a JSON object of property-definition fields), so it returns one row
// per differing key in lexical key order plus the count of identical keys. The
// final bool is false when either side is not a JSON object (missing, null,
// scalar, array, or malformed): the caller then keeps the raw dump so no
// information is lost.
func blueprintConflictDefRows(existing, incoming json.RawMessage) ([]blueprintDefRow, int, bool) {
	existingObj, ok := blueprintDefObject(existing)
	if !ok {
		return nil, 0, false
	}
	incomingObj, ok := blueprintDefObject(incoming)
	if !ok {
		return nil, 0, false
	}

	keys := make([]string, 0, len(existingObj)+len(incomingObj))
	seen := make(map[string]bool, len(existingObj)+len(incomingObj))
	for _, obj := range []map[string]json.RawMessage{existingObj, incomingObj} {
		for key := range obj {
			if !seen[key] {
				seen[key] = true
				keys = append(keys, key)
			}
		}
	}
	sort.Strings(keys)

	rows := make([]blueprintDefRow, 0, len(keys))
	unchanged := 0
	for _, key := range keys {
		existingVal, existingPresent := existingObj[key]
		incomingVal, incomingPresent := incomingObj[key]
		if blueprintDefValuesEqual(existingVal, existingPresent, incomingVal, incomingPresent) {
			unchanged++
			continue
		}
		existingText, existingMono := blueprintDefValueText(existingVal, existingPresent)
		incomingText, incomingMono := blueprintDefValueText(incomingVal, incomingPresent)
		rows = append(rows, blueprintDefRow{
			Key:          key,
			Existing:     existingText,
			Incoming:     incomingText,
			Changed:      true,
			ExistingMono: existingMono,
			IncomingMono: incomingMono,
		})
	}
	return rows, unchanged, true
}

// blueprintDefObject parses a property definition object. A nil result (null,
// array, scalar) or a parse error reports false so the caller falls back.
func blueprintDefObject(raw json.RawMessage) (map[string]json.RawMessage, bool) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return nil, false
	}
	return obj, true
}

// blueprintDefValuesEqual compares two definition values semantically: key
// order and whitespace do not count as a change. Values that cannot be
// canonicalised fall back to a trimmed byte comparison.
func blueprintDefValuesEqual(a json.RawMessage, aPresent bool, b json.RawMessage, bPresent bool) bool {
	if aPresent != bPresent {
		return false
	}
	if !aPresent {
		return true
	}
	aCanon, aOK := blueprintDefCanonical(a)
	bCanon, bOK := blueprintDefCanonical(b)
	if !aOK || !bOK {
		return bytes.Equal(bytes.TrimSpace(a), bytes.TrimSpace(b))
	}
	return bytes.Equal(aCanon, bCanon)
}

// blueprintDefDecode decodes a definition value without lossy float64
// conversion: UseNumber keeps integers exact (and json.Number re-marshals
// verbatim), so a >2^53 change is neither misrendered nor treated as equal.
func blueprintDefDecode(raw json.RawMessage) (any, bool) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, false
	}
	return v, true
}

// blueprintDefCanonical returns deterministic JSON for a value (object keys
// sorted by encoding/json) so reordered keys compare equal.
func blueprintDefCanonical(raw json.RawMessage) ([]byte, bool) {
	v, ok := blueprintDefDecode(raw)
	if !ok {
		return nil, false
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, false
	}
	return b, true
}

// blueprintDefValueText renders one side of a diff row: an em dash for an
// absent key, short scalars inline, and nested objects/arrays or long strings as
// compact JSON flagged for a monospace cell. Strings are always quoted (via
// encoding/json) so the string "64" is visibly distinct from the number 64;
// numbers, booleans and null stay bare.
func blueprintDefValueText(raw json.RawMessage, present bool) (string, bool) {
	if !present {
		return "—", false
	}
	v, ok := blueprintDefDecode(raw)
	if !ok {
		return strings.TrimSpace(string(raw)), true
	}
	if s, ok := v.(string); ok {
		b, err := json.Marshal(s)
		if err != nil {
			return s, false
		}
		return string(b), len(s) > blueprintDefInlineScalarMax
	}
	b, err := json.Marshal(v)
	if err != nil {
		return strings.TrimSpace(string(raw)), true
	}
	switch v.(type) {
	case nil, bool, json.Number:
		return string(b), false
	default:
		return string(b), true
	}
}

// blueprintConflictUnchangedLabel is the one-line note shown under the diff
// table for keys whose values are identical on both sides.
func blueprintConflictUnchangedLabel(n int) string {
	if n == 1 {
		return "1 unchanged key"
	}
	return fmt.Sprintf("%d unchanged keys", n)
}

// blueprintConflictResolutionLabel is the human label for a property conflict's
// resolution. The server only reports existing_wins.
func blueprintConflictResolutionLabel(resolution string) string {
	if resolution == "" || resolution == "existing_wins" {
		return "Your project's definition wins"
	}
	return resolution
}
