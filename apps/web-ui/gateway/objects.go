package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
	"golang.org/x/sync/errgroup"
)

// --- objects browser ---

// uiObjects renders the objects browser: graph objects, filterable by type and
// by branch.
func (s *Server) uiObjects(c echo.Context) error {
	ctx := c.Request().Context()
	branchID := c.QueryParam("branch")
	typeFilter := c.QueryParam("type")
	var (
		all         []GraphObject
		objectsErr  error
		branches    []Branch
		branchesErr error
		compiled    *CompiledSchemaTypes
		compiledErr error
	)
	var g errgroup.Group
	g.Go(func() error { all, objectsErr = s.memory.ListGraphObjects(ctx, branchID, "", nil); return nil })
	g.Go(func() error { branches, branchesErr = s.memory.ListBranches(ctx); return nil })
	g.Go(func() error { compiled, compiledErr = s.memory.GetCompiledTypes(ctx); return nil })
	_ = g.Wait()
	if objectsErr != nil {
		return s.page(c, pageTitle("Objects"), ObjectsPage(nil, nil, typeFilter, nil, branchID, nil, objectsErr))
	}
	captureError(branchesErr)
	captureError(compiledErr)
	objects := all
	if typeFilter != "" {
		objects = nil
		for _, o := range all {
			if o.Type == typeFilter {
				objects = append(objects, o)
			}
		}
	}
	var typeUIByType map[string]typeUI
	if compiled != nil {
		typeUIByType = objectTypeUIMap(compiled.ObjectTypes)
	}
	return s.page(c, pageTitle("Objects"), ObjectsPage(objects, distinctObjectTypes(all), typeFilter, branches, branchID, typeUIByType, nil))
}

// uiObject renders one object's detail: properties + relationships.
func (s *Server) uiObject(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	flashErr := flashError(c)

	obj, err := s.memory.GetGraphObject(ctx, id)
	if err != nil {
		return s.page(c, pageTitle("Object"), ObjectDetailPage(nil, nil, nil, nil, nil, err, "", flashErr, nil, nil, nil))
	}
	labelSuggestions := s.objectLabelSuggestions(ctx)

	compiled, cerr := s.memory.GetCompiledTypes(ctx)
	if cerr != nil {
		captureError(cerr)
	}
	var relTypes []CompiledType
	var propDefs []objectPropertyDef
	var typeUIByType map[string]typeUI
	if compiled != nil {
		relTypes = compiled.RelationshipTypes
		propDefs = compiledTypePropertyDefs(compiled.ObjectTypes, obj.Type)
		typeUIByType = objectTypeUIMap(compiled.ObjectTypes)
	}

	edges, err := s.memory.GetObjectEdges(ctx, id)
	captureError(err)
	related := s.loadRelatedObjects(ctx, obj, edges)
	similar, err := s.memory.GetSimilarObjects(ctx, id, 10)
	captureError(err)

	flashMsg := ""
	if c.QueryParam("updated") != "" {
		flashMsg = "Object updated."
	}
	return s.page(c, pageTitle(graphObjectLabel(*obj)), ObjectDetailPage(obj, relTypes, edges, related, filterSimilarMatches(similar), nil, flashMsg, flashErr, labelSuggestions, propDefs, typeUIByType))
}

