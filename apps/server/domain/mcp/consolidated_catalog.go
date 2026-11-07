package mcp

import (
	"fmt"
	"strings"
)

// This file defines the consolidated MCP catalog.
//
// The project MCP surface grew to 107 tools, many of which were per-verb
// entries for a single resource (eight graph-branch-* tools, eleven
// blueprint-* tools, five different "list schemas" reads). This file folds
// those families into one tool per resource-and-permission pair, exposing an
// `action` discriminator, while the legacy names remain callable as
// deprecated aliases so that saved agent definitions, share instances and
// existing clients keep working.
//
// The merge boundary is the required scope: a consolidated tool requires the
// strictest scope of its actions, so read and write paths are separate tools
// (`schema` / `schema-write`). The `-write` suffix mirrors the scope name.

// toolAlias is the deprecated legacy name -> consolidated tool + implied action.
type toolAlias struct {
	Tool   string
	Action string
}

// consolidatedAction maps one public action of a consolidated tool to the
// legacy dispatch case that implements it.
type consolidatedAction struct {
	// Action is the public action value.
	Action string
	// Handler is the legacy dispatchTool case that implements the action.
	Handler string
	// Fold lists alternate legacy names folded into this action, so they are
	// removed from the catalog (they stay callable through toolAliases).
	Fold []string
	// Description is a one-line summary used in the action enum description.
	Description string
	// Pick optionally selects the handler from the call arguments, for the one
	// family where two legacy tools express a single action and the arguments
	// disambiguate them (agent-question list by run vs project-wide).
	Pick func(args map[string]any) string
}

// consolidatedFamily is one consolidated tool.
type consolidatedFamily struct {
	Name        string
	Scope       string
	Description string
	Actions     []consolidatedAction
}

