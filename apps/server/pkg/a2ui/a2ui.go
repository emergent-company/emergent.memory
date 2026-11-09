// Package a2ui defines the A2UI v0.9.1 wire model for declarative agent cards
// (structured UI) plus a component catalog, structural validator, and fenced
// block extractor.
//
// Messages are a discriminated union: each envelope carries at most one of
// createSurface/updateComponents/updateDataModel/deleteSurface, mirroring the
// A2A DTO convention (pointer members + omitempty, presence not a `kind`
// discriminator).
package a2ui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// DefaultVersion is the A2UI protocol version stamped on every message when the
// wire payload omits it.
const DefaultVersion = "v0.9.1"

// CatalogID is the canonical catalog id advertised on the AgentCard and
// referenced by createSurface.catalogId in emitted surfaces.
const CatalogID = "memory-basic"

// DefaultSurfaceID is the stable surface identifier used when a block's
// messages carry no createSurface (e.g. updateComponents-only blocks). It keeps
// the progressive surface parser and the finished-response extractor on the
// same identifier.
const DefaultSurfaceID = "surface-1"

// Message is a single A2UI envelope. Exactly one of the four payload members
// must be set; there is no `kind` discriminator on the wire. Version defaults
// to DefaultVersion when absent.
type Message struct {
	Version          string            `json:"version,omitempty"`
	CreateSurface    *CreateSurface    `json:"createSurface,omitempty"`
	UpdateComponents *UpdateComponents `json:"updateComponents,omitempty"`
	UpdateDataModel  *UpdateDataModel  `json:"updateDataModel,omitempty"`
	DeleteSurface    *DeleteSurface    `json:"deleteSurface,omitempty"`
}

// CreateSurface opens a new UI surface bound to a catalog.
type CreateSurface struct {
	SurfaceID string `json:"surfaceId"`
	CatalogID string `json:"catalogId"`
}

// UpdateComponents replaces the component tree of an existing surface.
type UpdateComponents struct {
	SurfaceID  string      `json:"surfaceId"`
	Components []Component `json:"components"`
}

// UpdateDataModel patches a value into a surface's data model at the given path.
type UpdateDataModel struct {
	SurfaceID string `json:"surfaceId"`
	Path      string `json:"path,omitempty"`
	Value     any    `json:"value,omitempty"`
}

// DeleteSurface tears down an existing surface.
type DeleteSurface struct {
	SurfaceID string `json:"surfaceId"`
}

// Component is one card instance. `id` and `component` are the required
// identity fields; every remaining key is preserved as a flat prop in Props so
// the catalog stays open to future card shapes.
type Component struct {
	ID        string         `json:"id"`
	Component string         `json:"component"`
	Props     map[string]any `json:"-"`
}

// UnmarshalJSON reads `id` and `component` as fields and preserves every other
// key as a flat prop.
func (c *Component) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	c.Props = make(map[string]any)
	for k, v := range raw {
		switch k {
		case "id":
			if s, ok := v.(string); ok {
				c.ID = s
			}
		case "component":
			if s, ok := v.(string); ok {
				c.Component = s
			}
		default:
			c.Props[k] = v
		}
	}
	return nil
}

// MarshalJSON flattens Props back into the top-level component object.
func (c Component) MarshalJSON() ([]byte, error) {
	out := make(map[string]any, len(c.Props)+2)
	out["id"] = c.ID
	out["component"] = c.Component
	for k, v := range c.Props {
		out[k] = v
	}
	return json.Marshal(out)
}