// uiObjectUpdate applies an object edit from the detail form (key, status,
// labels, prop_* properties) as a delta merge and redirects to the updated
// object — the graph store is versioned, so memory returns a NEW id for the
// updated version and the redirect target is that id, not the submitted one.
// ?updated=1 carries PRG feedback so the detail page shows a success toast;
// failures redirect with ?err= so the real reason is surfaced instead of
// silently discarding the save.
func (s *Server) uiObjectUpdate(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	if err := c.Request().ParseForm(); err != nil {
		return redirectWithError(c, "/objects/"+url.PathEscape(id), err)
	}

	req := &UpdateObjectRequest{ReplaceLabels: true}
	if key := c.FormValue("key"); key != "" {
		req.Key = &key
	}
	if status := c.FormValue("status"); status != "" {
		req.Status = &status
	}
	req.Labels = indexedFormValues(c.Request().PostForm, "labels")

	props := map[string]any{}
	for field, vals := range c.Request().PostForm {
		name, ok := strings.CutPrefix(field, "prop_")
		if !ok || name == "" {
			continue
		}
		for _, v := range vals {
			if v == "" {
				continue
			}
			var parsed any
			if json.Unmarshal([]byte(v), &parsed) == nil {
				props[name] = parsed
			} else {
				props[name] = v
			}
		}
	}
	if len(props) > 0 {
		req.Properties = props
	}

	updated, err := s.memory.UpdateObject(ctx, id, req)
	if err != nil {
		return redirectWithError(c, "/objects/"+url.PathEscape(id), err)
	}
	// The graph store is versioned: PATCH returns a new object id for the
	// updated version. Redirect to it (memory resolves either the version or
	// canonical id, so stale links still land on the head version) and carry
	// ?updated=1 so the detail page flashes a success toast.
	return c.Redirect(http.StatusSeeOther, "/objects/"+url.PathEscape(updated.ID)+"?updated=1")
}

// uiObjectRelationshipCreate creates a relationship from the object detail
// modal (type + src/dst) and redirects back to the object page.
func (s *Server) uiObjectRelationshipCreate(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	relType := c.FormValue("type")
	srcID := c.FormValue("src_id")
	dstID := c.FormValue("dst_id")
	if relType == "" || srcID == "" || dstID == "" {
		return c.Redirect(http.StatusSeeOther, "/objects/"+url.PathEscape(id))
	}
	err := s.memory.CreateRelationship(ctx, &CreateRelationshipRequest{Type: relType, SrcID: srcID, DstID: dstID})
	if err != nil {
		captureError(err)
		return redirectWithError(c, "/objects/"+url.PathEscape(id), err)
	}
	return c.Redirect(http.StatusSeeOther, "/objects/"+url.PathEscape(id))
}

// uiObjectSearch is the JSON autocomplete endpoint behind the "add
// relationship" modal: it full-text-searches objects and returns their
// id / display label / type for the src/dst pickers.
func (s *Server) uiObjectSearch(c echo.Context) error {
	ctx := c.Request().Context()
	q := c.QueryParam("q")
	if q == "" {
		return c.JSON(http.StatusOK, []any{})
	}
	objects, err := s.memory.SearchObjectsFTS(ctx, q, c.QueryParam("type"))
	if err != nil {
		captureError(err)
		return c.JSON(http.StatusOK, []any{})
	}
	type objectSearchResult struct {
		ID    string `json:"id"`
		Label string `json:"label"`
		Type  string `json:"type"`
	}
	out := make([]objectSearchResult, 0, len(objects))
	for _, o := range objects {
		out = append(out, objectSearchResult{ID: o.ID, Label: graphObjectLabel(o), Type: o.Type})
	}
	return c.JSON(http.StatusOK, out)
}

// objectCreateForm carries create-form state for rendering and re-render on error.
type objectCreateForm struct {
	Type   string
	Key    string
	Status string
	Labels []string
	Props  map[string][]string // prop name -> raw submitted values (indexed for multi-value TagLists)
	Error  string
}

// knownObjectType reports whether name is a compiled object type.
func knownObjectType(objectTypes []CompiledType, name string) bool {
	for _, t := range objectTypes {
		if t.Name == name {
			return true
		}
	}
	return false
}

// objectTypeDisplayName renders a compiled object type's option label,
// falling back to its raw name when no human label is configured.
func objectTypeDisplayName(t CompiledType) string {
	if t.Label != "" {
		return t.Label
	}
	return t.Name
}