// consolidatedFamilies is the consolidated catalog. Every entry replaces at
// least two legacy tools. Tools that had no sibling keep their legacy name and
// are not listed here (relationship-list, session-get-messages, journal-list,
// journal-add-note, search-knowledge, remember-status, agent-hook-list,
// agent-question-respond).
var consolidatedFamilies = []consolidatedFamily{
	{
		Name:  "project",
		Scope: "graph:read",
		Description: "Read the current project. action=get returns the project info document (purpose, goals, audience); " +
			"action=briefing returns a task briefing with core memories, semantically matched memories and discovered conventions.",
		Actions: []consolidatedAction{
			{Action: "get", Handler: "project-get", Description: "project info document"},
			{Action: "briefing", Handler: "project-briefing", Description: "task briefing with relevant memories"},
		},
	},
	{
		Name:  "entity",
		Scope: "graph:read",
		Description: "Read the knowledge graph. action=query lists entities by type or ids; action=search is a full-text " +
			"(lexeme) search; action=history returns an entity's version history; action=edges returns incoming/outgoing " +
			"relationships; action=traverse walks multiple hops from an entity; action=tags lists the project's labels.",
		Actions: []consolidatedAction{
			{Action: "query", Handler: "entity-query", Description: "list entities by type, ids or key prefix"},
			{Action: "search", Handler: "entity-search", Description: "full-text entity search"},
			{Action: "history", Handler: "entity-history", Description: "entity version history"},
			{Action: "edges", Handler: "entity-edges-get", Description: "an entity's relationships"},
			{Action: "traverse", Handler: "graph-traverse", Description: "multi-hop traversal from an entity"},
			{Action: "tags", Handler: "tag-list", Description: "project label inventory"},
		},
	},
	{
		Name:  "entity-write",
		Scope: "graph:write",
		Description: "Create or change graph entities. action=create takes an entities[] array (idempotent on type+key); " +
			"action=update merges properties (null removes); action=delete soft-deletes; action=restore undeletes.",
		Actions: []consolidatedAction{
			{Action: "create", Handler: "entity-create", Description: "create one or more entities"},
			{Action: "update", Handler: "entity-update", Description: "merge properties on an entity"},
			{Action: "delete", Handler: "entity-delete", Description: "soft-delete an entity"},
			{Action: "restore", Handler: "entity-restore", Description: "restore a soft-deleted entity"},
		},
	},
	{
		Name:  "relationship-write",
		Scope: "graph:write",
		Description: "Create or change relationships between entities. action=create takes a relationships[] array; " +
			"action=update changes properties or weight; action=delete soft-deletes.",
		Actions: []consolidatedAction{
			{Action: "create", Handler: "relationship-create", Description: "create relationships"},
			{Action: "update", Handler: "relationship-update", Description: "update a relationship"},
			{Action: "delete", Handler: "relationship-delete", Description: "soft-delete a relationship"},
		},
	},
	{
		Name:  "schema-types",
		Scope: "graph:read",
		Description: "Read the type registry every project inherits from its installed schemas. action=version returns " +
			"the current schema version; action=compiled returns the merged effective object/relationship types; " +
			"action=types lists entity types with instance counts.",
		Actions: []consolidatedAction{
			{Action: "version", Handler: "schema-version", Description: "current schema version"},
			{Action: "compiled", Handler: "schema-compiled-types", Description: "effective merged type set"},
			{Action: "types", Handler: "entity-type-list", Description: "entity types with counts"},
		},
	},
	{
		Name:  "schema",
		Scope: "schema:read",
		Description: "Inspect schemas and domain packs. action=owned lists schemas registered by this project; " +
			"action=available lists everything installable with installation status and type counts; action=installed " +
			"lists what is active; action=packs lists installed domain packs with their classification keywords; " +
			"action=get returns one schema; action=history lists installation history; action=icons lists valid icon " +
			"names; action=classify-document matches a document against installed packs (read-only); " +
			"action=pack-claims lists the packs a catalogue manifest claims.",
		Actions: []consolidatedAction{
			{Action: "owned", Handler: "schema-list", Description: "schemas owned by the project"},
			{Action: "available", Handler: "schema-list-available", Description: "available schemas with install status"},
			{Action: "installed", Handler: "schema-list-installed", Description: "installed schemas"},
			{Action: "packs", Handler: "list-installed-schemas", Description: "installed domain packs"},
			{Action: "get", Handler: "schema-get", Description: "one schema's definitions"},
			{Action: "history", Handler: "schema-history", Description: "installation history"},
			{Action: "icons", Handler: "schema-icon-list", Description: "valid UI icon names"},
			{Action: "classify-document", Handler: "classify-document", Description: "classify a document against packs"},
			{Action: "pack-claims", Handler: "schema-pack-claims", Description: "packs claimed by catalogue manifests"},
		},
	},
	{
		Name:  "schema-write",
		Scope: "schema:write",
		Description: "Install, change or remove schemas and domain packs. action=assign installs a schema; " +
			"action=update-assignment toggles an assignment active; action=uninstall removes one; action=create and " +
			"action=delete manage project-owned schemas; action=finalize-discovery creates or extends a pack from a " +
			"discovery job; action=queue-reextraction re-extracts a document with a pack.",
		Actions: []consolidatedAction{
			{Action: "assign", Handler: "schema-assign", Description: "install a schema"},
			{Action: "update-assignment", Handler: "schema-assignment-update", Description: "toggle an assignment"},
			{Action: "uninstall", Handler: "schema-uninstall", Description: "uninstall a schema"},
			{Action: "create", Handler: "schema-create", Description: "create a project schema"},
			{Action: "delete", Handler: "schema-delete", Description: "delete a project schema"},
			{Action: "finalize-discovery", Handler: "finalize-discovery", Description: "create or extend a pack"},
			{Action: "queue-reextraction", Handler: "queue-reextraction", Description: "re-extract a document"},
		},
	},
	{
		Name:  "schema-migrate",
		Scope: "schema:migrate",
		Description: "Migrate objects between schema versions. action=preview dry-runs and reports per-type risk " +
			"(accepts either from_schema_id/to_schema_id or from_version/to_version); action=execute applies the " +
			"migration; action=rollback restores archived property data; action=commit discards archives; " +
			"action=status polls an async migration job; action=archives lists objects with recoverable data; " +
			"action=archive-get returns one object's archive.",
		Actions: []consolidatedAction{
			{Action: "preview", Handler: "schema-migrate-preview", Fold: []string{"schema-migration-preview"}, Description: "dry-run with per-type risk"},
			{Action: "execute", Handler: "schema-migrate-execute", Description: "apply the migration"},
			{Action: "rollback", Handler: "schema-migrate-rollback", Description: "restore archived properties"},
			{Action: "commit", Handler: "schema-migrate-commit", Description: "discard migration archives"},
			{Action: "status", Handler: "schema-migration-job-status", Description: "poll a migration job"},
			{Action: "archives", Handler: "migration-archive-list", Description: "objects with recoverable data"},
			{Action: "archive-get", Handler: "migration-archive-get", Description: "one object's archive"},
		},
	},
	{
		Name:  "blueprint",
		Scope: "schema:read",
		Description: "Inspect blueprints (schema packs + agent definitions + skills + seeds). action=list lists visible " +
			"blueprints; action=get returns one manifest; action=versions lists a blueprint's versions; action=applied " +
			"lists what this project applied; action=apply-preview reports collisions without materialising anything; " +
			"action=updates-status reports the last upstream-update check for an applied blueprint; action=import-preview " +
			"fetches and validates a blueprint without applying it; action=model-overrides returns the stored model overrides.",
		Actions: []consolidatedAction{
			{Action: "list", Handler: "blueprint-list", Description: "list visible blueprints"},
			{Action: "get", Handler: "blueprint-get", Description: "one blueprint manifest"},
			{Action: "versions", Handler: "blueprint-versions", Description: "versions of a blueprint"},
			{Action: "applied", Handler: "blueprint-list-applied", Description: "blueprints applied to this project"},
			{Action: "apply-preview", Handler: "blueprint-apply-preview", Description: "dry-run apply, reports collisions"},
			{Action: "updates-status", Handler: "blueprint-updates-status", Description: "latest upstream-update check for an applied blueprint"},
			{Action: "import-preview", Handler: "blueprint-import-preview", Description: "fetch and validate a blueprint without applying it"},
			{Action: "model-overrides", Handler: "blueprint-model-overrides-get", Description: "stored model overrides for a blueprint"},
		},
	},
	{
		Name:  "blueprint-write",
		Scope: "schema:write",
		Description: "Author, apply, import and retire blueprints. action=create starts a draft; action=update edits a draft; " +
			"action=publish publishes a private draft; action=new-version forks a version; action=apply materialises a " +
			"manifest (call blueprint action=apply-preview first when object types collide, then merge_types=true); " +
			"action=unapply reverses an apply; action=import fetches a blueprint from GitHub (optionally applying it); " +
			"action=delete removes a project-private blueprint; action=deprecate marks a published version deprecated; " +
			"action=updates-check enqueues an upstream check; action=updates-apply enqueues an upstream apply; " +
			"action=model-overrides-put sets per-project model overrides; action=github-credentials stores the project GitHub PAT.",
		Actions: []consolidatedAction{
			{Action: "create", Handler: "blueprint-create", Description: "create a draft"},
			{Action: "update", Handler: "blueprint-update", Description: "edit a draft"},
			{Action: "publish", Handler: "blueprint-publish", Description: "publish a draft"},
			{Action: "new-version", Handler: "blueprint-new-version", Description: "fork a new version"},
			{Action: "apply", Handler: "blueprint-apply", Description: "materialise a blueprint"},
			{Action: "unapply", Handler: "blueprint-unapply", Description: "reverse an apply"},
			{Action: "import", Handler: "blueprint-import", Description: "import a blueprint from GitHub"},
			{Action: "delete", Handler: "blueprint-delete", Description: "remove a project-private blueprint"},
			{Action: "deprecate", Handler: "blueprint-deprecate", Description: "mark a published version deprecated"},
			{Action: "updates-check", Handler: "blueprint-updates-check", Description: "enqueue an upstream check"},
			{Action: "updates-apply", Handler: "blueprint-updates-apply", Description: "enqueue an upstream apply"},
			{Action: "model-overrides-put", Handler: "blueprint-model-overrides-put", Description: "set per-project model overrides"},
			{Action: "github-credentials", Handler: "blueprint-github-credentials-put", Description: "store the project GitHub PAT"},
		},
	},
	{
		Name:  "branch",
		Scope: "graph:read",
		Description: "Read graph branches (isolated workspaces for staged changes). action=list lists branches; " +
			"action=get returns one; action=readiness reports whether a branch's objects are fully embedded and safe to merge.",
		Actions: []consolidatedAction{
			{Action: "list", Handler: "graph-branch-list", Description: "list branches"},
			{Action: "get", Handler: "graph-branch-get", Description: "one branch"},
			{Action: "readiness", Handler: "graph-branch-merge-readiness", Description: "merge readiness check"},
		},
	},
	{
		Name:  "branch-write",
		Scope: "graph:write",
		Description: "Create and merge graph branches. action=create adds a branch; action=update renames or describes; " +
			"action=fork copies a branch's objects; action=merge merges into main (dry-run unless execute=true); " +
			"action=delete removes a branch.",
		Actions: []consolidatedAction{
			{Action: "create", Handler: "graph-branch-create", Description: "create a branch"},
			{Action: "update", Handler: "graph-branch-update", Description: "rename or describe a branch"},
			{Action: "fork", Handler: "graph-branch-fork", Description: "fork a branch"},
			{Action: "merge", Handler: "graph-branch-merge", Description: "merge a branch (dry-run by default)"},
			{Action: "delete", Handler: "graph-branch-delete", Description: "delete a branch"},
		},
	},
	{
		Name:  "search",
		Scope: "search",
		Description: "Semantic search over the graph. action=hybrid combines full-text, vector similarity and graph " +
			"context (the best general choice); action=semantic is vector-only and finds conceptually similar entities; " +
			"action=similar finds entities similar to a known entity.",
		Actions: []consolidatedAction{
			{Action: "hybrid", Handler: "search-hybrid", Description: "hybrid full-text + vector + graph search"},
			{Action: "semantic", Handler: "search-semantic", Description: "vector similarity search"},
			{Action: "similar", Handler: "search-similar", Description: "entities similar to a given entity"},
		},
	},
	{
		Name:  "session-todo",
		Scope: "",
		Description: "Read and update the todos attached to an agent session. Session-scoped: ownership is enforced at " +
			"the data-access layer, not by an MCP scope.",
		Actions: []consolidatedAction{
			{Action: "list", Handler: "session-todo-list", Description: "list a session's todos"},
			{Action: "update", Handler: "session-todo-update", Description: "update a todo's status or content"},
		},
	},
	{
		Name:        "document",
		Scope:       "documents:read",
		Description: "Read project documents. action=list lists document metadata; action=get returns one document's metadata.",
		Actions: []consolidatedAction{
			{Action: "list", Handler: "document-list", Description: "list documents"},
			{Action: "get", Handler: "document-get", Description: "one document"},
		},
	},
	{
		Name:  "document-write",
		Scope: "documents:write",
		Description: "Add or remove documents. action=upload takes base64 content (max 10 MB decoded); action=delete " +
			"removes a document and its chunks.",
		Actions: []consolidatedAction{
			{Action: "upload", Handler: "document-upload", Description: "upload a document"},
			{Action: "delete", Handler: "document-delete", Description: "delete a document"},
		},
	},
	{
		Name:        "skill",
		Scope:       "skills:read",
		Description: "Read project skills. action=list lists available skills; action=get returns a skill's full content.",
		Actions: []consolidatedAction{
			{Action: "list", Handler: "skill-list", Description: "list skills"},
			{Action: "get", Handler: "skill-get", Description: "one skill with content"},
		},
	},
	{
		Name:  "skill-write",
		Scope: "skills:write",
		Description: "Create or change project skills. action=create adds a skill; action=update edits its description, " +
			"content or metadata; action=patch replaces one exact, unique substring of the content (the preferred edit " +
			"for learned skills); action=delete removes it.",
		Actions: []consolidatedAction{
			{Action: "create", Handler: "skill-create", Description: "create a skill"},
			{Action: "update", Handler: "skill-update", Description: "update a skill"},
			{Action: "patch", Handler: "skill-patch", Description: "replace one exact substring of the content"},
			{Action: "delete", Handler: "skill-delete", Description: "delete a skill"},
		},
	},
	{
		Name:  "agent-def",
		Scope: "agents:read",
		Description: "Read agent definitions (the configuration templates: system prompt, tools, model, visibility). " +
			"action=list returns definitions; action=get returns one in full; action=available returns the lightweight " +
			"catalogue of what this project can run.",
		Actions: []consolidatedAction{
			{Action: "list", Handler: "agent-def-list", Description: "list definitions"},
			{Action: "get", Handler: "agent-def-get", Description: "one definition"},
			{Action: "available", Handler: "agent-list-available", Description: "available agent catalogue"},
		},
	},
	{
		Name:  "agent-def-write",
		Scope: "agents:write",
		Description: "Create or change agent definitions. action=create adds a definition; action=update does a partial " +
			"update; action=delete removes the template without touching running agents.",
		Actions: []consolidatedAction{
			{Action: "create", Handler: "agent-def-create", Description: "create a definition"},
			{Action: "update", Handler: "update_agent_definition", Description: "update a definition"},
			{Action: "delete", Handler: "agent-def-delete", Description: "delete a definition"},
		},
	},
	{
		Name:  "agent",
		Scope: "agents:read",
		Description: "Read runtime agents (scheduled or triggered instances). action=list lists agents with their " +
			"definition metadata and last run; action=get returns one in full.",
		Actions: []consolidatedAction{
			{Action: "list", Handler: "agent-list", Description: "list runtime agents"},
			{Action: "get", Handler: "agent-get", Description: "one runtime agent"},
		},
	},
	{
		Name:  "agent-write",
		Scope: "agents:write",
		Description: "Create or drive runtime agents. action=create adds an agent with a schedule; action=update changes " +
			"it; action=delete stops and removes it; action=trigger starts a run (the run id is polled with agent-run " +
			"action=status).",
		Actions: []consolidatedAction{
			{Action: "create", Handler: "agent-create", Description: "create a runtime agent"},
			{Action: "update", Handler: "update_agent", Description: "update a runtime agent"},
			{Action: "delete", Handler: "agent-delete", Description: "delete a runtime agent"},
			{Action: "trigger", Handler: "trigger_agent", Description: "start a run"},
		},
	},
	{
		Name:  "agent-run",
		Scope: "agents:read",
		Description: "Inspect agent runs. action=list lists runs; action=get returns one with status and duration; " +
			"action=messages returns the LLM messages by step; action=tool-calls returns tool calls with inputs and " +
			"outputs; action=status is the lightweight poller for a queued run.",
		Actions: []consolidatedAction{
			{Action: "list", Handler: "agent-run-list", Description: "list runs"},
			{Action: "get", Handler: "agent-run-get", Description: "one run"},
			{Action: "messages", Handler: "agent-run-messages", Description: "run messages"},
			{Action: "tool-calls", Handler: "agent-run-tool-calls", Description: "run tool calls"},
			{Action: "status", Handler: "agent-run-status", Description: "poll run status"},
		},
	},
	{
		Name:  "agent-question",
		Scope: "agents:read",
		Description: "List questions an agent asked and is waiting on. Pass run_id to scope to one run; omit it to list " +
			"questions across the project (optionally filtered by status).",
		Actions: []consolidatedAction{
			{
				Action:      "list",
				Handler:     "agent-question-list-project",
				Fold:        []string{"agent-question-list"},
				Description: "list questions for a run or the whole project",
				Pick: func(args map[string]any) string {
					if s, _ := args["run_id"].(string); strings.TrimSpace(s) != "" {
						return "agent-question-list"
					}
					return "agent-question-list-project"
				},
			},
		},
	},
	{
		Name:  "agent-hook-write",
		Scope: "agents:write",
		Description: "Manage webhook hooks that let external systems trigger an agent. action=create returns the hook id " +
			"and a one-time token; action=delete removes it.",
		Actions: []consolidatedAction{
			{Action: "create", Handler: "agent-hook-create", Description: "create a webhook hook"},
			{Action: "delete", Handler: "agent-hook-delete", Description: "delete a webhook hook"},
		},
	},
	{
		Name:  "adk-session",
		Scope: "agents:read",
		Description: "Read raw ADK sessions (the agent runtime's own session store). action=list lists sessions; " +
			"action=get returns one with all events.",
		Actions: []consolidatedAction{
			{Action: "list", Handler: "adk-session-list", Description: "list ADK sessions"},
			{Action: "get", Handler: "adk-session-get", Description: "one ADK session with events"},
		},
	},
	{
		Name:  "project-write",
		Scope: "projects:write",
		Description: "Create or change the project itself. action=update writes the project info document (purpose, " +
			"goals, audience) or clears it with an empty content; action=create provisions a new project.",
		Actions: []consolidatedAction{
			{Action: "update", Handler: "project-update", Description: "write or clear the project info document"},
			{Action: "create", Handler: "project-create", Description: "provision a new project"},
		},
	},
	{
		Name:  "token",
		Scope: "projects:write",
		Description: "Inspect project API tokens. action=list lists the project's tokens (metadata only, never the " +
			"secret); action=get returns one token's metadata and last-used time.",
		Actions: []consolidatedAction{
			{Action: "list", Handler: "token-list", Description: "list project tokens"},
			{Action: "get", Handler: "token-get", Description: "one token's metadata"},
		},
	},
	{
		Name:  "token-write",
		Scope: "projects:write",
		Description: "Manage project API tokens. action=create mints a token and returns the secret exactly once; " +
			"action=revoke revokes it immediately.",
		Actions: []consolidatedAction{
			{Action: "create", Handler: "token-create", Description: "mint a token (secret shown once)"},
			{Action: "revoke", Handler: "token-revoke", Description: "revoke a token"},
		},
	},
}