// UnmarshalJSON decodes the envelope and defaults Version to DefaultVersion.
func (m *Message) UnmarshalJSON(data []byte) error {
	var aux struct {
		Version          *string           `json:"version"`
		CreateSurface    *CreateSurface    `json:"createSurface,omitempty"`
		UpdateComponents *UpdateComponents `json:"updateComponents,omitempty"`
		UpdateDataModel  *UpdateDataModel  `json:"updateDataModel,omitempty"`
		DeleteSurface    *DeleteSurface    `json:"deleteSurface,omitempty"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	m.CreateSurface = aux.CreateSurface
	m.UpdateComponents = aux.UpdateComponents
	m.UpdateDataModel = aux.UpdateDataModel
	m.DeleteSurface = aux.DeleteSurface
	if aux.Version != nil && *aux.Version != "" {
		m.Version = *aux.Version
	} else {
		m.Version = DefaultVersion
	}
	return nil
}

// PropType is the Go-native type of a catalog prop value. The catalog stays
// JSON-Schema-free by design (agent-structured-ui design doc): these constants
// are the minimum needed to reject a value a renderer would otherwise have to
// coerce or degrade.
type PropType string

const (
	PropString     PropType = "string"
	PropBool       PropType = "bool"
	PropNumber     PropType = "number"
	PropObject     PropType = "object"
	PropEnum       PropType = "enum"
	PropStringList PropType = "[]string"
	PropObjectList PropType = "[]object"
	PropAnyList    PropType = "[]any"
)

// Host limits. A catalog prop may override MaxLen/MaxItems; these are the
// defaults (and the structural caps that keep one card from becoming a payload).
const (
	// MaxComponentsPerMessage caps one updateComponents envelope.
	MaxComponentsPerMessage = 50
	// MaxListItems caps rows/items/fields/options when a prop sets no own limit.
	MaxListItems = 200
	// MaxStringLength is the default string cap for a declared prop.
	MaxStringLength = 4000
	// MaxCodeLength is the cap for the `code` component's code prop.
	MaxCodeLength = 20000
)

// PropSpec declares one catalog prop: its type, an optional enum domain, and
// optional per-prop limits (0 means the package default).
type PropSpec struct {
	Type     PropType
	Enum     []string // when Type == PropEnum
	MaxItems int      // when the type is a list
	MaxLen   int      // when Type == PropString
}

// ComponentSpec describes one catalog component: the props the validator
// enforces (type + requiredness) plus a one-line purpose used when the catalog
// is rendered into the system-prompt instruction. Keeping both in one place is
// what makes the prompt and the validator share a single source of truth.
type ComponentSpec struct {
	Props    map[string]PropSpec // declared props and their types; undeclared props stay open
	Required []string            // prop names a valid component must carry
	Purpose  string              // one-line purpose, for prompt emission
}

// Catalog maps a known component id to its spec. It is the single source of
// truth for which components an agent may emit: the validator checks membership
// and the prompt instruction is rendered from the same map.
type Catalog map[string]ComponentSpec

// BasicCatalog is the built-in catalog of first-class card components. A
// component may carry additional props beyond these; only declared props are
// type-checked, and only Required props are mandatory.
var BasicCatalog = Catalog{
	"proposal": {
		Required: []string{"kind", "summary"},
		Props: map[string]PropSpec{
			"kind":    {Type: PropString, MaxLen: 64},
			"summary": {Type: PropString, MaxLen: 2000},
			"body":    {Type: PropObject},
		},
		Purpose: "review/accept/reject a proposed change, where the host enables it",
	},
	"approval": {
		Required: []string{"tool"},
		Props: map[string]PropSpec{
			"tool":  {Type: PropString, MaxLen: 128},
			"input": {Type: PropObject},
		},
		Purpose: "gate a tool call on the user's approval, where the host enables it",
	},
	"question": {
		Required: []string{"prompt"},
		Props: map[string]PropSpec{
			"prompt":          {Type: PropString, MaxLen: 4000},
			"interactionType": {Type: PropEnum, Enum: []string{"text", "choice", "multi", "bool"}},
			"options":         {Type: PropAnyList, MaxItems: 50},
		},
		Purpose: "collect a structured answer from the user, where the host enables it",
	},
	"code": {
		Required: []string{"code"},
		Props: map[string]PropSpec{
			"code": {Type: PropString, MaxLen: MaxCodeLength},
			"lang": {Type: PropString, MaxLen: 64},
		},
		Purpose: "show a code block or diff",
	},
	"entity": {
		Required: []string{"type"},
		Props: map[string]PropSpec{
			"type":          {Type: PropString, MaxLen: 128},
			"label":         {Type: PropString, MaxLen: 256},
			"properties":    {Type: PropObject},
			"relationships": {Type: PropAnyList, MaxItems: MaxListItems},
		},
		Purpose: "preview a graph object",
	},
	"object-form": {
		Required: []string{"fields"},
		Props: map[string]PropSpec{
			"fields": {Type: PropObjectList, MaxItems: 100},
		},
		Purpose: "schema-driven data entry",
	},
	"todo": {
		Required: []string{"items"},
		Props: map[string]PropSpec{
			"items": {Type: PropAnyList, MaxItems: MaxListItems},
		},
		Purpose: "a checkable task list",
	},
	"result": {
		Required: []string{"rows"},
		Props: map[string]PropSpec{
			"rows": {Type: PropObjectList, MaxItems: MaxListItems},
		},
		Purpose: "key/value rows or a table",
	},
	"sources": {
		Required: []string{"items"},
		Props: map[string]PropSpec{
			"items": {Type: PropObjectList, MaxItems: MaxListItems},
		},
		Purpose: "list the sources behind an answer",
	},
}

// Contains reports whether id is a known component in the catalog.
func (c Catalog) Contains(id string) bool {
	_, ok := c[id]
	return ok
}

// IDs returns the component ids registered in the catalog, unordered.
func (c Catalog) IDs() []string {
	ids := make([]string, 0, len(c))
	for id := range c {
		ids = append(ids, id)
	}
	return ids
}

// RequiredProps returns the required prop names for a component id, or nil if
// the id is not in the catalog.
func (c Catalog) RequiredProps(id string) []string {
	return c[id].Required
}

// InstructionPrefix opens the A2UI prompt instruction. It doubles as the
// no-duplication marker: the executor skips appending the instruction when the
// resolved prompt already carries it.
const InstructionPrefix = "Structured UI cards (A2UI):"

// Instruction renders the system-prompt instruction that teaches a model how to
// emit A2UI cards. The component list is rendered from the catalog — the same
// map the validator reads — so the prompt and the validator cannot drift. The
// fixed prose describes the fenced envelope and when a card beats plain prose.
func (c Catalog) Instruction() string {
	var b strings.Builder
	b.WriteString(InstructionPrefix)
	b.WriteString(" you may answer with a structured card instead of plain prose. ")
	b.WriteString("To emit one, write a fenced ")
	b.WriteString(fenceOpen)
	b.WriteString(" code block (closed by a line of three backticks) containing one JSON message per line (JSONL). ")
	b.WriteString("Open a surface first with {\"createSurface\":{\"surfaceId\":\"s1\",\"catalogId\":\"")
	b.WriteString(CatalogID)
	b.WriteString("\"}}, then add components with {\"updateComponents\":{\"surfaceId\":\"s1\",\"components\":[{\"id\":\"root\",\"component\":\"result\",\"rows\":[]}]}}. ")
	b.WriteString("You may also patch a surface's data model with {\"updateDataModel\":{\"surfaceId\":\"s1\",\"path\":\"/x\",\"value\":0}} or tear a surface down with {\"deleteSurface\":{\"surfaceId\":\"s1\"}}. ")
	b.WriteString("Use a card only when the content is genuinely structured — a decision the user must make, a code block or diff, a task list, a table of results, a form, a graph-object preview, or a list of sources. ")
	b.WriteString("Otherwise answer in prose; do not wrap ordinary prose in a card.")
	if catalog := c.renderCatalog(); catalog != "" {
		b.WriteString("\n\nComponents:\n")
		b.WriteString(catalog)
	}
	return b.String()
}

// renderCatalog lists every component as one line — id, purpose, and required
// props — sorted by id for deterministic output.
func (c Catalog) renderCatalog() string {
	ids := c.IDs()
	sort.Strings(ids)
	lines := make([]string, 0, len(ids))
	for _, id := range ids {
		spec := c[id]
		line := "- " + id + ": " + spec.Purpose
		if len(spec.Required) > 0 {
			required := make([]string, 0, len(spec.Required))
			requiredSet := make(map[string]bool, len(spec.Required))
			for _, name := range spec.Required {
				requiredSet[name] = true
				prop := spec.Props[name]
				entry := name + " " + string(prop.Type)
				if prop.Type == PropEnum && len(prop.Enum) > 0 {
					entry = name + " (" + strings.Join(prop.Enum, "|") + ")"
				}
				required = append(required, entry)
			}
			line += " (required: " + strings.Join(required, ", ") + ")"
			// Optional declared props teach their type too, so the model does not
			// guess a shape the validator will reject.
			var optional []string
			for name, prop := range spec.Props {
				if requiredSet[name] {
					continue
				}
				entry := name + " " + string(prop.Type)
				if prop.Type == PropEnum && len(prop.Enum) > 0 {
					entry = name + " (" + strings.Join(prop.Enum, "|") + ")"
				}
				optional = append(optional, entry)
			}
			sort.Strings(optional)
			if len(optional) > 0 {
				line += ", optional: " + strings.Join(optional, ", ")
			}
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// Issue is one validation problem, addressed to the component and prop the
// model must repair. Its Error form is exactly the text a repair turn can carry
// back to the model (issue #416/#417).
type Issue struct {
	Message   int    // index of the offending message in the validated slice
	Component string // component id, for a component-scoped issue
	Kind      string // catalog component id (e.g. "proposal")
	Prop      string // offending prop, when the issue is about one
	Detail    string // human/model-readable problem
}

func (i Issue) Error() string {
	if i.Component != "" {
		s := fmt.Sprintf("message %d: component %q", i.Message, i.Component)
		if i.Kind != "" {
			s += " (" + i.Kind + ")"
		}
		return s + ": " + i.Detail
	}
	return fmt.Sprintf("message %d: %s", i.Message, i.Detail)
}

// Issues is an ordered list of validation problems. It implements error so a
// caller that only needs validity can use Validate; the repair pass consumes the
// individual issues.
type Issues []Issue

func (is Issues) Error() string {
	parts := make([]string, len(is))
	for i, issue := range is {
		parts[i] = issue.Error()
	}
	return strings.Join(parts, "; ")
}

// Err returns nil for an empty list, otherwise the list as an error.
func (is Issues) Err() error {
	if len(is) == 0 {
		return nil
	}
	return is
}

// Validate reports whether msgs form a valid A2UI message set, returning the
// first-block error text when they do not. It validates shape, catalog
// membership, required props and prop types (see ValidateAll for every issue).
func Validate(msgs []Message) error {
	return ValidateAll(msgs).Err()
}

// ValidateAll validates every message and returns every issue found, not just
// the first: the issues are what a single repair turn sends back to the model.
// Validation is structural: it never fails a run.
func ValidateAll(msgs []Message) Issues {
	var out Issues
	surfaceSeen := ""
	reportedMix := make(map[string]bool)
	for i, m := range msgs {
		issues, surfaceID := validateMessage(i, m)
		out = append(out, issues...)
		if surfaceID != "" {
			if surfaceSeen == "" {
				surfaceSeen = surfaceID
			} else if surfaceSeen != surfaceID {
				// Report the mixed-surface problem once per pair of ids, not once
				// per offending message — a long block would otherwise repeat the
				// same repair instruction.
				key := surfaceSeen + "\x00" + surfaceID
				if !reportedMix[key] {
					reportedMix[key] = true
					out = append(out, Issue{
						Message: i,
						Detail:  fmt.Sprintf("a message set may describe one surface only (saw %q and %q)", surfaceSeen, surfaceID),
					})
				}
			}
		}
	}
	return out
}

// validateMessage validates one envelope and returns its issues plus the
// surface id it operates on ("" when the message carries no surface).
func validateMessage(idx int, m Message) (Issues, string) {
	set := 0
	for _, present := range []bool{
		m.CreateSurface != nil,
		m.UpdateComponents != nil,
		m.UpdateDataModel != nil,
		m.DeleteSurface != nil,
	} {
		if present {
			set++
		}
	}
	if set != 1 {
		return Issues{{Message: idx, Detail: fmt.Sprintf(
			"message must set exactly one of createSurface/updateComponents/updateDataModel/deleteSurface (got %d)", set)}}, ""
	}

	switch {
	case m.CreateSurface != nil:
		if m.CreateSurface.SurfaceID == "" {
			return Issues{{Message: idx, Detail: "createSurface: surfaceId must be non-empty"}}, ""
		}
		if m.CreateSurface.CatalogID == "" {
			return Issues{{Message: idx, Detail: "createSurface: catalogId must be non-empty"}}, m.CreateSurface.SurfaceID
		}
		return nil, m.CreateSurface.SurfaceID
	case m.UpdateComponents != nil:
		if m.UpdateComponents.SurfaceID == "" {
			return Issues{{Message: idx, Detail: "updateComponents: surfaceId must be non-empty"}}, ""
		}
		var issues Issues
		if n := len(m.UpdateComponents.Components); n > MaxComponentsPerMessage {
			issues = append(issues, Issue{
				Message: idx,
				Detail:  fmt.Sprintf("updateComponents: %d components exceeds the host limit of %d", n, MaxComponentsPerMessage),
			})
		}
		seen := make(map[string]bool, len(m.UpdateComponents.Components))
		for j, comp := range m.UpdateComponents.Components {
			if comp.ID == "" {
				issues = append(issues, Issue{
					Message: idx,
					Detail:  fmt.Sprintf("updateComponents: component %d: id must be non-empty", j),
				})
				continue
			}
			if seen[comp.ID] {
				issues = append(issues, Issue{
					Message:   idx,
					Component: comp.ID,
					Kind:      comp.Component,
					Detail:    "duplicate component id in one updateComponents message",
				})
				continue
			}
			seen[comp.ID] = true
			if !BasicCatalog.Contains(comp.Component) {
				issues = append(issues, Issue{
					Message:   idx,
					Component: comp.ID,
					Detail:    fmt.Sprintf("unknown component %q", comp.Component),
				})
				continue
			}
			issues = append(issues, validateComponentProps(idx, comp, BasicCatalog[comp.Component])...)
		}
		return issues, m.UpdateComponents.SurfaceID
	case m.UpdateDataModel != nil:
		if m.UpdateDataModel.SurfaceID == "" {
			return Issues{{Message: idx, Detail: "updateDataModel: surfaceId must be non-empty"}}, ""
		}
		return nil, m.UpdateDataModel.SurfaceID
	case m.DeleteSurface != nil:
		if m.DeleteSurface.SurfaceID == "" {
			return Issues{{Message: idx, Detail: "deleteSurface: surfaceId must be non-empty"}}, ""
		}
		return nil, m.DeleteSurface.SurfaceID
	}
	return nil, ""
}

// validateComponentProps enforces a component's required props, declared prop
// types and host limits. Undeclared props stay open (the catalog is forward-
// compatible); declared ones must be the shape the renderers actually consume.
func validateComponentProps(idx int, comp Component, spec ComponentSpec) Issues {
	var issues Issues
	for _, name := range spec.Required {
		if _, ok := comp.Props[name]; !ok {
			issues = append(issues, Issue{
				Message:   idx,
				Component: comp.ID,
				Kind:      comp.Component,
				Prop:      name,
				Detail:    fmt.Sprintf("missing required prop %q", name),
			})
		}
	}
	// Deterministic order: validate declared props in catalog order by name.
	declared := make([]string, 0, len(spec.Props))
	for name := range spec.Props {
		declared = append(declared, name)
	}
	sort.Strings(declared)
	for _, name := range declared {
		value, ok := comp.Props[name]
		if !ok {
			continue
		}
		if detail := checkPropValue(value, spec.Props[name]); detail != "" {
			issues = append(issues, Issue{
				Message:   idx,
				Component: comp.ID,
				Kind:      comp.Component,
				Prop:      name,
				Detail:    detail,
			})
		}
	}
	return issues
}

// checkPropValue returns "" when value satisfies spec, or a model-readable
// problem naming the expected shape and what arrived.
func checkPropValue(value any, spec PropSpec) string {
	switch spec.Type {
	case PropString:
		s, ok := value.(string)
		if !ok {
			return fmt.Sprintf("expected string, got %s", jsonTypeName(value))
		}
		if s == "" {
			return "must be a non-empty string"
		}
		limit := spec.MaxLen
		if limit <= 0 {
			limit = MaxStringLength
		}
		if len(s) > limit {
			return fmt.Sprintf("string is %d bytes, exceeds the host limit of %d", len(s), limit)
		}
	case PropBool:
		if _, ok := value.(bool); !ok {
			return fmt.Sprintf("expected bool, got %s", jsonTypeName(value))
		}
	case PropNumber:
		if !isJSONNumber(value) {
			return fmt.Sprintf("expected number, got %s", jsonTypeName(value))
		}
	case PropObject:
		if _, ok := value.(map[string]any); !ok {
			return fmt.Sprintf("expected object, got %s", jsonTypeName(value))
		}
	case PropEnum:
		s, ok := value.(string)
		if !ok {
			return fmt.Sprintf("expected string, got %s", jsonTypeName(value))
		}
		for _, allowed := range spec.Enum {
			if s == allowed {
				return ""
			}
		}
		return fmt.Sprintf("expected one of %s, got %q", strings.Join(spec.Enum, ", "), s)
	case PropStringList:
		items, ok := asList(value)
		if !ok {
			return fmt.Sprintf("expected array of strings, got %s", jsonTypeName(value))
		}
		if detail := checkListLimit(len(items), spec); detail != "" {
			return detail
		}
		for i, item := range items {
			if _, ok := item.(string); !ok {
				return fmt.Sprintf("item %d: expected string, got %s", i, jsonTypeName(item))
			}
		}
	case PropObjectList:
		items, ok := asList(value)
		if !ok {
			return fmt.Sprintf("expected array of objects, got %s", jsonTypeName(value))
		}
		if detail := checkListLimit(len(items), spec); detail != "" {
			return detail
		}
		for i, item := range items {
			if _, ok := item.(map[string]any); !ok {
				return fmt.Sprintf("item %d: expected object, got %s", i, jsonTypeName(item))
			}
		}
	case PropAnyList:
		items, ok := asList(value)
		if !ok {
			return fmt.Sprintf("expected array, got %s", jsonTypeName(value))
		}
		return checkListLimit(len(items), spec)
	default:
		return ""
	}
	return ""
}

func checkListLimit(n int, spec PropSpec) string {
	limit := spec.MaxItems
	if limit <= 0 {
		limit = MaxListItems
	}
	if n > limit {
		return fmt.Sprintf("has %d items, exceeds the host limit of %d", n, limit)
	}
	return ""
}

func asList(value any) ([]any, bool) {
	if items, ok := value.([]any); ok {
		return items, true
	}
	// Tests and in-process callers may build typed slices; normalise them to the
	// []any shape UnmarshalJSON produces.
	switch typed := value.(type) {
	case []string:
		out := make([]any, len(typed))
		for i, v := range typed {
			out[i] = v
		}
		return out, true
	case []map[string]any:
		out := make([]any, len(typed))
		for i, v := range typed {
			out[i] = v
		}
		return out, true
	}
	return nil, false
}

func isJSONNumber(value any) bool {
	switch value.(type) {
	case float64, float32, int, int32, int64, json.Number:
		return true
	}
	return false
}

// jsonTypeName names the JSON type of a decoded value for an error message.
func jsonTypeName(value any) string {
	switch value.(type) {
	case nil:
		return "null"
	case string:
		return "string"
	case bool:
		return "bool"
	case float64, float32, int, int32, int64, json.Number:
		return "number"
	case []any, []string, []map[string]any:
		return "array"
	case map[string]any:
		return "object"
	default:
		return fmt.Sprintf("%T", value)
	}
}

const (
	fenceOpen  = "```a2ui"
	fenceClose = "```"
)

// ExtractFromText scans text for ```a2ui fenced blocks. Each non-empty line
// inside a block is one JSON message; a block is valid only if every message
// decodes and validates.
//
// Every recognised block is removed from the returned text — valid or not.
// The protocol is machine-facing: a block that fails to validate is still a
// fenced a2ui block, and leaving it in place would show the user raw JSONL.
// An unterminated block (the model was cut off mid-surface) counts as a block
// and is removed too.
//
// It returns the messages from all valid blocks (in order), the text with every
// a2ui block removed, and found=true only when at least one block validated.
// It never returns an error: a bad surface degrades to plain text.
func ExtractFromText(text string) ([]Message, string, bool) {
	messages, rest, _, _ := ExtractWithIssues(text)
	return messages, rest, len(messages) > 0
}

// ExtractWithIssues is ExtractFromText plus the validation issues of every
// block that failed. It is what makes the single bounded repair pass possible:
// a model that emitted an invalid surface gets the exact issues back instead of
// a silent degradation. hasBlock is true for any recognised fence, valid or
// not, so a caller can tell "no surface" from "the surface failed".
func ExtractWithIssues(text string) (messages []Message, rest string, hasBlock bool, issues Issues) {
	lines := strings.Split(text, "\n")

	type block struct {
		start int // index of the opening fence line
		end   int // index of the closing fence line (or the last line, if unterminated)
	}

	var (
		blocks    []block
		inFence   bool
		cur       block
		curMsgs   []Message
		curIssues Issues
		curValid  bool
	)

	for i, line := range lines {
		if !inFence {
			if strings.TrimSpace(line) == fenceOpen {
				inFence = true
				cur = block{start: i}
				curMsgs = nil
				curIssues = nil
				curValid = true
			}
			continue
		}
		if strings.TrimSpace(line) == fenceClose {
			inFence = false
			cur.end = i
			switch {
			case !curValid:
				issues = append(issues, curIssues...)
				if len(curIssues) == 0 {
					issues = append(issues, Issue{Detail: "A2UI block did not parse"})
				}
			case len(curMsgs) == 0:
				issues = append(issues, Issue{Detail: "A2UI block contained no messages"})
			default:
				if validated := ValidateAll(curMsgs); len(validated) > 0 {
					issues = append(issues, validated...)
				} else {
					messages = append(messages, curMsgs...)
				}
			}
			blocks = append(blocks, cur)
			continue
		}
		if strings.TrimSpace(line) == "" || !curValid {
			continue
		}
		var m Message
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			curValid = false
			curIssues = append(curIssues, Issue{
				Message: len(curMsgs),
				Detail:  fmt.Sprintf("invalid JSON: %v", err),
			})
			continue
		}
		curMsgs = append(curMsgs, m)
	}
	// An unterminated fence is still a fence: the model was cut off mid-block.
	if inFence {
		cur.end = len(lines) - 1
		blocks = append(blocks, cur)
		issues = append(issues, Issue{
			Message: len(curMsgs),
			Detail:  "unterminated A2UI block (the response was cut off)",
		})
	}

	if len(blocks) == 0 {
		return nil, text, false, nil
	}

	drop := make([]bool, len(lines))
	for _, b := range blocks {
		for i := b.start; i <= b.end && i < len(lines); i++ {
			drop[i] = true
		}
	}

	out := make([]string, 0, len(lines))
	for i, line := range lines {
		if drop[i] {
			continue
		}
		out = append(out, line)
	}
	return messages, strings.Join(out, "\n"), true, issues
}

