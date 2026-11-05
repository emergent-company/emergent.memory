package mcp

// ============================================================================
// Share templates
// ============================================================================

// Share templates are named presets of memory tools offered when creating a
// share instance. They are a convenience over the raw tool allowlist: the
// create/update payload still carries `tools`, so an API or CLI client is
// unaffected by their presence.
//
// A template is declared as the required scopes it includes wholesale, plus
// explicit tool names where a preset needs a subset of a scope. It is resolved
// against the project's shareable catalog at request time, which keeps the
// presets correct as the tool surface evolves:
//
//   - a new tool under an included scope joins every preset that includes that
//     scope (no code change here);
//   - a renamed or removed tool drops out silently instead of breaking a
//     create;
//   - a template that resolves to no tools is omitted from the response, so the
//     UI never offers an empty preset.
//
// IDs are frozen: the gateway renders them as `data-share-template` values and
// a client may remember a chosen id. Renaming an id is a contract change;
// labels and descriptions are display-only and may change freely.
//
// Templates can never exceed what a share may carry. `admin`, `admin:all`,
// `projects:write`, and the agent execution/mutation tools are refused by the
// share allowlist itself (see isNonShareableScope and agentMutationTools), so
// Full access is the largest preset and the UI must say so rather than implying
// platform administration.

// ShareTemplate is one preset definition. Tools and Scopes are unioned; All
// means "every shareable tool in the catalog".
type ShareTemplate struct {
	ID          string
	Label       string
	Description string
	// Default marks the preset preselected on the create form.
	Default bool
	// Scopes are required scopes included wholesale.
	Scopes []string
	// Tools are extra tool names included when the catalog carries them.
	Tools []string
	// All includes every tool in the shareable catalog.
	All bool
}

// shareTemplates is the ordered preset list. Order is the display order the
// gateway renders; Default is expected on exactly one entry (asserted in tests).
var shareTemplates = []ShareTemplate{
	{
		ID:          "read-only",
		Label:       "Read-only",
		Description: "Search and read the knowledge graph, documents, schemas, journal and agent runs — nothing can be changed.",
		Default:     true,
		Scopes: []string{
			"search",
			"graph:read",
			"documents:read",
			"journal:read",
			"schema:read",
			"skills:read",
			"agents:read",
			"projects:read",
		},
	},
	{
		ID:          "knowledge-writer",
		Label:       "Knowledge writer",
		Description: "Everything read-only, plus creating, editing and deleting objects, relationships, documents and journal notes.",
		Scopes: []string{
			"search",
			"graph:read",
			"documents:read",
			"journal:read",
			"schema:read",
			"skills:read",
			"agents:read",
			"projects:read",
			"graph:write",
			"documents:write",
			"journal:write",
		},
	},
	{
		ID:          "schema-admin",
		Label:       "Schema & blueprints",
		Description: "Inspect the graph, then manage schemas, schema migrations and blueprints.",
		Scopes: []string{
			"graph:read",
			"schema:read",
			"schema:write",
			"schema:migrate",
			"skills:read",
		},
	},
	{
		ID:          "agent-observer",
		Label:       "Agent observer",
		Description: "Watch agents, runs, questions and sessions, plus read access to the knowledge they work on.",
		Scopes: []string{
			"agents:read",
			"search",
			"graph:read",
		},
	},
	{
		ID:          "documents-only",
		Label:       "Documents",
		Description: "Upload, list, retrieve and delete documents, with keyword and semantic search over the knowledge base.",
		Scopes: []string{
			"documents:read",
			"documents:write",
		},
		Tools: []string{
			"classify-document",
			"search-hybrid",
			"search-semantic",
		},
	},
	{
		ID:          "full-access",
		Label:       "Full access",
		Description: "Every tool a share key can carry. This is the largest share possible — platform administration and credential management are never shareable.",
		All:         true,
	},
}

// ShareTemplates returns a copy of the preset definitions (tests and callers may
// not mutate the shared slice).
func ShareTemplates() []ShareTemplate {
	out := make([]ShareTemplate, len(shareTemplates))
	copy(out, shareTemplates)
	for i := range out {
		out[i].Scopes = append([]string(nil), shareTemplates[i].Scopes...)
		out[i].Tools = append([]string(nil), shareTemplates[i].Tools...)
	}
	return out
}

// ShareTemplateDTO is a preset resolved against a project's shareable catalog.
// Tools lists concrete tool names in catalog order; ToolCount mirrors len(Tools)
// so a client can render a count without walking the array.
type ShareTemplateDTO struct {
	ID          string   `json:"id"`
	Label       string   `json:"label"`
	Description string   `json:"description"`
	Default     bool     `json:"default,omitempty"`
	Tools       []string `json:"tools"`
	ToolCount   int      `json:"toolCount"`
}

// ResolveShareTemplates expands every preset against catalog (already filtered
// to shareable tools) and drops presets that resolve to nothing. Tool order
// follows the catalog, which is category then name sorted, so the same share
// renders identically everywhere.
func ResolveShareTemplates(catalog []CatalogToolDTO) []ShareTemplateDTO {
	has := make(map[string]bool, len(catalog))
	for _, tool := range catalog {
		has[tool.Name] = true
	}
	out := make([]ShareTemplateDTO, 0, len(shareTemplates))
	for _, t := range shareTemplates {
		want := make(map[string]bool, len(t.Scopes)+len(t.Tools))
		if t.All {
			for _, tool := range catalog {
				want[tool.Name] = true
			}
		}
		for _, scope := range t.Scopes {
			for _, tool := range catalog {
				if tool.RequiredScope == scope {
					want[tool.Name] = true
				}
			}
		}
		for _, name := range t.Tools {
			if has[name] {
				want[name] = true
			}
		}
		tools := make([]string, 0, len(want))
		for _, tool := range catalog {
			if want[tool.Name] {
				tools = append(tools, tool.Name)
			}
		}
		if len(tools) == 0 {
			continue
		}
		out = append(out, ShareTemplateDTO{
			ID:          t.ID,
			Label:       t.Label,
			Description: t.Description,
			Default:     t.Default,
			Tools:       tools,
			ToolCount:   len(tools),
		})
	}
	return out
}
