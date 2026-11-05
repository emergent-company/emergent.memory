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

// blueprintConflictResolutionLabel is the human label for a property conflict's
// resolution. The server only reports existing_wins.
func blueprintConflictResolutionLabel(resolution string) string {
	if resolution == "" || resolution == "existing_wins" {
		return "Your project's definition wins"
	}
	return resolution
}
