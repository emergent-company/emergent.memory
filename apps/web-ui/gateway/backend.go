package main

import (
	"context"
	"io"
	"time"
)

// MemoryBackend abstracts the memory REST API so handlers and the supervisor
// can be unit-tested with a fake. MemoryClient is the HTTP implementation.
type MemoryBackend interface {
	ListAgentDefinitions(ctx context.Context) ([]AgentDefinitionSummary, error)
	GetAgentDefinition(ctx context.Context, id string) (*AgentDefinition, error)
	CreateAgentDefinition(ctx context.Context, in *AgentDefinition) (*AgentDefinition, error)
	UpdateAgentDefinition(ctx context.Context, id string, in *AgentDefinition) (*AgentDefinition, error)
	DeleteAgentDefinition(ctx context.Context, id string) error
	ListScheduledAgents(ctx context.Context) ([]ScheduledAgent, error)
	GetScheduledAgent(ctx context.Context, id string) (*ScheduledAgent, error)
	CreateScheduledAgent(ctx context.Context, in *ScheduledAgent) (*ScheduledAgent, error)
	UpdateScheduledAgent(ctx context.Context, id string, in *ScheduledAgent) (*ScheduledAgent, error)
	EnableScheduledAgent(ctx context.Context, id string) (*ScheduledAgent, error)
	SetScheduledAgentEnabled(ctx context.Context, id string, enabled bool) (*ScheduledAgent, error)
	DeleteScheduledAgent(ctx context.Context, id string) error
	TriggerScheduledAgent(ctx context.Context, id string) (*TriggerResult, error)
	TriggerAgent(ctx context.Context, agentID, prompt string, ctxValues map[string]string) (*TriggerResult, error)
	ListScheduledAgentRuns(ctx context.Context, id string) ([]ScheduledAgentRun, error)
	GetRunFull(ctx context.Context, runID string) (*AgentRunFull, error)
	GetRunQuestions(ctx context.Context, runID string) ([]AgentQuestionItem, error)
	GetAgentRun(ctx context.Context, runID string) (*AgentRun, error)
	GetAgentSandboxConfig(ctx context.Context, id string) (*AgentSandboxConfig, error)
	SetAgentSandboxConfig(ctx context.Context, id string, cfg *AgentSandboxConfig) (*AgentSandboxConfig, error)
	ListSandboxProviders(ctx context.Context) ([]SandboxProvider, error)
	ListSandboxImages(ctx context.Context) ([]SandboxImage, error)
	ChatStream(ctx context.Context, req ChatRequest) (io.ReadCloser, error)
	RespondQuestion(ctx context.Context, questionID, response, message string) (*RespondQuestionResult, error)
	CancelQuestion(ctx context.Context, questionID string) (*RespondQuestionResult, error)
	ListAgentQuestions(ctx context.Context) ([]AgentQuestionItem, error)
	ListToolApprovals(ctx context.Context) ([]ToolApprovalItem, error)
	ListConversations(ctx context.Context) (*ConversationList, error)
	CreateObjectConversation(ctx context.Context, canonicalID, title, message string) (string, error)
	GetConversation(ctx context.Context, id string) (*ConversationDetail, error)
	GetConversationHistory(ctx context.Context, id string) (*ConversationHistory, error)
	ListMCPServers(ctx context.Context) ([]MCPServer, error)
	GetMCPServer(ctx context.Context, id string) (*MCPServer, error)
	CreateMCPServer(ctx context.Context, in *MCPServer) (*MCPServer, error)
	UpdateMCPServer(ctx context.Context, id string, in *MCPServer) (*MCPServer, error)
	DeleteMCPServer(ctx context.Context, id string) error
	SyncMCPServer(ctx context.Context, id string) error
	InspectMCPServer(ctx context.Context, id string) (*MCPInspectResult, error)
	ListMCPServerTools(ctx context.Context, id string) ([]MCPTool, error)
	SetMCPServerToolEnabled(ctx context.Context, id string, toolID string, enabled bool) error
	// MCP share instances (project-scoped MCP exposure; see mcp_shares.go).
	// The raw key is only ever present on Create/Rotate results.
	ListMCPShareInstances(ctx context.Context) ([]MCPShareInstance, error)
	CreateMCPShareInstance(ctx context.Context, in *MCPShareInput) (*MCPShareCreated, error)
	GetMCPShareInstance(ctx context.Context, id string) (*MCPShareInstance, error)
	UpdateMCPShareInstance(ctx context.Context, id string, in *MCPShareInput) (*MCPShareInstance, error)
	RevokeMCPShareInstance(ctx context.Context, id string) error
	RotateMCPShareInstance(ctx context.Context, id string) (*MCPShareCreated, error)
	ListMCPShareTools(ctx context.Context) ([]MCPShareTool, error)
	// Per-agent MCP shares (a single agent exposed as one MCP tool; see
	// agent_mcp_shares.go). The raw key is only ever present on Create/Rotate
	// results.
	ListAgentMCPShares(ctx context.Context, agentID string) ([]AgentMCPShare, error)
	ListProjectAgentMCPShares(ctx context.Context) ([]AgentMCPShare, error)
	CreateAgentMCPShare(ctx context.Context, agentID string, in *AgentMCPShareInput) (*AgentMCPShareCreated, error)
	RevokeAgentMCPShare(ctx context.Context, id string) error
	RotateAgentMCPShare(ctx context.Context, id string) (*AgentMCPShareCreated, error)
	// MCP relay (external nodes). Unlike the admin registry routes above these
	// are project-scoped read endpoints that return PLAIN JSON (no
	// successEnvelope wrapper). Sessions are in-memory on the backend: a node
	// present in ListRelaySessions is currently connected.
	ListRelaySessions(ctx context.Context) ([]RelaySession, error)
	GetRelaySessionTools(ctx context.Context, instanceID string) ([]RelayTool, error)
	ListModels(ctx context.Context) ([]Model, error)
	SearchMemories(ctx context.Context, query string) ([]Memory, error)
	ListMemories(ctx context.Context) ([]Memory, error)
	ListDocuments(ctx context.Context, cursor string) ([]Document, string, error)
	GetDocument(ctx context.Context, id string) (*Document, error)
	DeleteDocument(ctx context.Context, id string) error
	ListChunks(ctx context.Context, documentID string) ([]Chunk, error)
	UploadDocument(ctx context.Context, filename string, r io.Reader) (UploadResult, error)
	CreateExtractionJob(ctx context.Context, documentID string) (string, error)
	GetExtractionSummary(ctx context.Context, documentID string) (*ExtractionSummary, error)
	GetGraphObject(ctx context.Context, objectID string) (*GraphObject, error)
	ListGraphObjects(ctx context.Context, branchID, typeFilter string, ids []string) ([]GraphObject, error)
	ListGraphRelationships(ctx context.Context, branchID string) ([]GraphRelationship, error)
	GetObjectEdges(ctx context.Context, objectID string) ([]GraphRelationship, error)
	GetSimilarObjects(ctx context.Context, objectID string, limit int) ([]SimilarObject, error)
	UpdateObject(ctx context.Context, id string, req *UpdateObjectRequest) (*GraphObject, error)
	CreateObject(ctx context.Context, req *CreateObjectRequest) (*GraphObject, error)
	CreateRelationship(ctx context.Context, req *CreateRelationshipRequest) error
	SearchObjectsFTS(ctx context.Context, query, typeFilter string) ([]GraphObject, error)
	ListBranches(ctx context.Context) ([]Branch, error)
	GetCompiledTypes(ctx context.Context) (*CompiledSchemaTypes, error)
	ListAllSchemas(ctx context.Context) ([]SchemaInfo, error)
	GetSchema(ctx context.Context, packID string) (*BlueprintSchema, error)
	CreateSchemaPack(ctx context.Context, req *SchemaPackWriteRequest) (*BlueprintSchema, error)
	UpdateSchemaPack(ctx context.Context, packID string, edit ObjectTypeEdit) error
	AssignSchemaPack(ctx context.Context, schemaID string) error
	ListBlueprintVersions(ctx context.Context, name string) ([]BlueprintRecord, error)
	ListBlueprints(ctx context.Context) ([]BlueprintRecord, error)
	CreateBlueprint(ctx context.Context, req *createBlueprintRequest) (*BlueprintRecord, error)
	GetBlueprint(ctx context.Context, id string) (*BlueprintRecord, error)
	PublishBlueprint(ctx context.Context, id string) error
	ApplyBlueprint(ctx context.Context, id string) (*BlueprintApplyResult, error)
	ListAppliedBlueprints(ctx context.Context) ([]AppliedBlueprint, error)
	UnapplyBlueprint(ctx context.Context, id string) (*BlueprintUnapplyResult, error)
	CreateBlueprintVersion(ctx context.Context, id string, req *CreateBlueprintVersionRequest) (*BlueprintRecord, error)
	UpdateBlueprint(ctx context.Context, id string, req *UpdateBlueprintRequest) (*BlueprintRecord, error)
	GetSchemaHistory(ctx context.Context) ([]SchemaHistoryItem, error)
	PreviewMigration(ctx context.Context, fromSchemaID, toSchemaID string) (*MigrationPreviewResult, error)
	ExecuteMigration(ctx context.Context, fromSchemaID, toSchemaID string, force bool) (*MigrationExecuteResult, error)
	RollbackMigration(ctx context.Context, toVersion string) (*MigrationRollbackResult, error)
	CommitMigrationArchive(ctx context.Context, throughVersion string) (*MigrationCommitResult, error)
	GetMigrationJobStatus(ctx context.Context, jobID string) (*MigrationJob, error)
	ValidateSchemas(ctx context.Context) (*SchemaValidationResult, error)
	ListSkills(ctx context.Context) ([]Skill, error)
	GetSkill(ctx context.Context, id string) (*Skill, error)
	CreateSkill(ctx context.Context, name, description, content string) (*Skill, error)
	UpdateSkill(ctx context.Context, id, description, content string) (*Skill, error)
	DeleteSkill(ctx context.Context, id string) error
	ListBackups(ctx context.Context, orgID string) ([]Backup, int, error)
	GetBackup(ctx context.Context, orgID, backupID string) (*Backup, error)
	CreateBackup(ctx context.Context, includeDeleted, includeChat bool, retentionDays int) (*Backup, error)
	DownloadBackup(ctx context.Context, orgID, backupID string) (string, error)
	DeleteBackup(ctx context.Context, orgID, backupID string) error
	GetCurrentProject(ctx context.Context) (*Project, error)
	UpdateProject(ctx context.Context, id string, in *ProjectUpdate) (*Project, error)
	ListAgentOverrides(ctx context.Context) ([]AgentOverrideEntry, error)
	SetAgentOverride(ctx context.Context, agentName string, in *AgentOverrideInput) error
	DeleteAgentOverride(ctx context.Context, agentName string) error
	GetProjectSetting(ctx context.Context, category, key string) (*ProjectSetting, error)
	SetProjectSetting(ctx context.Context, category, key string, value map[string]any) error
	DeleteProjectSetting(ctx context.Context, category, key string) error
	GetProjectUsageSummary(ctx context.Context, since, until time.Time) (*UsageSummaryResponse, error)
	GetProjectUsageTimeSeries(ctx context.Context, granularity string, since, until time.Time) (*UsageTimeSeriesResponse, error)
	ListProjectProviders(ctx context.Context) ([]ProjectProviderConfig, error)
	ListProviderModels(ctx context.Context, provider string) ([]ProviderSupportedModel, error)
	ListPricing(ctx context.Context) ([]ProviderPricing, error)
	ListProjectPricingOverrides(ctx context.Context) ([]ProjectCustomPricing, error)
	UpsertProjectPricingOverride(ctx context.Context, provider, model string, rates modelPriceRates) (*ProjectCustomPricing, error)
	DeleteProjectPricingOverride(ctx context.Context, provider, model string) error
	UpsertProjectProviderConfig(ctx context.Context, provider string, in ProviderConfigInput) (*ProjectProviderConfig, error)
	DeleteProjectProviderConfig(ctx context.Context, provider string) error
	TestProjectProvider(ctx context.Context, provider string) (*ProviderTestResult, error)
	GetProjectModelConfig(ctx context.Context) (*ProjectModelConfig, error)
	UpsertProjectModelConfig(ctx context.Context, generativeModel, embeddingModel string) (*ProjectModelConfig, error)
	ListOrgs(ctx context.Context) ([]Org, error)
	GetOrg(ctx context.Context, orgID string) (*Org, error)
	CreateOrg(ctx context.Context, name string) (*Org, error)
	UpdateOrg(ctx context.Context, orgID, name string) (*Org, error)
	DeleteOrg(ctx context.Context, orgID string) error
	ListOrgMembers(ctx context.Context, orgID string) ([]OrgMemberDto, error)
	ListOrgToolSettings(ctx context.Context, orgID string) ([]OrgToolSettingDto, error)
	UpsertOrgToolSetting(ctx context.Context, orgID, toolName string, in UpsertOrgToolSettingInput) (*OrgToolSettingDto, error)
	DeleteOrgToolSetting(ctx context.Context, orgID, toolName string) error
	GetOrgsAndProjects(ctx context.Context) ([]OrgWithProjectsDto, error)
	ListProjects(ctx context.Context) ([]ProjectRef, error)
	ListProjectsIncludingPending(ctx context.Context) ([]ProjectRef, error)
	CreateProject(ctx context.Context, name, orgID string) (*ProjectRef, error)
	DeleteProject(ctx context.Context, projectID string) error
	RestoreProject(ctx context.Context, projectID string) error
	TransferProject(ctx context.Context, projectID, destinationOrgID string) error
	ListMembers(ctx context.Context) ([]ProjectMemberDto, error)
	RemoveMember(ctx context.Context, userID string) error
	ListInvites(ctx context.Context) ([]SentInviteDto, error)
	CreateInvite(ctx context.Context, in CreateInviteDto) (*Invite, error)
	AcceptInvite(ctx context.Context, token string) error
	ListPendingInvites(ctx context.Context) ([]PendingInviteDto, error)
	DeclineInvite(ctx context.Context, inviteID string) error
	CancelInvite(ctx context.Context, inviteID string) error
	SearchUsers(ctx context.Context, email string) ([]UserSearchResultDto, error)
	GetProfile(ctx context.Context) (*UserProfileDto, error)
	UpdateProfile(ctx context.Context, in UpdateUserProfileDto) (*UserProfileDto, error)
	UploadAvatar(ctx context.Context, filename string, r io.Reader) (*UserProfileDto, error)
	DeleteAvatar(ctx context.Context) (*UserProfileDto, error)
	GetAvatar(ctx context.Context) (io.ReadCloser, string, error)
	// API-token management (project-scoped: the session's active project).
	ListAPITokens(ctx context.Context) ([]APIToken, error)
	CreateAPIToken(ctx context.Context, name string, scopes []string) (*APITokenCreateResponse, error)
	GetAPIToken(ctx context.Context, tokenID string) (*APIToken, error)
	UpdateAPITokenScopes(ctx context.Context, tokenID string, scopes []string) (*APIToken, error)
	RevokeAPIToken(ctx context.Context, tokenID string) error
	RegenerateAPIToken(ctx context.Context, tokenID string) (*APITokenCreateResponse, error)
	// API-token management (account-scoped: the signed-in user).
	ListAccountAPITokens(ctx context.Context) ([]APIToken, error)
	CreateAccountAPIToken(ctx context.Context, name string, scopes []string) (*APITokenCreateResponse, error)
	GetAccountAPIToken(ctx context.Context, tokenID string) (*APIToken, error)
	UpdateAccountAPITokenScopes(ctx context.Context, tokenID string, scopes []string) (*APIToken, error)
	RevokeAccountAPIToken(ctx context.Context, tokenID string) error
	RegenerateAccountAPIToken(ctx context.Context, tokenID string) (*APITokenCreateResponse, error)
}

// compile-time assertion that MemoryClient satisfies the interface.
var _ MemoryBackend = (*MemoryClient)(nil)
