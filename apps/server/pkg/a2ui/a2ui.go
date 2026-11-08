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

// ComponentSpec describes one catalog component: the prop names the prompt
// documents as required plus a one-line purpose used when the catalog is
// rendered into the system-prompt instruction. Keeping both in one place is what
// makes the prompt and the catalog membership check share a single source of
// truth.
type ComponentSpec struct {
	Required []string // prop names documented for the model (not enforced by Validate)
	Purpose  string   // one-line purpose, for prompt emission
}

// Catalog maps a known component id to its spec. It is the single source of
// truth for which components an agent may emit: the validator checks membership
// and the prompt instruction is rendered from the same map.
type Catalog map[string]ComponentSpec

// BasicCatalog is the built-in catalog of first-class card components. Required
// props are kept minimal; a component may carry additional props beyond these.
var BasicCatalog = Catalog{
	"proposal":    {Required: []string{"kind", "summary"}, Purpose: "review/accept/reject a proposed change, where the host enables it"},
	"approval":    {Required: []string{"tool"}, Purpose: "gate a tool call on the user's approval, where the host enables it"},
	"question":    {Required: []string{"prompt"}, Purpose: "collect a structured answer from the user, where the host enables it"},
	"code":        {Required: []string{"code"}, Purpose: "show a code block or diff"},
	"entity":      {Required: []string{"type"}, Purpose: "preview a graph object"},
	"object-form": {Required: []string{"fields"}, Purpose: "schema-driven data entry"},
	"todo":        {Required: []string{"items"}, Purpose: "a checkable task list"},
	"result":      {Required: []string{"rows"}, Purpose: "key/value rows or a table"},
	"sources":     {Required: []string{"items"}, Purpose: "list the sources behind an answer"},
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
			line += " (required: " + strings.Join(spec.Required, ", ") + ")"
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// Validate performs structural validation only. It checks envelope shape,
// non-empty surface identifiers, and component-catalog membership, returning a
// wrapped error naming the offending message index.
func Validate(msgs []Message) error {
	for i, m := range msgs {
		if err := validateMessage(m); err != nil {
			return fmt.Errorf("message %d: %w", i, err)
		}
	}
	return nil
}

func validateMessage(m Message) error {
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
		return fmt.Errorf("message must set exactly one of createSurface/updateComponents/updateDataModel/deleteSurface (got %d)", set)
	}

	switch {
	case m.CreateSurface != nil:
		if m.CreateSurface.SurfaceID == "" {
			return fmt.Errorf("createSurface: surfaceId must be non-empty")
		}
		if m.CreateSurface.CatalogID == "" {
			return fmt.Errorf("createSurface: catalogId must be non-empty")
		}
	case m.UpdateComponents != nil:
		if m.UpdateComponents.SurfaceID == "" {
			return fmt.Errorf("updateComponents: surfaceId must be non-empty")
		}
		for j, comp := range m.UpdateComponents.Components {
			if comp.ID == "" {
				return fmt.Errorf("updateComponents: component %d: id must be non-empty", j)
			}
			if !BasicCatalog.Contains(comp.Component) {
				return fmt.Errorf("updateComponents: component %d: unknown component %q", j, comp.Component)
			}
		}
	case m.UpdateDataModel != nil:
		if m.UpdateDataModel.SurfaceID == "" {
			return fmt.Errorf("updateDataModel: surfaceId must be non-empty")
		}
	case m.DeleteSurface != nil:
		if m.DeleteSurface.SurfaceID == "" {
			return fmt.Errorf("deleteSurface: surfaceId must be non-empty")
		}
	}
	return nil
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
	lines := strings.Split(text, "\n")

	type block struct {
		start int // index of the opening fence line
		end   int // index of the closing fence line (or the last line, if unterminated)
	}

	var (
		blocks   []block
		messages []Message
		inFence  bool
		cur      block
		curMsgs  []Message
		curValid bool
	)

	for i, line := range lines {
		if !inFence {
			if strings.TrimSpace(line) == fenceOpen {
				inFence = true
				cur = block{start: i}
				curMsgs = nil
				curValid = true
			}
			continue
		}
		if strings.TrimSpace(line) == fenceClose {
			inFence = false
			cur.end = i
			if curValid && len(curMsgs) > 0 && Validate(curMsgs) == nil {
				messages = append(messages, curMsgs...)
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
			continue
		}
		curMsgs = append(curMsgs, m)
	}
	// An unterminated fence is still a fence: the model was cut off mid-block.
	if inFence {
		cur.end = len(lines) - 1
		blocks = append(blocks, cur)
	}

	if len(blocks) == 0 {
		return nil, text, false
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
	return messages, strings.Join(out, "\n"), len(messages) > 0
}
