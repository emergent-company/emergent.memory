// Package citations derives grounded object/relationship citations for a
// knowledge-base-backed chat answer from the identifiers the run's tools
// actually returned and the identifiers the answer actually referenced.
//
// It is a pure function of (tool-call outputs, answer text, A2UI surfaces):
// Candidates collects every object and relationship id a run retrieved,
// Derive keeps only those the answer referenced, and NeutralizeLinks demotes
// answer links whose ids did not validate so the client never renders a
// hyperlink to an identifier the agent hallucinated.
package citations

import (
	"regexp"
	"sort"
	"strings"

	"github.com/emergent-company/emergent.memory/pkg/a2ui"
)

// Citation is a validated reference to a graph object or relationship that an
// answer actually used. It is the wire shape shared by the live SSE `citations`
// event and the history timeline.
type Citation struct {
	Kind  string `json:"kind"` // "object" | "relationship"
	ID    string `json:"id"`
	Type  string `json:"type"` // object schema type, or relationship type
	Label string `json:"label"`
	URL   string `json:"url"` // "/objects/<id>"; relationship -> source object's page
}

// Reference is an internal candidate/reference record used during derivation.
type Reference struct {
	ID    string
	Type  string
	Label string
	Kind  string // "object" | "relationship"
	SrcID string // relationship source object id (URL resolution)
	DstID string // relationship target object id (label resolution)
}

// ToolCall is the minimal tool-call shape Candidates needs: the LLM-facing
// result map whose nested values carry graph object/relationship ids.
type ToolCall struct {
	Output map[string]any
}

// uuidPattern matches a canonical (RFC 4122) UUID string, so non-id `id` fields
// (run_id, arbitrary numeric ids, slugs) are ignored during collection.
const uuidPattern = `[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`

// relPattern matches a relationship id: a UUID in practice, or the synthesized
// "src:type:dst" form (colons allowed).
const relPattern = `[^)\s]+`

var uuidRe = regexp.MustCompile(`^` + uuidPattern + `$`)

// isUUID reports whether s is a canonical UUID string.
func isUUID(s string) bool {
	return uuidRe.MatchString(s)
}

// forEachMap invokes fn for every map reachable from v, recursing through
// nested maps and slices (both []any from JSON decoding and []map[string]any
// from literal construction). It never panics on odd shapes.
func forEachMap(v any, fn func(map[string]any)) {
	switch t := v.(type) {
	case map[string]any:
		fn(t)
		for _, e := range t {
			forEachMap(e, fn)
		}
	case []any:
		for _, e := range t {
			forEachMap(e, fn)
		}
	case []map[string]any:
		for _, e := range t {
			fn(e)
			for _, ev := range e {
				forEachMap(ev, fn)
			}
		}
	}
}

// Candidates walks every tool call's Output and collects candidate object and
// relationship references keyed by id.
//
//   - object candidate: a map with a UUID string `id` and a non-empty string
//     `type`. Label is `name` if present, else `key`, else the id.
//   - relationship candidate: a map with string `src_id` and `dst_id`. Type is
//     `type`; ID is `id` if present, else `src_id+":"+type+":"+dst_id`. Label
//     is `<src label/name> —<type>→ <dst label/name>` using object labels when
//     known, else the raw ids. URL source is `src_id`.
//
// A relationship map (src_id + dst_id) takes precedence over the object rule so
// its own `id` is never mis-read as an object id.
func Candidates(toolCalls []ToolCall) map[string]Reference {
	objects := map[string]Reference{}
	var rels []Reference

	for _, tc := range toolCalls {
		forEachMap(tc.Output, func(m map[string]any) {
			src, srcOK := m["src_id"].(string)
			dst, dstOK := m["dst_id"].(string)
			if srcOK && dstOK {
				typ, _ := m["type"].(string)
				id, _ := m["id"].(string)
				if id == "" {
					id = src + ":" + typ + ":" + dst
				}
				rels = append(rels, Reference{Kind: "relationship", ID: id, Type: typ, SrcID: src, DstID: dst})
				return
			}

			id, idOK := m["id"].(string)
			typ, typOK := m["type"].(string)
			if idOK && typOK && typ != "" && isUUID(id) {
				if _, exists := objects[id]; !exists {
					objects[id] = Reference{Kind: "object", ID: id, Type: typ, Label: objectLabel(m, id)}
				}
			}
		})
	}

	out := make(map[string]Reference, len(objects)+len(rels))
	for id, ref := range objects {
		out[id] = ref
	}
	for _, rel := range rels {
		rel.Label = relationshipLabel(rel, objects)
		if _, exists := out[rel.ID]; !exists {
			out[rel.ID] = rel
		}
	}
	return out
}

