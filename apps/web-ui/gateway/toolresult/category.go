package toolresult

import "strings"

// Family is the kind of work a tool does, in the console's own vocabulary. The
// taxonomy is deliberately coarser than one category per tool: the point is a
// legible verb, and a tool the table does not know degrades to FamilyTool rather
// than a broken row.
//
// It mirrors the shipped iOS vocabulary (MemoryToolCategory) so the two clients
// describe the same call the same way. Adding a family means adding one row to
// prefixes or exact *and* its copy here.
type Family string

// The families. A category's verb has to be true of every tool in it, so a family
// that both reads and writes gets neutral copy ("Worked in the session") and only
// a read-only family gets a reading verb ("Read the trace").
const (
	FamilyMemory     Family = "memory"
	FamilySearch     Family = "search"
	FamilySchemas    Family = "schemas"
	FamilyDocuments  Family = "documents"
	FamilyWeb        Family = "web"
	FamilyAgents     Family = "agents"
	FamilySkills     Family = "skills"
	FamilyBlueprints Family = "blueprints"
	FamilyProjects   Family = "projects"
	FamilySession    Family = "session"
	FamilyTrace      Family = "trace"
	FamilyWorkspace  Family = "workspace"
	FamilyConnector  Family = "connector"
	FamilyTool       Family = "tool"
)

// Category is what a step reports instead of its wire name: a verb for each tense,
// the icon that carries the family (colour is reserved for state, so the shape has
// to do the distinguishing), and the family itself as a stable hook for styles.
type Category struct {
	Family Family `json:"family"`
	// Verb is what the settled step says, e.g. "Searched memory".
	Verb string `json:"verb"`
	// RunningVerb is what it says while the call is in flight.
	RunningVerb string `json:"runningVerb"`
	// Icon is a lucide iconify class, e.g. "lucide--search".
	Icon string `json:"icon"`
}

// exact names that no prefix describes. Matched before the prefix table.
var exactFamilies = map[string]Family{
	"remember":                FamilyMemory,
	"forget":                  FamilyMemory,
	"memory-guide":            FamilyMemory,
	"lookup":                  FamilySearch,
	"search_entities":         FamilySearch,
	"set_session_title":       FamilySession,
	"classify-document":       FamilySchemas,
	"finalize-discovery":      FamilySchemas,
	"list-installed-schemas":  FamilySchemas,
	"call_llm":                FamilyAgents,
	"execute_tool":            FamilyAgents,
	"trigger_agent":           FamilyAgents,
	"update_agent":            FamilyAgents,
	"update_agent_definition": FamilyAgents,
	"search_mcp_registry":     FamilyConnector,
	"sync_mcp_server_tools":   FamilyConnector,
	"toggle_mcp_server_tool":  FamilyConnector,
	"update_mcp_server":       FamilyConnector,
	"run_python":              FamilyWorkspace,
	"run_go":                  FamilyWorkspace,
}

// prefixes are matched in order: a more specific family is listed before the
// family it sits inside ("project-record-" before "project-", "adk-session-"
// before "adk-").
var prefixes = []struct {
	prefix string
	family Family
}{
	{"project-record-", FamilyMemory},
	{"graph-", FamilyMemory},
	{"entity-", FamilyMemory},
	{"tag-", FamilyMemory},
	{"journal-", FamilyMemory},
	{"relationship-", FamilyMemory},
	{"search-", FamilySearch},
	{"schema-", FamilySchemas},
	{"migration-", FamilySchemas},
	{"document-", FamilyDocuments},
	{"embedding-", FamilyDocuments},
	{"extraction-", FamilyDocuments},
	{"queue-reextraction", FamilyDocuments},
	{"web-", FamilyWeb},
	{"ts_web_fetch", FamilyWeb},
	{"adk-session-", FamilySession},
	{"agent-", FamilyAgents},
	{"adk-", FamilyAgents},
	{"skill", FamilySkills},
	{"blueprint-", FamilyBlueprints},
	{"project-", FamilyProjects},
	{"org-", FamilyProjects},
	{"user-", FamilyProjects},
	{"session-", FamilySession},
	{"trace-", FamilyTrace},
	{"workspace_", FamilyWorkspace},
	{"account-key-", FamilyConnector},
	{"service-token-", FamilyConnector},
	{"token-", FamilyConnector},
	{"mcp-", FamilyConnector},
	{"mcp_", FamilyConnector},
	{"provider-", FamilyConnector},
	{"a2a-", FamilyConnector},
}

// copyFor holds the wording and glyph per family. One table, so a family cannot
// exist without a verb and an icon.
var copyFor = map[Family]struct {
	verb    string
	running string
	icon    string
}{
	FamilyMemory:     {"Used memory", "Using memory", "lucide--brain"},
	FamilySearch:     {"Searched memory", "Searching memory", "lucide--search"},
	FamilySchemas:    {"Worked with schemas", "Working with schemas", "lucide--layers"},
	FamilyDocuments:  {"Worked with documents", "Working with documents", "lucide--file-text"},
	FamilyWeb:        {"Searched the web", "Searching the web", "lucide--globe"},
	FamilyAgents:     {"Worked with agents", "Working with agents", "lucide--users"},
	FamilySkills:     {"Used a skill", "Using a skill", "lucide--book-open"},
	FamilyBlueprints: {"Worked with blueprints", "Working with blueprints", "lucide--copy"},
	FamilyProjects:   {"Worked with projects", "Working with projects", "lucide--folder"},
	FamilySession:    {"Worked in the session", "Working in the session", "lucide--history"},
	FamilyTrace:      {"Read the trace", "Reading the trace", "lucide--activity"},
	FamilyWorkspace:  {"Worked in the workspace", "Working in the workspace", "lucide--terminal"},
	FamilyConnector:  {"Used a connector", "Using a connector", "lucide--puzzle"},
	FamilyTool:       {"Used a tool", "Using a tool", "lucide--wrench"},
}

// Classify maps a tool's wire name to its category. An unknown name returns the
// generic category, so a new tool family reads as "Used a tool" instead of an
// identifier a person has to decode.
func Classify(name string) Category {
	family := FamilyOf(name)
	copy := copyFor[family]
	return Category{Family: family, Verb: copy.verb, RunningVerb: copy.running, Icon: copy.icon}
}

// FamilyOf returns just the family a tool name belongs to.
func FamilyOf(name string) Family {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if family, ok := exactFamilies[normalized]; ok {
		return family
	}
	for _, entry := range prefixes {
		if strings.HasPrefix(normalized, entry.prefix) {
			return entry.family
		}
	}
	return FamilyTool
}