// familyByName indexes consolidatedFamilies by tool name.
var familyByName = func() map[string]*consolidatedFamily {
	m := make(map[string]*consolidatedFamily, len(consolidatedFamilies))
	for i := range consolidatedFamilies {
		m[consolidatedFamilies[i].Name] = &consolidatedFamilies[i]
	}
	return m
}()

// toolAliases maps every deprecated legacy tool name to its consolidated tool
// and implied action. Legacy names stay callable: dispatchTool resolves the
// (tool, action) pair back to the per-verb case (legacyDispatchName), and the
// authority checks canonicalise the name (canonicalToolName) so a saved agent
// definition or share allowlist entry that names `entity-search` keeps working
// and keeps its scope.
var toolAliases = func() map[string]toolAlias {
	m := make(map[string]toolAlias)
	for _, fam := range consolidatedFamilies {
		for _, act := range fam.Actions {
			m[act.Handler] = toolAlias{Tool: fam.Name, Action: act.Action}
			for _, fold := range act.Fold {
				m[fold] = toolAlias{Tool: fam.Name, Action: act.Action}
			}
		}
	}
	return m
}()

// CanonicalToolName returns the consolidated tool name that replaced a
// deprecated per-verb name, or the name unchanged when it is not an alias.
// Exported for callers (and parity tests) that reason about the catalog by name.
func CanonicalToolName(name string) string { return canonicalToolName(name) }