// objectPropertyDef is one schema property of an object type, with the schema
// metadata needed to render a type-appropriate input. Type drives the default
// widget; Widget is an explicit schema hint ("textarea" for free-form long
// text, "input" to force a single line) that overrides the default only for
// string-ish properties; Enum, when declared, renders a native select of the
// allowed values. The schema carries these losslessly from the pack through
// the compiled-types view.
type objectPropertyDef struct {
	Name   string
	Type   string
	Widget string
	Enum   []string
}

// indexedFormValues collects base[0], base[1], … from the submitted form
// values in ascending index order — the submission format of form.TagList's
// hidden inputs — stopping at the first missing index. Returns nil when no
// indexed field is present.
func indexedFormValues(v url.Values, base string) []string {
	var out []string
	for i := 0; ; i++ {
		vs := v[base+"["+strconv.Itoa(i)+"]"]
		if len(vs) == 0 {
			break
		}
		out = append(out, vs[len(vs)-1])
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// objectPropertyType returns the declared schema type for a property within
// the given compiled property defs, or "" when the property is unknown
// (treated as a plain string).
func objectPropertyType(defs []objectPropertyDef, name string) string {
	for _, d := range defs {
		if d.Name == name {
			return d.Type
		}
	}
	return ""
}

// compiledTypePropertyDefs returns the schema properties for the named object
// type (skipping underscore-prefixed internal properties), sorted by name.
// Each property's declared type, widget hint, and enum are read from its raw
// JSON schema ({"type":"...","widget":"...","enum":[...]}); empty Type means
// string/unrecognised. Returns nil when the type is unknown or has no
// parseable properties.
func compiledTypePropertyDefs(objectTypes []CompiledType, typeName string) []objectPropertyDef {
	var props json.RawMessage
	for _, t := range objectTypes {
		if t.Name == typeName {
			props = t.Properties
			break
		}
	}
	if len(props) == 0 {
		return nil
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(props, &m) != nil {
		return nil
	}
	out := make([]objectPropertyDef, 0, len(m))
	for k, raw := range m {
		if strings.HasPrefix(k, "_") {
			continue
		}
		def := objectPropertyDef{Name: k}
		var meta struct {
			Type   string   `json:"type"`
			Widget string   `json:"widget"`
			Enum   []string `json:"enum"`
		}
		if json.Unmarshal(raw, &meta) == nil {
			def.Type = meta.Type
			def.Widget = meta.Widget
			def.Enum = meta.Enum
		}
		out = append(out, def)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// uiObjectNew renders the create-object form, driven by the project's compiled
// object types. Selecting a type (via ?type=) reveals that type's property inputs.
func (s *Server) uiObjectNew(c echo.Context) error {
	ctx := c.Request().Context()
	compiled, err := s.memory.GetCompiledTypes(ctx)
	if err != nil {
		return s.page(c, pageTitle("New object"), ObjectCreatePage(nil, nil, objectCreateForm{}, nil, err))
	}
	var objectTypes []CompiledType
	if compiled != nil {
		objectTypes = compiled.ObjectTypes
	}
	selectedType := c.QueryParam("type")
	propDefs := compiledTypePropertyDefs(objectTypes, selectedType)
	return s.page(c, pageTitle("New object"), ObjectCreatePage(objectTypes, propDefs, objectCreateForm{Type: selectedType}, s.objectLabelSuggestions(ctx), nil))
}

// uiObjectCreate creates an object from the create form and redirects to the
// new object's detail view. On validation or create failure it re-renders the
// form with an error and the submitted values preserved.
func (s *Server) uiObjectCreate(c echo.Context) error {
	ctx := c.Request().Context()
	if err := c.Request().ParseForm(); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid form submission")
	}
	form := objectCreateForm{
		Type:   c.FormValue("type"),
		Key:    c.FormValue("key"),
		Status: c.FormValue("status"),
		Labels: indexedFormValues(c.Request().PostForm, "labels"),
		Props:  map[string][]string{},
	}
	for field, vals := range c.Request().PostForm {
		name, ok := strings.CutPrefix(field, "prop_")
		if !ok || name == "" {
			continue
		}
		if i := strings.IndexByte(name, '['); i >= 0 {
			name = name[:i] // TagList field prop_x[0] → x
		}
		if _, seen := form.Props[name]; seen {
			continue
		}
		// TagList submits prop_x[0], prop_x[1], … — collect all in index order.
		if indexed := indexedFormValues(c.Request().PostForm, "prop_"+name); indexed != nil {
			form.Props[name] = indexed
			continue
		}
		// Scalar field: the last submitted value wins (a boolean property
		// submits its hidden "false" then, when checked, the toggle's "true").
		last := ""
		for _, v := range vals {
			if v != "" {
				last = v
			}
		}
		if last != "" {
			form.Props[name] = []string{last}
		}
	}

	compiled, cerr := s.memory.GetCompiledTypes(ctx)
	if cerr != nil {
		captureError(cerr)
	}
	var objectTypes []CompiledType
	if compiled != nil {
		objectTypes = compiled.ObjectTypes
	}
	suggestions := s.objectLabelSuggestions(ctx)

	render := func(errMsg string) error {
		form.Error = errMsg
		return s.page(c, pageTitle("New object"), ObjectCreatePage(objectTypes, compiledTypePropertyDefs(objectTypes, form.Type), form, suggestions, nil))
	}

	if form.Type == "" {
		return render("Select an object type.")
	}
	if !knownObjectType(objectTypes, form.Type) {
		return render("Unknown object type.")
	}

	req := &CreateObjectRequest{Type: form.Type}
	if form.Key != "" {
		req.Key = &form.Key
	}
	if form.Status != "" {
		req.Status = &form.Status
	}
	req.Labels = form.Labels
	props := map[string]any{}
	defs := compiledTypePropertyDefs(objectTypes, form.Type)
	for name, vals := range form.Props {
		if objectPropertyType(defs, name) == "array" || objectPropertyType(defs, name) == "object" {
			// Multi-value: each TagList value is one element (parsed as JSON
			// when possible, raw string otherwise).
			var items []any
			for _, raw := range vals {
				if raw == "" {
					continue
				}
				var parsed any
				if json.Unmarshal([]byte(raw), &parsed) == nil {
					items = append(items, parsed)
				} else {
					items = append(items, raw)
				}
			}
			if len(items) > 0 {
				props[name] = items
			}
			continue
		}
		// Scalar: keep the existing JSON-parse-or-string behaviour on the value.
		if len(vals) == 0 || vals[0] == "" {
			continue
		}
		var parsed any
		if json.Unmarshal([]byte(vals[0]), &parsed) == nil {
			props[name] = parsed
		} else {
			props[name] = vals[0]
		}
	}
	if len(props) > 0 {
		req.Properties = props
	}

	created, err := s.memory.CreateObject(ctx, req)
	if err != nil {
		captureError(err)
		return render("Could not create the object.")
	}
	return c.Redirect(http.StatusSeeOther, "/objects/"+url.PathEscape(created.ID))
}

// uiObjectChat starts (or resumes) the refinement conversation linked to an
// object: it get-or-creates the canonical conversation seeded with the object
// instruction, then deep-links into /chat with the editor agent preselected.
// When no editor agent resolves, it redirects back with an ?err= flash telling
// the user to create an agent first — never a silent no-op.
func (s *Server) uiObjectChat(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	obj, err := s.memory.GetGraphObject(ctx, id)
	if err != nil {
		return c.Redirect(http.StatusSeeOther, "/objects/"+url.PathEscape(id))
	}
	agentID := s.resolveEditorAgentID(ctx)
	if agentID == "" {
		return redirectWithError(c, "/objects/"+url.PathEscape(id), fmt.Errorf("no agents configured — create an agent first, then come back to chat about this object"))
	}
	convID, err := s.memory.CreateObjectConversation(ctx, obj.CanonicalID, graphObjectLabel(*obj), objectChatInstruction(obj))
	if err != nil {
		return c.Redirect(http.StatusSeeOther, "/objects/"+url.PathEscape(id))
	}
	return c.Redirect(http.StatusSeeOther, "/chat?c="+url.QueryEscape(convID)+"&agent="+url.QueryEscape(agentID))
}

// uiObjectMerge starts a merge session: redirects to /chat with the merge
// instruction pre-filled and the merge agent preselected.
func (s *Server) uiObjectMerge(c echo.Context) error {
	srcID := c.Param("id")
	dstID := c.QueryParam("with")
	if dstID == "" {
		return c.Redirect(http.StatusSeeOther, "/objects/"+srcID)
	}
	ctx := c.Request().Context()
	src, err := s.memory.GetGraphObject(ctx, srcID)
	if err != nil {
		return c.Redirect(http.StatusSeeOther, "/objects/"+srcID)
	}
	dst, err := s.memory.GetGraphObject(ctx, dstID)
	if err != nil {
		return c.Redirect(http.StatusSeeOther, "/objects/"+srcID)
	}
	agentID := s.mergeAgentID(ctx)
	if agentID == "" {
		return c.Redirect(http.StatusSeeOther, "/objects/"+srcID)
	}
	msg := mergeInstruction(src, dst)
	return c.Redirect(http.StatusSeeOther, "/chat?agent="+url.QueryEscape(agentID)+"&prompt="+url.QueryEscape(msg))
}

// mergeAgentID picks the agent to run a merge: the default agent (IsDefault),
// else the first enabled agent, else the first agent. Empty on error/none.
func (s *Server) mergeAgentID(ctx context.Context) string {
	agents, err := s.memory.ListAgentDefinitions(ctx)
	if err != nil {
		return ""
	}
	for _, a := range agents {
		if a.IsDefault {
			return a.ID
		}
	}
	for _, a := range agents {
		if a.Enabled {
			return a.ID
		}
	}
	if len(agents) > 0 {
		return agents[0].ID
	}
	return ""
}

// resolveEditorAgentID returns the agent to run object-content edits: the
// configured editor agent (project setting), else the default/enabled/first
// fallback. Empty when none resolve.
func (s *Server) resolveEditorAgentID(ctx context.Context) string {
	if ps, perr := s.memory.GetProjectSetting(ctx, settingsEditorCategory, settingsEditorKey); perr == nil && ps != nil {
		if id, ok := ps.Value["agentId"].(string); ok && id != "" {
			return id
		}
	}
	return s.mergeAgentID(ctx)
}

// similarObjectLabel renders the display label for a similar-object row: the
// object's key, else a name/title/summary/content property, else type + short id.
func similarObjectLabel(so SimilarObject) string {
	return graphObjectLabel(GraphObject{ID: so.ID, Type: so.Type, Key: so.Key, Properties: so.Properties})
}

// similarMatchThresholdPercent is the minimum match percentage (0..100) a
// similar object must exceed to be surfaced. 80 = "over 80% match".
const similarMatchThresholdPercent = 80

// similarMatchPercent converts a pgvector cosine distance (0..2, lower = more
// similar) into a match percentage (0..100), clamped. Higher = more similar.
func similarMatchPercent(d float32) int {
	s := 1 - float64(d)
	if s < 0 {
		s = 0
	}
	if s > 1 {
		s = 1
	}
	return int(math.Round(s * 100))
}

// filterSimilarMatches keeps only similar objects whose match percentage
// exceeds the threshold, preserving order.
func filterSimilarMatches(similar []SimilarObject) []SimilarObject {
	out := make([]SimilarObject, 0, len(similar))
	for _, so := range similar {
		if similarMatchPercent(so.Distance) > similarMatchThresholdPercent {
			out = append(out, so)
		}
	}
	return out
}

// similarityLabel converts a pgvector cosine distance (0..2, lower = more
// similar) into a match percentage label, e.g. "83% match".
func similarityLabel(d float32) string {
	return strconv.Itoa(similarMatchPercent(d)) + "% match"
}

// mergeInstruction builds the first message for a merge session: it names both
// objects (label, id, type, properties) and asks the agent to propose a merged
// shape before applying the merge with its memory tools.
func mergeInstruction(src, dst *GraphObject) string {
	var b strings.Builder
	writeObject := func(o *GraphObject) {
		fmt.Fprintf(&b, "- Label: %s\n", graphObjectLabel(*o))
		fmt.Fprintf(&b, "- ID: %s\n", o.ID)
		if o.Type != "" {
			fmt.Fprintf(&b, "- Type: %s\n", o.Type)
		}
		if o.Status != "" {
			fmt.Fprintf(&b, "- Status: %s\n", o.Status)
		}
		if len(o.Properties) > 0 {
			b.WriteString("- Properties:\n")
			for _, p := range graphObjectProperties(*o) {
				fmt.Fprintf(&b, "  %s: %s\n", p.Key, p.Value)
			}
		}
	}
	b.WriteString("Merge the following two knowledge-graph objects into one.\n\n")
	b.WriteString("Object A (source):\n")
	writeObject(src)
	b.WriteString("\nObject B (target):\n")
	writeObject(dst)
	b.WriteString("\nSteps:\n")
	b.WriteString("1. FIRST propose the merged shape: a single type, a single key, merged properties (reconciling conflicting values), and which relationships to keep or rewire.\n")
	b.WriteString("2. THEN apply the merge using your memory tools: create/update the merged object and retire the source so only one object remains.\n")
	return b.String()
}

// objectChatInstruction builds the first message for an object-chat session:
// it names the object (label, id, type, status, properties) and asks the agent
// to discuss/edit/refine the object's content.
func objectChatInstruction(obj *GraphObject) string {
	var b strings.Builder
	writeObject := func(o *GraphObject) {
		fmt.Fprintf(&b, "- Label: %s\n", graphObjectLabel(*o))
		fmt.Fprintf(&b, "- ID: %s\n", o.ID)
		if o.Type != "" {
			fmt.Fprintf(&b, "- Type: %s\n", o.Type)
		}
		if o.Status != "" {
			fmt.Fprintf(&b, "- Status: %s\n", o.Status)
		}
		if len(o.Properties) > 0 {
			b.WriteString("- Properties:\n")
			for _, p := range graphObjectProperties(*o) {
				fmt.Fprintf(&b, "  %s: %s\n", p.Key, p.Value)
			}
		}
	}
	b.WriteString("Help me work with this knowledge-graph object.\n\n")
	b.WriteString("Object:\n")
	writeObject(obj)
	b.WriteString("\nSteps:\n")
	b.WriteString("1. FIRST review the object and propose any edits: corrected or added properties, a better key or type, reworded content, or relationships worth creating.\n")
	b.WriteString("2. THEN apply the edits with your memory tools once I confirm: update the object, create relationships, or search for related objects as needed.\n")
	return b.String()
}

// loadRelatedObjects fetches the objects referenced by the given edges so
// relationship rows can resolve src/dst labels.
func (s *Server) loadRelatedObjects(ctx context.Context, obj *GraphObject, edges []GraphRelationship) []GraphObject {
	idSet := map[string]bool{}
	for _, r := range edges {
		for _, id := range []string{r.SrcID, r.DstID} {
			if id != "" && id != obj.ID && id != obj.CanonicalID {
				idSet[id] = true
			}
		}
	}
	ids := make([]string, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil
	}
	objects, err := s.memory.ListGraphObjects(ctx, obj.BranchID, "", ids)
	captureError(err)
	return objects
}

// distinctObjectTypes returns the sorted distinct types of the given objects.
func distinctObjectTypes(objects []GraphObject) []string {
	seen := map[string]bool{}
	var out []string
	for _, o := range objects {
		if o.Type != "" && !seen[o.Type] {
			seen[o.Type] = true
			out = append(out, o.Type)
		}
	}
	sort.Strings(out)
	return out
}

// distinctObjectLabels returns the sorted, deduped, non-empty labels across
// the given objects (used to autocomplete the create form's label TagList).
func distinctObjectLabels(objects []GraphObject) []string {
	seen := map[string]bool{}
	var out []string
	for _, o := range objects {
		for _, l := range o.Labels {
			if l != "" && !seen[l] {
				seen[l] = true
				out = append(out, l)
			}
		}
	}
	sort.Strings(out)
	return out
}

// objectLabelSuggestions returns the project's existing distinct object labels
// for the create form's label autocomplete — best-effort: an error or an empty
// graph yields nil (the input then simply has no suggestions).
func (s *Server) objectLabelSuggestions(ctx context.Context) []string {
	objects, err := s.memory.ListGraphObjects(ctx, "", "", nil)
	if err != nil {
		captureError(err)
		return nil
	}
	return distinctObjectLabels(objects)
}

// objectEndpointLabel resolves one relationship endpoint id to a display
// label: the object itself, a related object, or a shortened id.
func objectEndpointLabel(obj *GraphObject, related []GraphObject, id string) string {
	if obj != nil && (id == obj.ID || id == obj.CanonicalID) {
		return graphObjectLabel(*obj)
	}
	return graphObjectLabelByID(related, id)
}

// propertyKV is one key/value pair of an object's properties for display.
type propertyKV struct {
	Key   string
	Value string
}

// graphObjectProperties returns the object's display properties sorted by key,
// skipping internal (underscore-prefixed) and empty values.
func graphObjectProperties(o GraphObject) []propertyKV {
	keys := make([]string, 0, len(o.Properties))
	for k := range o.Properties {
		if strings.HasPrefix(k, "_") {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]propertyKV, 0, len(keys))
	for _, k := range keys {
		if v := strAny(o.Properties[k]); v != "" {
			out = append(out, propertyKV{Key: k, Value: v})
		}
	}
	return out
}

// propInputValue renders an object property value as form input text: strings
// pass through unchanged; non-scalar values (numbers, bools, arrays, objects)
// stringify as JSON so the update handler can parse them back to the same
// typed value.
func propInputValue(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

// objectPropertyDefByName returns the compiled schema def (with its Type) for a
// stored property, so the edit form can render a type-appropriate input.
func objectPropertyDefByName(defs []objectPropertyDef, name string) (objectPropertyDef, bool) {
	for _, d := range defs {
		if d.Name == name {
			return d, true
		}
	}
	return objectPropertyDef{}, false
}

// propValues converts a stored object property value to the []string form the
// propertyInput templ expects: scalar → single element, slice → one per element.
func propValues(v any) []string {
	if v == nil {
		return nil
	}
	switch t := v.(type) {
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			out = append(out, propInputValue(e))
		}
		return out
	case []string:
		return t
	default:
		return []string{propInputValue(v)}
	}
}

// relationshipOptions returns the compiled relationship types usable from an
// object of the given type: those whose source or target is that type,
// falling back to all types when objType is empty or nothing matches.
func relationshipOptions(relTypes []CompiledType, objType string) []CompiledType {
	if objType == "" {
		return relTypes
	}
	var out []CompiledType
	for _, rt := range relTypes {
		if rt.SourceType == objType || rt.TargetType == objType {
			out = append(out, rt)
		}
	}
	if len(out) > 0 {
		return out
	}
	return relTypes
}

// relTypeDisplayName renders a compiled relationship type's option label,
// falling back to its raw name when no human label is configured.
func relTypeDisplayName(rt CompiledType) string {
	if rt.Label != "" {
		return rt.Label
	}
	return rt.Name
}