// SurfaceIDOf returns the first createSurface surfaceId in msgs, or
// DefaultSurfaceID when none is present. It is the single derivation rule shared
// by the finished-response extractor and the progressive surface stream so both
// agree on a block's identifier.
func SurfaceIDOf(msgs []Message) string {
	for _, m := range msgs {
		if m.CreateSurface != nil && m.CreateSurface.SurfaceID != "" {
			return m.CreateSurface.SurfaceID
		}
	}
	return DefaultSurfaceID
}

// SummaryText renders a short, deterministic plain-text description of the
// cards in msgs. It exists for replay paths that carry text only (ACP/MCP
// call_agent, a `kb.chat_messages` row) so a card-only answer is not reported as
// an empty reply. It never echoes the raw protocol JSONL and never dumps a code
// prop: at most a short human title is quoted.
func SummaryText(msgs []Message) string {
	var kinds []string
	var title string
	for _, m := range msgs {
		if m.UpdateComponents == nil {
			continue
		}
		for _, comp := range m.UpdateComponents.Components {
			kinds = append(kinds, comp.Component)
			if title == "" {
				title = cardTitle(comp)
			}
		}
	}
	if len(kinds) == 0 {
		return "Rendered a structured card."
	}
	if len(kinds) == 1 {
		if title != "" {
			return fmt.Sprintf("Rendered a structured card (%s): %s", kinds[0], title)
		}
		return fmt.Sprintf("Rendered a structured card (%s).", kinds[0])
	}
	return fmt.Sprintf("Rendered %d structured cards (%s).", len(kinds), strings.Join(kinds, ", "))
}

// cardTitle returns a short human title for a component, if it carries one.
// Only title-like string props are considered; a `code` body is never echoed.
func cardTitle(comp Component) string {
	for _, name := range []string{"summary", "prompt", "label", "title"} {
		if s, ok := comp.Props[name].(string); ok && strings.TrimSpace(s) != "" {
			return truncateRunes(strings.TrimSpace(s), 160)
		}
	}
	return ""
}

func truncateRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}