func objectLabel(m map[string]any, id string) string {
	if name, ok := m["name"].(string); ok && name != "" {
		return name
	}
	if key, ok := m["key"].(string); ok && key != "" {
		return key
	}
	return id
}

// relationshipLabel builds "<src> —<type>→ <dst>", falling back to the raw ids
// when an endpoint object's label is unknown.
func relationshipLabel(rel Reference, objects map[string]Reference) string {
	srcLabel := rel.SrcID
	if o, ok := objects[rel.SrcID]; ok && o.Label != "" {
		srcLabel = o.Label
	}
	dstLabel := rel.DstID
	if o, ok := objects[rel.DstID]; ok && o.Label != "" {
		dstLabel = o.Label
	}
	return srcLabel + " —" + rel.Type + "→ " + dstLabel
}

// posRef is a reference paired with its byte position in the answer text.
type posRef struct {
	pos int
	ref Reference
}

// Derive finds the references in answerText (markdown links `[label](/objects/<uuid>)`,
// bare `/objects/<uuid>`, and `#relationship-<rel>` fragments) and in the A2UI
// surfaces' component props, keeps only those whose id is a candidate, and
// returns object/relationship citations ordered by first appearance in the text
// (then surfaces). References are deduped by (kind, id), keeping the first label.
func Derive(answerText string, surfaces []a2ui.Message, candidates map[string]Reference) []Citation {
	var ordered []posRef
	ordered = append(ordered, textRefs(answerText)...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].pos < ordered[j].pos })

	var refs []Reference
	for _, pr := range ordered {
		refs = append(refs, pr.ref)
	}
	for _, msg := range surfaces {
		refs = append(refs, surfaceRefs(msg)...)
	}

	seen := make(map[string]bool)
	var out []Citation
	for _, r := range refs {
		key := r.Kind + ":" + r.ID
		if seen[key] {
			continue
		}
		cand, ok := candidates[r.ID]
		if !ok {
			continue // referenced but not retrieved → hallucinated, drop
		}
		seen[key] = true
		out = append(out, buildCitation(r, cand))
	}
	return out
}

// textRefs extracts object and relationship references from answerText with
// their byte positions.
func textRefs(text string) []posRef {
	var out []posRef

	objLinkRe := regexp.MustCompile(`\[([^\]]*)\]\(/objects/(` + uuidPattern + `)\)`)
	relLinkRe := regexp.MustCompile(`\[([^\]]*)\]\(/objects/(` + uuidPattern + `)#relationship-(` + relPattern + `)\)`)

	type span struct{ start, end int }
	var linkSpans []span

	for _, m := range relLinkRe.FindAllStringSubmatchIndex(text, -1) {
		label := text[m[2]:m[3]]
		src := text[m[4]:m[5]]
		rel := text[m[6]:m[7]]
		out = append(out, posRef{m[0], Reference{Kind: "relationship", ID: rel, SrcID: src, Label: label}})
		linkSpans = append(linkSpans, span{m[0], m[1]})
	}
	for _, m := range objLinkRe.FindAllStringSubmatchIndex(text, -1) {
		label := text[m[2]:m[3]]
		id := text[m[4]:m[5]]
		out = append(out, posRef{m[0], Reference{Kind: "object", ID: id, Label: label}})
		linkSpans = append(linkSpans, span{m[0], m[1]})
	}

	// Mask markdown links so the bare scan below does not double-count them.
	masked := text
	sort.Slice(linkSpans, func(i, j int) bool { return linkSpans[i].start > linkSpans[j].start })
	for _, s := range linkSpans {
		masked = masked[:s.start] + strings.Repeat(" ", s.end-s.start) + masked[s.end:]
	}

	bareRe := regexp.MustCompile(`/objects/(` + uuidPattern + `)(?:#relationship-(` + relPattern + `))?`)
	for _, m := range bareRe.FindAllStringSubmatchIndex(masked, -1) {
		id := masked[m[2]:m[3]]
		rel := ""
		if m[4] >= 0 {
			rel = masked[m[4]:m[5]]
		}
		if rel != "" {
			out = append(out, posRef{m[0], Reference{Kind: "relationship", ID: rel, SrcID: id}})
		} else {
			out = append(out, posRef{m[0], Reference{Kind: "object", ID: id}})
		}
	}

	return out
}