// canonicalToolName returns the consolidated name for a legacy alias, and the
// name unchanged when it is not an alias.
func canonicalToolName(name string) string {
	if a, ok := toolAliases[name]; ok {
		return a.Tool
	}
	return name
}

// actionArg reads the action discriminator. Only `action` is a discriminator:
// legacy tools carry their own `view` and `strategy` parameters, so treating
// those as synonyms would misroute a legacy call that sets one of them.
func actionArg(args map[string]any) (string, bool) {
	if v, ok := args["action"].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v), true
	}
	return "", false
}

// resolveConsolidatedHandler maps a consolidated call to the legacy dispatch
// case that implements it. ok is false when the name is not a consolidated
// tool, in which case dispatch proceeds unchanged.
func resolveConsolidatedHandler(name string, args map[string]any) (string, bool, error) {
	fam, ok := familyByName[name]
	if !ok {
		return name, false, nil
	}
	action, _ := actionArg(args)
	if action == "" {
		return "", true, fmt.Errorf("%s: 'action' is required (one of: %s)", name, familyActionList(fam))
	}
	for _, act := range fam.Actions {
		if act.Action != action {
			continue
		}
		handler := act.Handler
		if act.Pick != nil {
			handler = act.Pick(args)
		}
		return handler, true, nil
	}
	return "", true, fmt.Errorf("%s: unknown action %q (one of: %s)", name, action, familyActionList(fam))
}

