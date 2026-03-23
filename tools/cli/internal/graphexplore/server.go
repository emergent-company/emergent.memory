package graphexplore

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/a-h/templ"
)

// PALETTE matches the JS palette for consistent colors.
var PALETTE = []string{
	"#58a6ff", "#bc8cff", "#3fb950", "#d29922", "#f78166",
	"#79c0ff", "#ffa657", "#ff7b72", "#56d364", "#e3b341",
	"#a5d6ff", "#d2a8ff", "#7ee787", "#f0883e", "#ff9492",
}

// Server holds the state for the graph explore HTTP server.
type Server struct {
	ProjectID  string
	ServerURL  string
	AuthHeader string
	httpClient *http.Client

	// Cached schema data (loaded once on first request)
	objectTypes       []ObjectType
	relationshipTypes []RelationshipType
	typeColorMap      map[string]string
	relLabelMap       map[string]relLabelEntry
	paletteIdx        int
	schemaLoaded      bool
}

type relLabelEntry struct {
	Label        string
	InverseLabel string
}

// NewServer creates a new graph explore server.
func NewServer(projectID, serverURL, authHeader string) *Server {
	return &Server{
		ProjectID:    projectID,
		ServerURL:    serverURL,
		AuthHeader:   authHeader,
		httpClient:   &http.Client{},
		typeColorMap: make(map[string]string),
		relLabelMap:  make(map[string]relLabelEntry),
	}
}

// typeColor assigns a consistent color to a type name.
func (s *Server) typeColor(typeName string) string {
	if c, ok := s.typeColorMap[typeName]; ok {
		return c
	}
	c := PALETTE[s.paletteIdx%len(PALETTE)]
	s.paletteIdx++
	s.typeColorMap[typeName] = c
	return c
}

// proxyGet makes a GET request to the Memory API server.
func (s *Server) proxyGet(path string) ([]byte, int, error) {
	url := s.ServerURL + path
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, 0, err
	}
	if s.AuthHeader != "" {
		req.Header.Set("Authorization", s.AuthHeader)
	}
	if s.ProjectID != "" {
		req.Header.Set("X-Project-ID", s.ProjectID)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	return body, resp.StatusCode, err
}

