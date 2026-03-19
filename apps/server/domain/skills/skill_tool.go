package skills

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"

	"github.com/emergent-company/emergent.memory/pkg/embeddings"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// SkillListThreshold is the maximum number of skills before semantic retrieval kicks in.
// When total agent-visible skills ≤ SkillListThreshold, all are listed in the tool description.
// Above this threshold, pgvector semantic search is used to select the top SkillTopK most
// relevant skills for inclusion.
const SkillListThreshold = 50

// SkillTopK is the number of semantically relevant skills to include when the full set
// exceeds SkillListThreshold.
const SkillTopK = 10

// SkillToolDeps holds the dependencies needed by BuildSkillTool.
type SkillToolDeps struct {
	Repo             *Repository
	EmbeddingsSvc    *embeddings.Service
	Logger           *slog.Logger
	ProjectID        string
	OrgID            string // org context for org-scoped skill resolution
	TriggerMessage   string // agent run trigger message (used as query for semantic retrieval)
	AgentName        string
	AgentDescription string
	Skills           []string // declared skill names from agent definition; ["*"] means all; nil/empty means all (legacy)
}

// BuildSkillTool creates the `skill` ADK tool for an agent run.
//
// Selection algorithm:
//   - Fetch all agent-visible skills (global + org-scoped + project-scoped, merged).
//   - If total ≤ SkillListThreshold: include all in the tool description.
//   - If total > SkillListThreshold: embed the trigger message and retrieve
//     the top SkillTopK by cosine similarity. On embedding error, fall back to all.
//
// The tool itself performs an exact name lookup in an in-memory map built at
// construction time. Returns an error if the name is not found.
func BuildSkillTool(ctx context.Context, deps SkillToolDeps) (tool.Tool, error) {
	// Fetch all skills accessible to this agent
	all, err := deps.Repo.FindForAgent(ctx, deps.ProjectID, deps.OrgID)
	if err != nil {
		return nil, fmt.Errorf("skills: failed to load skills for agent: %w", err)
	}

	if len(all) == 0 {
		return nil, nil // nothing to expose
	}

	// Filter to declared skill names when an explicit list is provided (not wildcard).
	all = filterByDeclaredSkills(deps, all)
	if len(all) == 0 {
		return nil, nil // declared skills not found
	}

	// Determine which skills to advertise in the tool description
	advertised := all
	if len(all) > SkillListThreshold {
		advertised = selectRelevantSkills(ctx, deps, all)
	}

	// Build name → skill lookup map from the full set (exact lookup is always global)
	byName := make(map[string]*Skill, len(all))
	for _, s := range all {
		byName[s.Name] = s
	}

	// Build the available_skills XML block for the tool description
	desc := buildSkillListXML(advertised, len(all))

	inputSchema := &jsonschema.Schema{
		Type: "object",
		Properties: map[string]*jsonschema.Schema{
			"name": {
				Type:        "string",
				Description: "The exact name of the skill to load (e.g. 'my-skill')",
			},
		},
		Required: []string{"name"},
	}

	return functiontool.New(
		functiontool.Config{
			Name:        "skill",
			Description: desc,
			InputSchema: inputSchema,
		},
		func(ctx tool.Context, args map[string]any) (map[string]any, error) {
			name, _ := args["name"].(string)
			if name == "" {
				return map[string]any{"error": "name is required"}, nil
			}

			s, ok := byName[name]
			if !ok {
				available := make([]string, 0, len(byName))
				for n := range byName {
					available = append(available, n)
				}
				return map[string]any{
					"error":           fmt.Sprintf("skill %q not found", name),
					"available_names": available,
				}, nil
			}

			content := fmt.Sprintf("<skill_content name=%q>\n%s\n</skill_content>", s.Name, s.Content)
			return map[string]any{"content": content}, nil
		},
	)
}

// selectRelevantSkills performs semantic retrieval when the skill set is large.
// Falls back to the full list on embedding error (with a warning log).
func selectRelevantSkills(ctx context.Context, deps SkillToolDeps, all []*Skill) []*Skill {
	query := deps.TriggerMessage
	if query == "" {
		query = deps.AgentName + " " + deps.AgentDescription
	}
	if query == "" {
		return all
	}

	vec, err := deps.EmbeddingsSvc.EmbedQuery(ctx, query)
	if err != nil {
		deps.Logger.Warn("skills: failed to embed trigger message for semantic retrieval, using full skill list",
			logger.Error(err),
		)
		return all
	}

	relevant, err := deps.Repo.FindRelevant(ctx, deps.ProjectID, deps.OrgID, vec, SkillTopK)
	if err != nil {
		deps.Logger.Warn("skills: semantic retrieval failed, using full skill list",
			logger.Error(err),
		)
		return all
	}

	if len(relevant) == 0 {
		return all
	}
	return relevant
}

// filterByDeclaredSkills narrows the skill set to the names declared in deps.Skills.
// When deps.Skills is empty, nil, or contains only "*", all skills are returned unchanged.
// Skills whose declared names are not found are logged as warnings.
func filterByDeclaredSkills(deps SkillToolDeps, all []*Skill) []*Skill {
	if len(deps.Skills) == 0 || (len(deps.Skills) == 1 && deps.Skills[0] == "*") {
		return all
	}

	byName := make(map[string]*Skill, len(all))
	for _, s := range all {
		byName[s.Name] = s
	}

	filtered := make([]*Skill, 0, len(deps.Skills))
	for _, name := range deps.Skills {
		s, ok := byName[name]
		if !ok {
			deps.Logger.Warn("skills: declared skill not found for agent",
				slog.String("skill_name", name),
				slog.String("agent_name", deps.AgentName),
			)
			continue
		}
		filtered = append(filtered, s)
	}
	return filtered
}

// buildSkillListXML constructs the <available_skills> XML block that is injected
// into the skill tool's description so the LLM knows what skills are available.
func buildSkillListXML(skills []*Skill, total int) string {
	var b strings.Builder

	b.WriteString("Load a named skill to get detailed workflow instructions.\n\n")

	if total > SkillListThreshold {
		fmt.Fprintf(&b, "Showing %d most relevant skills (out of %d total). Call with the exact name.\n\n", len(skills), total)
	}

	b.WriteString("<available_skills>\n")
	for _, s := range skills {
		fmt.Fprintf(&b, "  <skill>\n")
		fmt.Fprintf(&b, "    <name>%s</name>\n", s.Name)
		fmt.Fprintf(&b, "    <description>%s</description>\n", s.Description)
		fmt.Fprintf(&b, "  </skill>\n")
	}
	b.WriteString("</available_skills>\n\n")
	b.WriteString("Call this tool with {\"name\": \"<skill-name>\"} to load the full skill content.")

	return b.String()
}
