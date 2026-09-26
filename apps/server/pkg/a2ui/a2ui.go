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
	"strings"
)

// DefaultVersion is the A2UI protocol version stamped on every message when the
// wire payload omits it.
const DefaultVersion = "v0.9.1"

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

// Catalog maps a known component id to the prop names that component requires.
// It is the single source of truth for which components an agent may emit.
type Catalog map[string][]string

// BasicCatalog is the built-in catalog of first-class card components. Required
// props are kept minimal; a component may carry additional props beyond these.
var BasicCatalog = Catalog{
	"proposal":    {"kind", "summary"},
	"approval":    {"tool"},
	"question":    {"prompt"},
	"code":        {"code"},
	"entity":      {"type"},
	"object-form": {"fields"},
	"todo":        {"items"},
	"result":      {"rows"},
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
	return c[id]
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
// decodes and validates. If at least one block validates, it returns the
// concatenated messages from all valid blocks, the text with the fences (and
// valid-block contents) removed, and found=true. Otherwise it returns
// nil, text, false (never an error).
func ExtractFromText(text string) ([]Message, string, bool) {
	lines := strings.Split(text, "\n")

	type block struct {
		start int   // index of the opening fence line
		end   int   // index of the closing fence line
		lines []int // indices of non-empty content lines
		valid bool
	}

	var (
		blocks   []block
		messages []Message
		inFence  bool
		cur      block
	)

	for i, line := range lines {
		if !inFence {
			if strings.TrimSpace(line) == fenceOpen {
				inFence = true
				cur = block{start: i}
			}
			continue
		}
		if strings.TrimSpace(line) == fenceClose {
			cur.end = i
			inFence = false

			cur.valid = len(cur.lines) > 0
			var curMsgs []Message
			for _, ci := range cur.lines {
				var m Message
				if err := json.Unmarshal([]byte(lines[ci]), &m); err != nil {
					cur.valid = false
					break
				}
				curMsgs = append(curMsgs, m)
			}
			if cur.valid {
				if err := Validate(curMsgs); err != nil {
					cur.valid = false
				}
			}
			if cur.valid {
				blocks = append(blocks, cur)
				messages = append(messages, curMsgs...)
			}
			continue
		}
		if strings.TrimSpace(line) != "" {
			cur.lines = append(cur.lines, i)
		}
	}

	if len(messages) == 0 {
		return nil, text, false
	}

	drop := make([]bool, len(lines))
	for _, b := range blocks {
		drop[b.start] = true
		drop[b.end] = true
		for i := b.start + 1; i < b.end; i++ {
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
	return messages, strings.Join(out, "\n"), true
}