// loadSchema fetches compiled-types and schema-registry, caches the result.
func (s *Server) loadSchema() error {
	if s.schemaLoaded {
		return nil
	}

	if s.ProjectID == "" {
		return fmt.Errorf("no project ID configured — restart with --project <id>")
	}

	// 1. compiled-types
	body, status, err := s.proxyGet(fmt.Sprintf("/api/schemas/projects/%s/compiled-types", s.ProjectID))
	if err != nil {
		return fmt.Errorf("compiled-types: %w", err)
	}
	if status == 401 {
		return fmt.Errorf("authentication failed (HTTP 401) — credentials may be expired, run 'memory login'")
	}
	if status == 403 {
		return fmt.Errorf("access denied (HTTP 403) — check project permissions")
	}
	if status != 200 {
		return fmt.Errorf("compiled-types: HTTP %d — %s", status, truncateBody(body, 200))
	}

	var compiled struct {
		ObjectTypes       []compiledObjectType `json:"objectTypes"`
		RelationshipTypes []compiledRelType    `json:"relationshipTypes"`
	}
	if err := json.Unmarshal(body, &compiled); err != nil {
		return fmt.Errorf("compiled-types parse: %w", err)
	}

	// 2. schema-registry
	regBody, _, _ := s.proxyGet(fmt.Sprintf("/api/schema-registry/projects/%s", s.ProjectID))
	var regEntries []registryEntry
	_ = json.Unmarshal(regBody, &regEntries)
	regByType := make(map[string]registryEntry)
	for _, e := range regEntries {
		if e.Type != "" {
			regByType[e.Type] = e
		}
	}

	// 3. Merge into ObjectType list
	s.objectTypes = nil
	for _, ot := range compiled.ObjectTypes {
		if ot.Name == "" {
			continue
		}
		reg := regByType[ot.Name]
		color := ""
		icon := ""
		if reg.UIConfig != nil {
			color = reg.UIConfig.Color
			icon = reg.UIConfig.Icon
		}
		if color == "" {
			color = s.typeColor(ot.Name)
		} else {
			s.typeColorMap[ot.Name] = color
		}
		if icon == "" {
			icon = firstLetter(ot.Name)
		} else {
			icon = resolveIcon(icon, ot.Name)
		}
		desc := ot.Description
		if desc == "" {
			desc = reg.Description
		}
		s.objectTypes = append(s.objectTypes, ObjectType{
			Name:        ot.Name,
			Label:       ot.Label,
			Description: desc,
			Color:       color,
			Icon:        icon,
			Count:       0,
			InGraph:     0,
		})
	}

	// Also add registry-only types
	for _, e := range regEntries {
		if e.Type == "" {
			continue
		}
		found := false
		for _, ot := range s.objectTypes {
			if ot.Name == e.Type {
				found = true
				break
			}
		}
		if !found {
			color := ""
			icon := ""
			if e.UIConfig != nil {
				color = e.UIConfig.Color
				icon = e.UIConfig.Icon
			}
			if color == "" {
				color = s.typeColor(e.Type)
			}
			if icon == "" {
				icon = firstLetter(e.Type)
			} else {
				icon = resolveIcon(icon, e.Type)
			}
			s.objectTypes = append(s.objectTypes, ObjectType{
				Name:        e.Type,
				Description: e.Description,
				Color:       color,
				Icon:        icon,
			})
		}
	}

	// 4. Relationship types
	s.relationshipTypes = nil
	for _, rel := range compiled.RelationshipTypes {
		rtype := rel.Name
		if rtype == "" {
			rtype = rel.Type
		}
		if rtype == "" {
			continue
		}
		label := rel.Label
		if label == "" {
			label = rtype
		}
		s.relLabelMap[rtype] = relLabelEntry{
			Label:        label,
			InverseLabel: coalesce(rel.InverseLabel, rel.InverseLabelAlt),
		}
		s.relationshipTypes = append(s.relationshipTypes, RelationshipType{
			Name:         rtype,
			Label:        label,
			InverseLabel: coalesce(rel.InverseLabel, rel.InverseLabelAlt),
			Color:        s.typeColor(rtype),
			SourceType:   rel.SourceType,
			TargetType:   rel.TargetType,
		})
	}

	// 5. Fetch per-type counts in parallel (sequentially here for simplicity)
	for i, ot := range s.objectTypes {
		body, status, err := s.proxyGet(fmt.Sprintf("/api/graph/objects/count?type=%s", ot.Name))
		if err == nil && status == 200 {
			var countResp struct {
				Count int `json:"count"`
			}
			if json.Unmarshal(body, &countResp) == nil {
				s.objectTypes[i].Count = countResp.Count
			}
		}
	}

	// Only cache if we got at least some types — if empty, allow retry on next request
	if len(s.objectTypes) > 0 || len(s.relationshipTypes) > 0 {
		s.schemaLoaded = true
	}
	return nil
}

// RegisterRoutes adds all HTTP handlers to the mux.
func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	// Main page
	mux.HandleFunc("/", s.handlePage)

	// Static JS file
	mux.HandleFunc("/static/graph_explore.js", s.handleStaticJS)

	// HTMX partials
	mux.HandleFunc("/htmx/node-types", s.handleNodeTypes)
	mux.HandleFunc("/htmx/edge-types", s.handleEdgeTypes)
	mux.HandleFunc("/htmx/node-detail", s.handleNodeDetail)
	mux.HandleFunc("/htmx/node-relations", s.handleNodeRelations)

	// Proxy — pass-through to Memory API (for JS canvas operations: expand, search, load-by-type)
	mux.HandleFunc("/proxy/", s.handleProxy)
}

func (s *Server) handlePage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	component := PageLayout(s.ProjectID)
	_ = component.Render(r.Context(), w)
}

func (s *Server) handleStaticJS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(graphExploreJS)
}

