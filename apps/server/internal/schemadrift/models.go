// Package schemadrift guards the bun model structs (their `bun` column tags and
// Go types) against the real PostgreSQL schema produced by the embedded
// migrations. It exists because models have silently drifted from the migrated
// schema three times this cycle (#946, #956), each a runtime Scan failure with
// no CI signal.
//
// The guard is deliberately exhaustive: every exported bun model in the tree is
// registered here and reflected over, and a static census (census.go) parses the
// source to prove the registry is complete. A model that is added without being
// registered here, or registered here without a matching table, fails the test.
//
// Three model structs are unexported (auth.introspectionCacheEntry,
// extraction.embeddingCacheRow, provider.budgetNotification) and cannot be
// referenced from this package; their tables are covered at name level by the
// census instead (see drift_test.go). Every other model is reflected here.
package schemadrift

import (
	"github.com/emergent-company/emergent.memory/domain/agents"
	"github.com/emergent-company/emergent.memory/domain/apitoken"
	"github.com/emergent-company/emergent.memory/domain/backups"
	"github.com/emergent-company/emergent.memory/domain/blueprints"
	"github.com/emergent-company/emergent.memory/domain/branches"
	"github.com/emergent-company/emergent.memory/domain/chat"
	"github.com/emergent-company/emergent.memory/domain/chunks"
	"github.com/emergent-company/emergent.memory/domain/discoveryjobs"
	"github.com/emergent-company/emergent.memory/domain/documents"
	"github.com/emergent-company/emergent.memory/domain/email"
	"github.com/emergent-company/emergent.memory/domain/embeddingpolicies"
	"github.com/emergent-company/emergent.memory/domain/extraction"
	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/domain/invites"
	"github.com/emergent-company/emergent.memory/domain/journal"
	"github.com/emergent-company/emergent.memory/domain/mcp"
	"github.com/emergent-company/emergent.memory/domain/mcpregistry"
	"github.com/emergent-company/emergent.memory/domain/modelconfig"
	"github.com/emergent-company/emergent.memory/domain/monitoring"
	"github.com/emergent-company/emergent.memory/domain/notifications"
	"github.com/emergent-company/emergent.memory/domain/orgs"
	"github.com/emergent-company/emergent.memory/domain/projects"
	"github.com/emergent-company/emergent.memory/domain/provider"
	"github.com/emergent-company/emergent.memory/domain/sandbox"
	"github.com/emergent-company/emergent.memory/domain/sandboximages"
	"github.com/emergent-company/emergent.memory/domain/scheduler"
	"github.com/emergent-company/emergent.memory/domain/schemaregistry"
	"github.com/emergent-company/emergent.memory/domain/schemas"
	"github.com/emergent-company/emergent.memory/domain/search"
	"github.com/emergent-company/emergent.memory/domain/sessiontodos"
	"github.com/emergent-company/emergent.memory/domain/skills"
	"github.com/emergent-company/emergent.memory/domain/superadmin"
	"github.com/emergent-company/emergent.memory/domain/tasks"
	"github.com/emergent-company/emergent.memory/domain/useractivity"
	"github.com/emergent-company/emergent.memory/domain/userprofile"
	"github.com/emergent-company/emergent.memory/domain/users"
	"github.com/emergent-company/emergent.memory/pkg/adk/session/bunsession"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// models is the exhaustive registry of every exported bun model. Each entry is
// a typed nil pointer, so reflect.TypeOf yields the model's type without
// allocating a value. modelsTableFor (drift.go) turns each into a table+columns
// via bun's own dialect.
//
// The census (census.go) parses the source tree for every struct carrying a
// `bun:"table:..."` tag and asserts this registry covers all of them except the
// explicitly excluded unexported ones, so a new model cannot be forgotten here.
var models = []any{
	// pkg/auth, pkg/adk/session/bunsession
	(*auth.UserProfile)(nil),
	(*bunsession.ADKSession)(nil),
	(*bunsession.ADKEvent)(nil),
	(*bunsession.ADKState)(nil),

	// domain/scheduler
	(*scheduler.DatabaseBackup)(nil),

	// domain/email
	(*email.EmailJob)(nil),

	// domain/mcp
	(*mcp.AgentMCPSession)(nil),
	(*mcp.AgentMCPSessionDetail)(nil),
	(*mcp.AgentMCPKey)(nil),
	(*mcp.AgentMCPKeyDetail)(nil),
	(*mcp.AgentMCPEndpoint)(nil),
	(*mcp.MCPShareInstance)(nil),

	// domain/journal
	(*journal.JournalEntry)(nil),
	(*journal.JournalNote)(nil),

	// domain/search
	(*search.RetrievalTrace)(nil),

	// domain/graph
	(*graph.GraphObject)(nil),
	(*graph.GraphRelationship)(nil),
	(*graph.Branch)(nil),
	(*graph.BranchLineage)(nil),

	// domain/apitoken
	(*apitoken.ApiToken)(nil),

	// domain/modelconfig
	(*modelconfig.ProjectModelConfig)(nil),
	(*modelconfig.OrgModelConfig)(nil),

	// domain/sandboximages
	(*sandboximages.SandboxImage)(nil),

	// domain/notifications
	(*notifications.Notification)(nil),

	// domain/invites
	(*invites.Invite)(nil),

	// domain/useractivity
	(*useractivity.UserRecentItem)(nil),

	// domain/schemaregistry
	(*schemaregistry.ProjectObjectSchemaRegistry)(nil),
	(*schemaregistry.ProjectObjectSchemaRegistryVersion)(nil),
	(*schemaregistry.GraphMemorySchema)(nil),
	(*schemaregistry.ProjectMemorySchema)(nil),
	(*schemaregistry.GraphObject)(nil),
	(*schemaregistry.Project)(nil),

	// domain/provider
	(*provider.OrgProviderConfig)(nil),
	(*provider.ProjectProviderConfig)(nil),
	(*provider.ProviderSupportedModel)(nil),
	(*provider.LLMUsageEvent)(nil),
	(*provider.ProviderPricing)(nil),
	(*provider.OrganizationCustomPricing)(nil),
	(*provider.ProjectCustomPricing)(nil),

	// domain/users
	(*users.UserProfile)(nil),
	(*users.UserEmail)(nil),

	// domain/mcpregistry
	(*mcpregistry.MCPServer)(nil),
	(*mcpregistry.MCPServerTool)(nil),

	// domain/backups
	(*backups.DatabaseBackup)(nil),
	(*backups.Backup)(nil),
	(*backups.Restore)(nil),

	// domain/projects
	(*projects.Project)(nil),
	(*projects.ProjectMembership)(nil),

	// domain/superadmin
	(*superadmin.Superadmin)(nil),
	(*superadmin.UserProfile)(nil),
	(*superadmin.UserEmail)(nil),
	(*superadmin.Org)(nil),
	(*superadmin.Project)(nil),
	(*superadmin.ProjectMembership)(nil),
	(*superadmin.OrganizationMembership)(nil),
	(*superadmin.EmailJob)(nil),
	(*superadmin.GraphEmbeddingJob)(nil),
	(*superadmin.ChunkEmbeddingJob)(nil),
	(*superadmin.ObjectExtractionJob)(nil),
	(*superadmin.DocumentParsingJob)(nil),

	// domain/chunks
	(*chunks.Chunk)(nil),

	// domain/blueprints
	(*blueprints.Blueprint)(nil),
	(*blueprints.BlueprintApplication)(nil),
	(*blueprints.BlueprintPackClaim)(nil),

	// domain/skills
	(*skills.Skill)(nil),

	// domain/documents
	(*documents.Document)(nil),

	// domain/tasks
	(*tasks.Task)(nil),

	// domain/orgs
	(*orgs.Org)(nil),
	(*orgs.OrganizationMembership)(nil),
	(*orgs.OrgToolSetting)(nil),

	// domain/chat
	(*chat.Conversation)(nil),
	(*chat.Message)(nil),

	// domain/sessiontodos
	(*sessiontodos.SessionTodo)(nil),

	// domain/schemas
	(*schemas.SchemaMigrationJob)(nil),
	(*schemas.GraphMemorySchema)(nil),
	(*schemas.ProjectMemorySchema)(nil),

	// domain/monitoring
	(*monitoring.SystemProcessLog)(nil),
	(*monitoring.LLMCallLog)(nil),

	// domain/agents
	(*agents.ProjectSetting)(nil),
	(*agents.AgentWebhookHook)(nil),
	(*agents.Agent)(nil),
	(*agents.AgentRun)(nil),
	(*agents.AgentProcessingLog)(nil),
	(*agents.AgentDefinition)(nil),
	(*agents.AgentRunMessage)(nil),
	(*agents.AgentRunToolCall)(nil),
	(*agents.AgentQuestion)(nil),
	(*agents.AgentToolApproval)(nil),
	(*agents.AgentRunJob)(nil),
	(*agents.ACPSession)(nil),
	(*agents.ACPRunEvent)(nil),
	(*agents.AgentShareLink)(nil),
	(*agents.AgentShareSession)(nil),
	(*agents.AgentShareEndUser)(nil),
	(*agents.AgentShareUsage)(nil),

	// domain/branches
	(*branches.Branch)(nil),

	// domain/userprofile
	(*userprofile.Profile)(nil),

	// domain/sandbox
	(*sandbox.AgentSandbox)(nil),

	// domain/extraction
	(*extraction.DocumentParsingJob)(nil),
	(*extraction.ChunkEmbeddingJob)(nil),
	(*extraction.GraphEmbeddingJob)(nil),
	(*extraction.ObjectExtractionJob)(nil),
	(*extraction.ObjectExtractionLog)(nil),
	(*extraction.GraphRelationshipEmbeddingJob)(nil),
	(*extraction.GraphMemorySchema)(nil),
	(*extraction.ProjectMemorySchema)(nil),

	// domain/embeddingpolicies
	(*embeddingpolicies.EmbeddingPolicy)(nil),

	// domain/discoveryjobs
	(*discoveryjobs.DiscoveryJob)(nil),
	(*discoveryjobs.DiscoveryTypeCandidate)(nil),
}