func familyActionList(fam *consolidatedFamily) string {
	out := make([]string, 0, len(fam.Actions))
	for _, act := range fam.Actions {
		out = append(out, act.Action)
	}
	return strings.Join(out, ", ")
}

// legacyDispatchName returns the per-verb name a call resolves to, so shared
// surfaces that key on tool names keep their pre-consolidation granularity.
// It returns the name unchanged when the call is not a consolidated tool.
func legacyDispatchName(name string, args map[string]any) string {
	// A deprecated per-verb name carries its action implicitly. It cannot also be
	// a family name (the catalog shape test enforces that), so a per-verb name
	// always identifies exactly one handler and the caller's own arguments can
	// never be mistaken for an action choice.
	if alias, ok := toolAliases[name]; ok {
		if handler, ok, err := resolveConsolidatedHandler(alias.Tool, map[string]any{"action": alias.Action}); err == nil && ok {
			return handler
		}
	}
	if handler, ok, err := resolveConsolidatedHandler(name, args); err == nil && ok {
		return handler
	}
	return name
}

// consolidateToolDefinitions replaces the legacy per-verb tool definitions with
// the consolidated catalog, leaving every unrelated tool (operator tools, relay
// tools) untouched. A family is emitted only when at least one of its legacy
// members is present, so a Service assembled without a handler does not
// advertise tools it cannot serve.
func consolidateToolDefinitions(legacy []ToolDefinition) []ToolDefinition {
	if len(legacy) == 0 {
		return legacy
	}
	byName := make(map[string]*ToolDefinition, len(legacy))
	for i := range legacy {
		byName[legacy[i].Name] = &legacy[i]
	}
	familyOf := make(map[string]int, len(toolAliases))
	for i := range consolidatedFamilies {
		for _, act := range consolidatedFamilies[i].Actions {
			familyOf[act.Handler] = i
			for _, fold := range act.Fold {
				familyOf[fold] = i
			}
		}
	}

	out := make([]ToolDefinition, 0, len(legacy))
	emitted := make(map[string]bool, len(consolidatedFamilies))
	// Families encountered in the legacy order keep their position, so the
	// catalog reads roughly as before.
	for i := range legacy {
		fi, ok := familyOf[legacy[i].Name]
		if !ok {
			out = append(out, legacy[i])
			continue
		}
		fam := &consolidatedFamilies[fi]
		if emitted[fam.Name] {
			continue
		}
		if def, ok := buildConsolidatedDefinition(fam, byName); ok {
			out = append(out, def)
			emitted[fam.Name] = true
		}
	}
	return out
}