func (s *Server) handleNodeTypes(w http.ResponseWriter, r *http.Request) {
	if err := s.loadSchema(); err != nil {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(fmt.Sprintf(`<div class="px-3 py-3 text-[11px] text-red-400">Failed to load types: %s</div>`, err.Error())))
		return
	}

	// Build set of hidden type names from query param (comma-separated)
	hiddenSet := make(map[string]bool)
	if h := r.URL.Query().Get("hiddenNodeTypes"); h != "" {
		for _, name := range strings.Split(h, ",") {
			if name = strings.TrimSpace(name); name != "" {
				hiddenSet[name] = true
			}
		}
	}

	// Which type is currently selected (for relationship filtering highlight)
	selectedType := strings.TrimSpace(r.URL.Query().Get("selectedType"))

	// Sort by count descending; apply hidden + selected state
	types := make([]ObjectType, len(s.objectTypes))
	copy(types, s.objectTypes)
	sort.Slice(types, func(i, j int) bool {
		return types[i].Count > types[j].Count
	})
	for i := range types {
		types[i].Hidden = hiddenSet[types[i].Name]
		types[i].Selected = selectedType != "" && types[i].Name == selectedType
	}

	w.Header().Set("Content-Type", "text/html")
	component := NodeTypeList(types)
	_ = component.Render(r.Context(), w)
}