// surfaceRefs extracts object and relationship references from a single A2UI
// surface message's components.
func surfaceRefs(msg a2ui.Message) []Reference {
	if msg.UpdateComponents == nil {
		return nil
	}
	var refs []Reference
	for _, comp := range msg.UpdateComponents.Components {
		// Component identity may itself be the object id (entity cards link on id).
		if isUUID(comp.ID) {
			refs = append(refs, Reference{Kind: "object", ID: comp.ID})
		}
		forEachMap(comp.Props, func(m map[string]any) {
			src, srcOK := m["src_id"].(string)
			dst, dstOK := m["dst_id"].(string)
			if srcOK && dstOK {
				refs = append(refs, Reference{Kind: "relationship", ID: relIDFromProps(m, src, dst), SrcID: src})
				return
			}
			for _, k := range []string{"id", "objectId", "object_id"} {
				if v, ok := m[k].(string); ok && isUUID(v) {
					refs = append(refs, Reference{Kind: "object", ID: v})
					break
				}
			}
		})
	}
	return refs
}

func relIDFromProps(m map[string]any, src, dst string) string {
	if v, ok := m["id"].(string); ok && v != "" {
		return v
	}
	typ, _ := m["type"].(string)
	return src + ":" + typ + ":" + dst
}

func buildCitation(r Reference, cand Reference) Citation {
	label := r.Label
	if label == "" {
		label = cand.Label
	}
	url := "/objects/" + r.ID
	if r.Kind == "relationship" {
		src := r.SrcID
		if src == "" {
			src = cand.SrcID
		}
		url = "/objects/" + src
	}
	return Citation{
		Kind:  r.Kind,
		ID:    r.ID,
		Type:  cand.Type,
		Label: label,
		URL:   url,
	}
}

// NeutralizeLinks demotes `/objects/<id>` links whose id is not a cited object
// to plain text, drops `#relationship-<rel>` fragments whose rel is not a cited
// relationship, and leaves links to other paths untouched.
//
//   - markdown `[label](/objects/<id>)` not cited → `label`
//   - bare `/objects/<id>` not cited → `<id>` (plain text)
//   - a link carrying `#relationship-<rel>` with uncited `<rel>` → fragment
//     dropped, then the remaining object link follows the object rule
func NeutralizeLinks(answerText string, cited []Citation) string {
	objIDs := make(map[string]bool)
	relIDs := make(map[string]bool)
	for _, c := range cited {
		switch c.Kind {
		case "object":
			objIDs[c.ID] = true
		case "relationship":
			relIDs[c.ID] = true
		}
	}

	linkRe := regexp.MustCompile(`\[([^\]]*)\]\(/objects/(` + uuidPattern + `)(#relationship-(` + relPattern + `))?\)`)
	out := linkRe.ReplaceAllStringFunc(answerText, func(m string) string {
		sub := linkRe.FindStringSubmatch(m)
		label := sub[1]
		objID := sub[2]
		frag := sub[3]
		rel := sub[4]

		if frag != "" {
			if relIDs[rel] {
				return m // relationship cited → keep whole link
			}
			// drop the fragment, then apply the object rule to the remainder
			if objIDs[objID] {
				return "[" + label + "](/objects/" + objID + ")"
			}
			return label
		}
		if objIDs[objID] {
			return m
		}
		return label
	})

	bareRe := regexp.MustCompile(`/objects/(` + uuidPattern + `)(#relationship-(` + relPattern + `))?`)
	out = bareRe.ReplaceAllStringFunc(out, func(m string) string {
		sub := bareRe.FindStringSubmatch(m)
		objID := sub[1]
		frag := sub[2]
		rel := sub[3]

		if frag != "" {
			if relIDs[rel] {
				return m
			}
			if objIDs[objID] {
				return "/objects/" + objID
			}
			return objID
		}
		if objIDs[objID] {
			return m
		}
		return objID
	})

	return out
}