// buildConsolidatedDefinition derives one consolidated ToolDefinition from the
// legacy definitions it replaces: the parameter schema is the union of theirs,
// so no parameter is lost, plus the action discriminator.
func buildConsolidatedDefinition(fam *consolidatedFamily, byName map[string]*ToolDefinition) (ToolDefinition, bool) {
	props := make(map[string]PropertySchema)
	actions := make([]string, 0, len(fam.Actions))
	help := make([]string, 0, len(fam.Actions))
	configKeys := make([]string, 0, 2)
	// A consolidated tool declares a structured output schema only when every
	// action it exposes produces structured content. A family that mixes
	// envelope and prose actions must not advertise an outputSchema: MCP clients
	// treat a declared schema as a promise about structuredContent.
	allHaveOutput := true
	found := false
	var outputSchema *InputSchema
	// Authority is the strictest of the members: folding an operator-only or
	// agent-only tool into a family must never widen who can call it.
	superadminOnly := false
	agentOnly := false

	for _, act := range fam.Actions {
		actions = append(actions, act.Action)
		help = append(help, act.Action+" — "+act.Description)
		for _, name := range append([]string{act.Handler}, act.Fold...) {
			src := byName[name]
			if src == nil {
				continue
			}
			found = true
			if src.SuperadminOnly {
				superadminOnly = true
			}
			if src.AgentOnly {
				agentOnly = true
			}
			if src.OutputSchema == nil {
				allHaveOutput = false
			} else if outputSchema == nil {
				// Keep the first action's structured schema (the envelope shape)
				// instead of inventing a generic one.
				outputSchema = src.OutputSchema
			}
			for k, v := range src.InputSchema.Properties {
				// `action` is the discriminator the family adds; every other
				// parameter a member declared is a real parameter and is kept.
				if k == "action" {
					continue
				}
				if existing, ok := props[k]; !ok || existing.Type == "" {
					props[k] = v
				}
			}
			for _, key := range src.ConfigKeys {
				duplicate := false
				for _, seen := range configKeys {
					if seen == key {
						duplicate = true
						break
					}
				}
				if !duplicate {
					configKeys = append(configKeys, key)
				}
			}
		}
	}
	if !found {
		return ToolDefinition{}, false
	}

	if !allHaveOutput {
		outputSchema = nil
	}

	props["action"] = PropertySchema{
		Type:        "string",
		Enum:        actions,
		Description: "Operation to perform. " + strings.Join(help, "; "),
	}
	return ToolDefinition{
		Name:           fam.Name,
		Description:    fam.Description,
		OutputSchema:   outputSchema,
		ConfigKeys:     configKeys,
		RequiredScope:  fam.Scope,
		SuperadminOnly: superadminOnly,
		AgentOnly:      agentOnly,
		InputSchema: InputSchema{
			Type:       "object",
			Properties: props,
			Required:   []string{"action"},
		},
	}, true
}