func (s *Server) handleEdgeTypes(w http.ResponseWriter, r *http.Request) {
	if err := s.loadSchema(); err != nil {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<div class="px-3 py-3 text-[11px] text-red-400">Failed to load edge types</div>`))
		return
	}

	// Build set of hidden edge type names from query param (comma-separated)
	hiddenSet := make(map[string]bool)
	if h := r.URL.Query().Get("hiddenEdgeTypes"); h != "" {
		for _, name := range strings.Split(h, ",") {
			if name = strings.TrimSpace(name); name != "" {
				hiddenSet[name] = true
			}
		}
	}

	// Filter by selected node type — only show relationships where source or target matches
	selectedType := strings.TrimSpace(r.URL.Query().Get("selectedType"))

	types := make([]RelationshipType, 0, len(s.relationshipTypes))
	for _, rt := range s.relationshipTypes {
		// If a type is selected, skip relationships that don't involve it
		if selectedType != "" {
			src := rt.SourceType
			dst := rt.TargetType
			if src != "" || dst != "" {
				// Both fields present — filter strictly
				if src != selectedType && dst != selectedType {
					continue
				}
			}
			// If both SourceType and TargetType are empty (no schema info), include it
		}
		rt.Hidden = hiddenSet[rt.Name]
		types = append(types, rt)
	}

	w.Header().Set("Content-Type", "text/html")
	component := EdgeTypeList(types)
	_ = component.Render(r.Context(), w)
}

func (s *Server) handleNodeDetail(w http.ResponseWriter, r *http.Request) {
	nodeID := r.URL.Query().Get("nodeId")
	if nodeID == "" {
		nodeID = r.FormValue("nodeId")
	}
	if nodeID == "" {
		_, _ = w.Write([]byte(`<div class="px-3 py-3 text-[11px] text-gh-muted">Select a node</div>`))
		return
	}

	// Fetch node data from the API
	body, status, err := s.proxyGet(fmt.Sprintf("/api/graph/objects/%s", nodeID))
	if err != nil || status != 200 {
		_, _ = w.Write([]byte(fmt.Sprintf(`<div class="px-3 py-3 text-[11px] text-red-400">Failed to load node: %v</div>`, err)))
		return
	}

	var node struct {
		ID          string                 `json:"id"`
		CanonicalID string                 `json:"canonical_id"`
		EntityID    string                 `json:"entity_id"`
		Type        string                 `json:"type"`
		Key         string                 `json:"key"`
		Labels      []string               `json:"labels"`
		Properties  map[string]interface{} `json:"properties"`
	}
	if err := json.Unmarshal(body, &node); err != nil {
		_, _ = w.Write([]byte(fmt.Sprintf(`<div class="px-3 py-3 text-[11px] text-red-400">Parse error: %v</div>`, err)))
		return
	}

	id := coalesce(node.CanonicalID, node.EntityID, node.ID)
	typeName := node.Type
	if typeName == "" {
		typeName = "unknown"
	}

	// Build properties list
	var props []PropertyField
	idx := 0
	for k, v := range node.Properties {
		val := "—"
		if v != nil {
			switch vt := v.(type) {
			case string:
				val = vt
			default:
				b, _ := json.Marshal(v)
				val = string(b)
			}
		}
		props = append(props, PropertyField{Key: k, Value: val, Index: idx})
		idx++
	}

	detail := NodeDetail{
		ID:         id,
		Type:       typeName,
		TypeColor:  s.typeColor(typeName),
		Key:        node.Key,
		Labels:     node.Labels,
		Properties: props,
	}

	w.Header().Set("Content-Type", "text/html")
	component := NodeDetailContent(detail)
	_ = component.Render(r.Context(), w)
}

func (s *Server) handleNodeRelations(w http.ResponseWriter, r *http.Request) {
	nodeID := r.URL.Query().Get("nodeId")
	if nodeID == "" {
		_, _ = w.Write([]byte(`<div class="px-3 py-3 text-[11px] text-gh-muted italic">No relationships</div>`))
		return
	}

	// Fetch edges
	body, status, err := s.proxyGet(fmt.Sprintf("/api/graph/objects/%s/edges", nodeID))
	if err != nil || status != 200 {
		_, _ = w.Write([]byte(fmt.Sprintf(`<div class="px-3 py-3 text-[11px] text-red-400">Failed: %v</div>`, err)))
		return
	}

	var edgesResp struct {
		Outgoing []edgeEntry `json:"outgoing"`
		Incoming []edgeEntry `json:"incoming"`
	}
	if err := json.Unmarshal(body, &edgesResp); err != nil {
		_, _ = w.Write([]byte(fmt.Sprintf(`<div class="px-3 py-3 text-[11px] text-red-400">Parse error: %v</div>`, err)))
		return
	}

	// Batch-fetch unknown node names
	unknownIDs := make(map[string]bool)
	for _, e := range edgesResp.Outgoing {
		unknownIDs[e.DstID] = true
	}
	for _, e := range edgesResp.Incoming {
		unknownIDs[e.SrcID] = true
	}
	delete(unknownIDs, nodeID)

	nodeNames := make(map[string]nodeInfo)
	if len(unknownIDs) > 0 {
		ids := make([]string, 0, len(unknownIDs))
		for id := range unknownIDs {
			ids = append(ids, id)
		}
		searchBody, sstatus, serr := s.proxyGet(fmt.Sprintf("/api/graph/objects/search?ids=%s&limit=200", strings.Join(ids, ",")))
		if serr == nil && sstatus == 200 {
			var searchResp struct {
				Items []json.RawMessage `json:"items"`
				Data  []json.RawMessage `json:"data"`
			}
			_ = json.Unmarshal(searchBody, &searchResp)
			items := searchResp.Items
			if len(items) == 0 {
				items = searchResp.Data
			}
			for _, raw := range items {
				var obj struct {
					ID          string                 `json:"id"`
					CanonicalID string                 `json:"canonical_id"`
					EntityID    string                 `json:"entity_id"`
					Type        string                 `json:"type"`
					Key         string                 `json:"key"`
					Properties  map[string]interface{} `json:"properties"`
				}
				if json.Unmarshal(raw, &obj) == nil {
					oid := coalesce(obj.CanonicalID, obj.EntityID, obj.ID)
					name := ""
					if props := obj.Properties; props != nil {
						if n, ok := props["name"].(string); ok {
							name = n
						} else if t, ok := props["title"].(string); ok {
							name = t
						}
					}
					if name == "" {
						name = obj.Key
					}
					if name == "" && obj.Type != "" {
						name = obj.Type + " " + oid[:min(6, len(oid))]
					}
					nodeNames[oid] = nodeInfo{Name: name, Type: obj.Type}
				}
			}
		}
	}

	// Build relation groups
	var groups []RelationGroup

	// Outgoing grouped by type
	outByType := make(map[string][]Relation)
	for _, e := range edgesResp.Outgoing {
		info := nodeNames[e.DstID]
		name := info.Name
		if name == "" {
			name = e.DstID[:min(8, len(e.DstID))]
		}
		otype := info.Type
		outByType[e.Type] = append(outByType[e.Type], Relation{
			OtherID:    e.DstID,
			OtherName:  name,
			OtherType:  otype,
			OtherColor: s.typeColor(otype),
		})
	}
	for rtype, rels := range outByType {
		label := rtype
		if entry, ok := s.relLabelMap[rtype]; ok && entry.Label != "" {
			label = entry.Label
		}
		for i := range rels {
			rels[i].Index = i
		}
		groups = append(groups, RelationGroup{TypeLabel: label, Relations: rels})
	}

	// Incoming grouped by type
	inByType := make(map[string][]Relation)
	for _, e := range edgesResp.Incoming {
		info := nodeNames[e.SrcID]
		name := info.Name
		if name == "" {
			name = e.SrcID[:min(8, len(e.SrcID))]
		}
		otype := info.Type
		inByType[e.Type] = append(inByType[e.Type], Relation{
			OtherID:    e.SrcID,
			OtherName:  name,
			OtherType:  otype,
			OtherColor: s.typeColor(otype),
		})
	}
	for rtype, rels := range inByType {
		label := rtype
		if entry, ok := s.relLabelMap[rtype]; ok {
			if entry.InverseLabel != "" {
				label = entry.InverseLabel
			} else if entry.Label != "" {
				label = entry.Label
			}
		}
		for i := range rels {
			rels[i].Index = i
		}
		groups = append(groups, RelationGroup{TypeLabel: label, Relations: rels})
	}

	w.Header().Set("Content-Type", "text/html")
	component := NodeRelationsContent(groups)
	_ = component.Render(r.Context(), w)
}

func (s *Server) handleProxy(w http.ResponseWriter, r *http.Request) {
	upstreamPath := strings.TrimPrefix(r.URL.Path, "/proxy")
	upstreamURL := s.ServerURL + upstreamPath
	if r.URL.RawQuery != "" {
		upstreamURL += "?" + r.URL.RawQuery
	}

	proxyReq, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL, r.Body)
	if err != nil {
		http.Error(w, "Failed to create upstream request", http.StatusInternalServerError)
		return
	}

	if ct := r.Header.Get("Content-Type"); ct != "" {
		proxyReq.Header.Set("Content-Type", ct)
	}
	if s.AuthHeader != "" {
		proxyReq.Header.Set("Authorization", s.AuthHeader)
	}
	if s.ProjectID != "" {
		proxyReq.Header.Set("X-Project-ID", s.ProjectID)
	}

	resp, err := s.httpClient.Do(proxyReq)
	if err != nil {
		http.Error(w, fmt.Sprintf("Upstream request failed: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(resp.StatusCode)
	buf := make([]byte, 32*1024)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			_, _ = w.Write(buf[:n])
		}
		if rerr != nil {
			break
		}
	}
}

// RenderPartial is a helper to render a templ component to an http.ResponseWriter.
func RenderPartial(w http.ResponseWriter, r *http.Request, component templ.Component) {
	w.Header().Set("Content-Type", "text/html")
	_ = component.Render(r.Context(), w)
}

// ── Internal types for JSON parsing ──────────────────────────────────────

type compiledObjectType struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

type compiledRelType struct {
	Name            string `json:"name"`
	Type            string `json:"type"`
	Label           string `json:"label"`
	InverseLabel    string `json:"inverseLabel"`
	InverseLabelAlt string `json:"inverse_label"`
	SourceType      string `json:"sourceType"`
	TargetType      string `json:"targetType"`
}

type registryEntry struct {
	Type        string    `json:"type"`
	Description string    `json:"description"`
	UIConfig    *uiConfig `json:"ui_config"`
}

type uiConfig struct {
	Color string `json:"color"`
	Icon  string `json:"icon"`
}

type edgeEntry struct {
	Type  string `json:"type"`
	SrcID string `json:"src_id"`
	DstID string `json:"dst_id"`
}

type nodeInfo struct {
	Name string
	Type string
}

func coalesce(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// truncateBody returns at most maxLen bytes of a response body for error messages.
func truncateBody(body []byte, maxLen int) string {
	if len(body) <= maxLen {
		return string(body)
	}
	return string(body[:maxLen]) + "..."
}
