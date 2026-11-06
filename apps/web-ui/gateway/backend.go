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
	QueryKnowledge(ctx context.Context, question, branch string) (answer, sessionID string, err error)
	RespondQuestion(ctx context.Context, questionID, response, message string) (*RespondQuestionResult, error)
	CancelQuestion(ctx context.Context, questionID string) (*RespondQuestionResult, error)
	ListAgentQuestions(ctx context.Context) ([]AgentQuestionItem, error)
	ListToolApprovals(ctx context.Context) ([]ToolApprovalItem, error)
	CancelAgentRun(ctx context.Context, runID string) (bool, error)
	ListSessionTodos(ctx context.Context, sessionID string) ([]SessionTodo, error)
	ListConversations(ctx context.Context, includeArchived bool) (*ConversationList, error)
	// ListConversationsWithAncestors is the rail's variant: the page also
	// carries the visible ancestors of its sub-chats so a child that outran its
	// parent is still groupable.
	ListConversationsWithAncestors(ctx context.Context, includeArchived bool) (*ConversationList, error)
	ArchiveConversation(ctx context.Context, id string) error
	UnarchiveConversation(ctx context.Context, id string) error
	DeleteConversation(ctx context.Context, id string) error
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
	TestMCPServer(ctx context.Context, in *MCPServer, serverID string) (*MCPInspectResult, error)
	ListMCPServerTools(ctx context.Context, id string) ([]MCPTool, error)
	SetMCPServerToolEnabled(ctx context.Context, id string, toolID string, enabled bool) error
	CallMCPServerTool(ctx context.Context, id, toolName string, args map[string]any) (*MCPToolCallResult, error)
	// ListBuiltinToolGroups returns memory's builtin tool capability taxonomy
	// (the grouped view of the builtin server's tools).
	ListBuiltinToolGroups(ctx context.Context) ([]BuiltinToolGroup, error)
	// Official MCP registry browse/install (see mcp_registry_client.go).
	SearchRegistry(ctx context.Context, query string, limit int, cursor string) (*MCPRegistrySearchResult, error)
	InstallRegistryServer(ctx context.Context, registryName, version, name string) (*MCPRegistryInstallResult, error)
	// InstallRegistryServerToDevice installs a registry entry's local package on
	// a connected connector device; empty instanceID broadcasts (admins only).
	InstallRegistryServerToDevice(ctx context.Context, registryName, version, name, instanceID string) (*MCPRegistryInstallResult, error)
	// MCP OAuth connect (see mcp_servers_client.go).
	AuthorizeMCPServer(ctx context.Context, id string) (*MCPOAuthAuthorize, error)
	DisconnectMCPServer(ctx context.Context, id string) error
	SetMCPServerOAuthClient(ctx context.Context, id, clientID, clientSecret string) (*MCPOAuthStatus, error)
	ClearMCPServerOAuthClient(ctx context.Context, id string) error
	// MCP share instances (project-scoped MCP exposure; see mcp_shares.go).
	// The raw key is only ever present on Create/Rotate results.
	ListMCPShareInstances(ctx context.Context) ([]MCPShareInstance, error)
	CreateMCPShareInstance(ctx context.Context, in *MCPShareInput) (*MCPShareCreated, error)
	GetMCPShareInstance(ctx context.Context, id string) (*MCPShareInstance, error)
	UpdateMCPShareInstance(ctx context.Context, id string, in *MCPShareInput) (*MCPShareInstance, error)
	RevokeMCPShareInstance(ctx context.Context, id string) error
	RotateMCPShareInstance(ctx context.Context, id string, emails []string) (*MCPShareCreated, error)
	// InviteMCPShareInstance emails an existing share's CURRENT key (recovered
	// from encrypted storage) to the listed recipients; it never rotates.
	InviteMCPShareInstance(ctx context.Context, id string, emails []string) (*MCPShareInvite, error)
	ListMCPShareCatalog(ctx context.Context) (*MCPShareCatalog, error)
	// Agent-owned MCP endpoint + labeled keys (agent-scoped-mcp-endpoint; see
	// agent_mcp_endpoint.go). The raw key secret is only ever present on
	// Create/Rotate results; Get/List carry metadata only.
	GetAgentMCPEndpoint(ctx context.Context, agentID string) (*AgentMCPEndpoint, error)
	CreateAgentMCPEndpoint(ctx context.Context, agentID string) (*AgentMCPEndpoint, error)
	RevokeAgentMCPEndpoint(ctx context.Context, endpointID string) error
	ListAgentMCPKeys(ctx context.Context, endpointID string) ([]AgentMCPKey, error)
	CreateAgentMCPKey(ctx context.Context, endpointID, label string) (*AgentMCPKeySecret, error)
	RevokeAgentMCPKey(ctx context.Context, keyID string) error
	RotateAgentMCPKey(ctx context.Context, keyID string) (*AgentMCPKeySecret, error)
	ListAgentMCPSessions(ctx context.Context, endpointID, status string) ([]AgentMCPSession, error)
	// MCP relay (external nodes). Unlike the admin registry routes above these
	// are project-scoped read endpoints that return PLAIN JSON (no
	// successEnvelope wrapper). Sessions are in-memory on the backend: a node
	// present in ListRelaySessions is currently connected.
	ListRelaySessions(ctx context.Context) ([]RelaySession, error)
	GetRelaySessionTools(ctx context.Context, instanceID string) ([]RelayTool, error)
	ListModels(ctx context.Context) ([]Model, error)
	ListDocuments(ctx context.Context, params DocumentListParams) (*DocumentPage, error)
	ListDocumentSourceCounts(ctx context.Context) ([]DocumentSourceCount, error)
	GetDocument(ctx context.Context, id string) (*Document, error)
	DeleteDocument(ctx context.Context, id string) error
	ListChunks(ctx context.Context, documentID string) ([]Chunk, error)
	EditChunk(ctx context.Context, chunkID, text string) (ChunkEditResult, error)
	ListChunkRevisions(ctx context.Context, chunkID string) ([]ChunkRevision, error)
	DiffChunkRevisions(ctx context.Context, chunkID string, from, to int) (ChunkDiff, error)
	RollbackChunk(ctx context.Context, chunkID string, seq int) (ChunkEditResult, error)
	UploadDocument(ctx context.Context, filename string, r io.Reader) (UploadResult, error)
	CreateExtractionJob(ctx context.Context, documentID string) (string, error)
	GetExtractionSummary(ctx context.Context, documentID string) (*ExtractionSummary, error)
	GetDocumentContent(ctx context.Context, documentID string) (string, error)
	ListExtractionJobs(ctx context.Context, documentID string) ([]ExtractionJob, error)
	// ListProjectExtractionJobs lists the project's extraction jobs without a
	// source filter; the chat rail's extraction origin uses it.
	ListProjectExtractionJobs(ctx context.Context, limit int) ([]ExtractionJob, error)
	GetExtractionJob(ctx context.Context, jobID string) (*ExtractionJob, error)
	// Processing problems: one project-level read model behind the
	// needs-attention panel and the Documents badge.
	GetProcessingProblems(ctx context.Context) (*ProcessingProblemsReport, error)
	RetryExtractionJob(ctx context.Context, jobID string) error
	// RetryParsingJob resets one failed document conversion in place; the
	// per-unit counterpart of RetryAllParsingJobs.
	RetryParsingJob(ctx context.Context, jobID string) error
	// The bulk retries take includeNonRetryable: the panel sets it only after
	// the user confirms the reported cause is fixed, because the server
	// otherwise skips configuration/permission failures.
	RetryAllExtractionJobs(ctx context.Context, includeNonRetryable bool) (int, error)
	RetryAllParsingJobs(ctx context.Context, includeNonRetryable bool) (int, error)
	RetriggerEmbeddings(ctx context.Context, includeNonRetryable bool) (int, error)
	DownloadDocument(ctx context.Context, id string) (io.ReadCloser, string, error)
	ListDocumentVersions(ctx context.Context, documentID string) ([]DocumentVersion, error)
	// Data-source integrations (see memory_datasources.go). Credentials are
	// write-only: no read method ever returns them.
	ListDSProviders(ctx context.Context) ([]DSProvider, error)
	GetDSProviderSchema(ctx context.Context, providerType string) (*DSProviderSchema, error)
	// ListDSCredentials returns the member-readable OAuth credential
	// descriptions (configured flags, non-secret client ids, resolved scope).
	ListDSCredentials(ctx context.Context) ([]DSOAuthCredentialDescription, error)
	// Saved, project-scoped data-source credentials (metadata only; secrets are
	// write-only). Used by the credentials page and the connect-form picker.
	ListSavedDSCredentials(ctx context.Context, providerType string) ([]DSSavedCredential, error)
	CreateSavedDSCredential(ctx context.Context, req *DSSavedCredentialRequest) (*DSSavedCredential, error)
	UpdateSavedDSCredential(ctx context.Context, id string, req *DSSavedCredentialUpdateRequest) (*DSSavedCredential, error)
	DeleteSavedDSCredential(ctx context.Context, id string) error
	ListDSSourceTypes(ctx context.Context) ([]DSSourceType, error)
	TestDSConfig(ctx context.Context, req *DSTestConfigRequest) (*DSTestConnectionResponse, error)
	TestDSConnection(ctx context.Context, id string) (*DSTestConnectionResponse, error)
	ListDSIntegrations(ctx context.Context) ([]DSIntegration, error)
	GetDSIntegration(ctx context.Context, id string) (*DSIntegration, error)
	GetDSPickerSession(ctx context.Context, id string) (*DSPickerSession, error)
	SetDSSyncTargets(ctx context.Context, id string, folderIDs []string) (*DSIntegration, error)
	CreateDSIntegration(ctx context.Context, req *DSCreateIntegrationRequest) (*DSIntegration, error)
	UpdateDSIntegration(ctx context.Context, id string, req *DSUpdateIntegrationRequest) (*DSIntegration, error)
	DeleteDSIntegration(ctx context.Context, id string) error
	TriggerDSSync(ctx context.Context, id string) (*DSTriggerSyncResponse, error)
	ListDSSyncJobs(ctx context.Context, id, status string, limit, offset int) (*DSListSyncJobsResponse, error)
	GetLatestDSSyncJob(ctx context.Context, id string) (*DSSyncJob, error)
	GetDSSyncJob(ctx context.Context, id, jobID string) (*DSSyncJob, error)
	CancelDSSyncJob(ctx context.Context, id, jobID string) (*DSCancelSyncJobResponse, error)
	StartDSOAuth(ctx context.Context, providerType string, config map[string]any, scopes []string, reconnectSourceID string) (*DSOAuthAuthorizeResponse, error)
	ListDSOAuthClients(ctx context.Context, orgID string) ([]DSOAuthClientConfig, error)
	GetDSOAuthClient(ctx context.Context, orgID, providerType string) (*DSOAuthClientConfig, error)
	UpsertDSOAuthClient(ctx context.Context, orgID, providerType string, req *DSSetOAuthClientRequest) (*DSOAuthClientConfig, error)
	DeleteDSOAuthClient(ctx context.Context, orgID, providerType string) error
	ListProjectDSOAuthClients(ctx context.Context, projectID string) ([]DSOAuthClientConfig, error)
	GetProjectDSOAuthClient(ctx context.Context, projectID, providerType string) (*DSOAuthClientConfig, error)
	UpsertProjectDSOAuthClient(ctx context.Context, projectID, providerType string, req *DSSetOAuthClientRequest) (*DSOAuthClientConfig, error)
	DeleteProjectDSOAuthClient(ctx context.Context, projectID, providerType string) error
	ListDSResources(ctx context.Context, id, query string) ([]DSResource, bool, error)
	// Credential-scoped listing for unsaved configs (connect-form pickers).
	ListDSProviderResources(ctx context.Context, providerType string, config map[string]any, credentialID string) ([]DSResource, bool, error)
	ListDSProviderRefs(ctx context.Context, providerType string, config map[string]any, credentialID string) ([]DSResource, bool, error)
	SaveDSResource(ctx context.Context, id string, cfg map[string]any) (*DSIntegration, error)
	GetGraphObject(ctx context.Context, objectID string) (*GraphObject, error)
	GetObjectHistory(ctx context.Context, objectID string) ([]GraphObject, error)
	LookupGraphObjectByKey(ctx context.Context, typeFilter, key string) (*GraphObject, error)
	ListGraphObjects(ctx context.Context, branchID, typeFilter string, ids []string) ([]GraphObject, error)
	ListGraphRelationships(ctx context.Context, branchID string) ([]GraphRelationship, error)
	GetObjectEdges(ctx context.Context, objectID string) ([]GraphRelationship, error)
	GetSimilarObjects(ctx context.Context, objectID string, limit int) ([]SimilarObject, error)
	// Work items (object-driven work / Kanban board).
	ListWorkItems(ctx context.Context, status, typeName string, limit int) ([]WorkItem, error)
	GetWorkItem(ctx context.Context, canonicalID string) (*WorkItemDetail, error)
	ApproveWorkItem(ctx context.Context, canonicalID string) (*WorkItemAction, error)
	RequestChangesWorkItem(ctx context.Context, canonicalID, feedback string) (*WorkItemAction, error)
	AddWorkComment(ctx context.Context, canonicalID, body string) (*WorkItemAction, error)
	RetryWorkItem(ctx context.Context, canonicalID string) (*WorkItemAction, error)
	ReassignWorkItem(ctx context.Context, canonicalID, assignee string) (*WorkItemAction, error)
	SetWorkItemStatus(ctx context.Context, canonicalID, status string) (*WorkItemAction, error)
	CancelWorkItem(ctx context.Context, canonicalID string) (*WorkItemAction, error)
	// ListBoardRuns lists the project's object-driven (board/work) runs.
	ListBoardRuns(ctx context.Context, limit int) ([]BoardRun, error)
	UpdateObject(ctx context.Context, id string, req *UpdateObjectRequest) (*GraphObject, error)
	// DeleteGraphObject soft-deletes one object on the given branch (empty = main
	// branch; branch history keeps the versions). BulkUpdateStatus sets one status
	// across up to 100 objects in a single call; both sit behind the objects
	// browser's selection bar.
	DeleteGraphObject(ctx context.Context, id, branchID string) error
	BulkUpdateStatus(ctx context.Context, ids []string, status string) (*BulkUpdateStatusResponse, error)
	CreateObject(ctx context.Context, req *CreateObjectRequest) (*GraphObject, error)
	CreateRelationship(ctx context.Context, req *CreateRelationshipRequest) error
	SearchObjectsFTS(ctx context.Context, query, typeFilter string) ([]GraphObject, error)
	ListGraphObjectsPage(ctx context.Context, params ObjectListParams) ([]GraphObject, string, error)
	CountObjects(ctx context.Context, branchID string) (int, error)
	SearchObjects(ctx context.Context, mode, query, types, branchID string, limit, offset int) ([]ObjectSearchResult, bool, error)
	SearchObjectsUnified(ctx context.Context, query, types, branchID string, limit int) ([]ObjectSearchResult, error)
	SearchUnified(ctx context.Context, params UnifiedSearchParams) (*UnifiedSearchResponse, error)
	GetEmbeddingProgress(ctx context.Context) (*EmbeddingProgress, error)
	GetEmbeddingStatus(ctx context.Context) (*EmbeddingStatus, error)
	GetEmbeddingCoverage(ctx context.Context) (*EmbeddingCoverageResponse, error)
	ListBranches(ctx context.Context) ([]Branch, error)
	GetMergeReadiness(ctx context.Context, branchID string) (*MergeReadiness, error)
	MergeBranch(ctx context.Context, sourceBranchID, policy string) (*BranchMergeResult, error)
	PreviewMergeBranch(ctx context.Context, sourceBranchID, policy string) (*BranchMergeResult, error)
	GetCompiledTypes(ctx context.Context) (*CompiledSchemaTypes, error)
	// Schema discovery: the Schema page's card and the per-document trigger.
	ListDiscoveryJobs(ctx context.Context) ([]discoveryJob, error)
	StartProjectDiscovery(ctx context.Context) (*DiscoveryStartResult, error)
	StartDocumentDiscovery(ctx context.Context, documentID string) (*DiscoveryStartResult, error)
	RetryDiscoveryJob(ctx context.Context, jobID string) (*DiscoveryStartResult, error)
	CancelDiscoveryJob(ctx context.Context, jobID string) error
	ListAllSchemas(ctx context.Context) ([]SchemaInfo, error)
	GetSchema(ctx context.Context, packID string) (*BlueprintSchema, error)
	CreateSchemaPack(ctx context.Context, req *SchemaPackWriteRequest) (*BlueprintSchema, error)
	UpdateSchemaPack(ctx context.Context, packID string, edit ObjectTypeEdit) error
	AssignSchemaPack(ctx context.Context, schemaID string) error
	ListBlueprintVersions(ctx context.Context, name string) ([]BlueprintRecord, error)
	ListBlueprints(ctx context.Context) ([]BlueprintRecord, error)
	CreateBlueprint(ctx context.Context, req *createBlueprintRequest) (*BlueprintRecord, error)
	ImportBlueprint(ctx context.Context, url, ref, token string) (*BlueprintRecord, error)
	PreviewImportBlueprint(ctx context.Context, url, ref, token string) (*BlueprintPreview, error)
	ImportAndApplyBlueprint(ctx context.Context, url, ref, token string, overrides map[string]string) (*BlueprintRecord, error)
	ImportAndApplyBlueprintWithOptions(ctx context.Context, url, ref, token string, overrides map[string]string, merge bool) (*BlueprintRecord, error)
	GetBlueprint(ctx context.Context, id string) (*BlueprintRecord, error)
	PublishBlueprint(ctx context.Context, id string) error
	ApplyBlueprint(ctx context.Context, id string) (*BlueprintApplyResult, error)
	// ApplyBlueprintWithOverrides is kept for compatibility; new code uses
	// ApplyBlueprintWithOptions so overrides and the merge flag travel together.
	ApplyBlueprintWithOverrides(ctx context.Context, id string, overrides map[string]string) (*BlueprintApplyResult, error)
	ApplyBlueprintWithOptions(ctx context.Context, id string, opts BlueprintApplyOptions) (*BlueprintApplyResult, error)
	PreviewApplyBlueprint(ctx context.Context, id string) (*BlueprintApplyPreview, error)
	GetModelOverrides(ctx context.Context, name string) (map[string]string, error)
	ListAppliedBlueprints(ctx context.Context) ([]AppliedBlueprint, error)
	UnapplyBlueprint(ctx context.Context, id string) (*BlueprintUnapplyResult, error)
	GetBlueprintUpdates(ctx context.Context, name string) (*BlueprintUpdateCheck, error)
	CheckBlueprintUpdates(ctx context.Context, name string) (*BlueprintUpdateJob, error)
	ApplyBlueprintUpdate(ctx context.Context, name string) (*BlueprintUpdateJob, error)
	ApplyBlueprintUpdateWithMerge(ctx context.Context, name string, merge bool) (*BlueprintUpdateJob, error)
	PreviewBlueprintUpdate(ctx context.Context, name string) (*BlueprintUpdatePreview, error)
	SetGithubCredential(ctx context.Context, token string) error
	DeleteBlueprint(ctx context.Context, id string) error
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
	ListProviderDefinitions(ctx context.Context) ([]ProviderDefinition, error)
	ListProviderModels(ctx context.Context, provider string) ([]ProviderSupportedModel, error)
	ListPricing(ctx context.Context) ([]ProviderPricing, error)
	ListProjectPricingOverrides(ctx context.Context) ([]ProjectCustomPricing, error)
	UpsertProjectPricingOverride(ctx context.Context, provider, providerSlug, model string, rates modelPriceRates) (*ProjectCustomPricing, error)
	DeleteProjectPricingOverride(ctx context.Context, provider, model string) error
	UpsertProjectProviderConfig(ctx context.Context, provider string, in ProviderConfigInput) (*ProjectProviderConfig, error)
	DeleteProjectProviderConfig(ctx context.Context, provider string) error
	TestProjectProvider(ctx context.Context, provider string) (*ProviderTestResult, error)
	TestProjectModel(ctx context.Context, provider, model, modelType string) (*ProviderTestResult, error)
	GetProjectModelConfig(ctx context.Context) (*ProjectModelConfig, error)
	GetEffectiveModelConfig(ctx context.Context) (*EffectiveModelConfig, error)
	UpsertProjectModelConfig(ctx context.Context, generativeModel, embeddingModel string) (*ProjectModelConfig, error)
	ListOrgs(ctx context.Context) ([]Org, error)
	GetOrg(ctx context.Context, orgID string) (*Org, error)
	CreateOrg(ctx context.Context, name string) (*Org, error)
	UpdateOrg(ctx context.Context, orgID, name string) (*Org, error)
	DeleteOrg(ctx context.Context, orgID string) error
	ListOrgMembers(ctx context.Context, orgID string) ([]OrgMemberDto, error)
	AddOrgMember(ctx context.Context, orgID, email, role string) (*OrgMemberDto, error)
	UpdateOrgMemberRole(ctx context.Context, orgID, userID, role string) error
	RemoveOrgMember(ctx context.Context, orgID, userID string) error
	ListOrgInvites(ctx context.Context, orgID string) ([]SentInviteDto, error)
	GetOrgPresence(ctx context.Context, orgID string) (*PresenceDto, error)
	GetProjectPresence(ctx context.Context) (*PresenceDto, error)
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
	UpdateMemberRole(ctx context.Context, userID, role string) error
	ListInvites(ctx context.Context) ([]SentInviteDto, error)
	CreateInvite(ctx context.Context, in CreateInviteDto) (*Invite, error)
	AcceptInvite(ctx context.Context, token string) error
	ListPendingInvites(ctx context.Context) ([]PendingInviteDto, error)
	DeclineInvite(ctx context.Context, inviteID string) error
	CancelInvite(ctx context.Context, inviteID string) error
	ResendInvite(ctx context.Context, inviteID string) (InviteResendOutcome, error)
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
	// Scoped per-device credential (see web-device-credential): introspect a raw
	// bearer to recognise the device:api marker + resolve its project/org, and
	// mint a credential on the session-authenticated setup flow.
	IntrospectDeviceToken(ctx context.Context, token string) (*deviceTokenInfo, error)
	CreateDeviceToken(ctx context.Context, name string) (*APITokenCreateResponse, error)
	// API-token management (account-scoped: the signed-in user).
	ListAccountAPITokens(ctx context.Context) ([]APIToken, error)
	CreateAccountAPIToken(ctx context.Context, name string, scopes []string) (*APITokenCreateResponse, error)
	GetAccountAPIToken(ctx context.Context, tokenID string) (*APIToken, error)
	UpdateAccountAPITokenScopes(ctx context.Context, tokenID string, scopes []string) (*APIToken, error)
	RevokeAccountAPIToken(ctx context.Context, tokenID string) error
	RegenerateAccountAPIToken(ctx context.Context, tokenID string) (*APITokenCreateResponse, error)
	// Public agent-share (see share.go / memory_share.go). Share methods take an
	// explicit share token (the decrypted key) and never send project/org scope.
	SharePublicConfig(ctx context.Context, token string) (*SharePublicConfig, error)
	ShareListSessions(ctx context.Context, token, endUserRef, filter string) ([]ShareSession, error)
	ShareGetSession(ctx context.Context, token, endUserRef, id string) (*ShareSessionDetail, error)
	ShareCreateSession(ctx context.Context, token, endUserRef string, in ShareCreateSessionInput) (*ShareSession, error)
	ShareArchiveSession(ctx context.Context, token, endUserRef, id string) error
	ShareUnarchiveSession(ctx context.Context, token, endUserRef, id string) error
	ShareDeleteSession(ctx context.Context, token, endUserRef, id string) error
	ShareCancelApproval(ctx context.Context, token, endUserRef, sessionID, questionID string) error
	ShareApprove(ctx context.Context, token, endUserRef, sessionID, questionID string, in ShareApproveInput) error
	ShareChatStream(ctx context.Context, token, endUserRef string, in ShareStreamInput) (io.ReadCloser, error)
	// Owner share-link management (project-scoped via the session context).
	ListShareLinks(ctx context.Context, agentDefinitionID string) ([]ShareLink, error)
	CreateShareLink(ctx context.Context, agentDefinitionID string, in ShareLinkCreateInput) (*ShareLink, error)
	RevokeShareLink(ctx context.Context, linkID string) error
	RotateShareLink(ctx context.Context, linkID string) (*ShareLink, error)
	RevealShareLink(ctx context.Context, linkID string) (string, error)
	// Owner share-session management (project-scoped via the session context).
	ListShareSessionsByProject(ctx context.Context) ([]ShareOwnerSession, error)
	GetShareSessionTranscript(ctx context.Context, id string) ([]ShareMessage, error)
	// Inbox / notifications (see memory_notifications.go). Account vs project
	// scope is explicit; preferences drive opt-in project-event delivery.
	ListNotifications(ctx context.Context, p NotificationListParams) ([]Notification, error)
	NotificationCounts(ctx context.Context, scope, projectID string) (*NotificationCounts, error)
	// Preferences are project-scoped: the project id selects which project's
	// preferences are read/written.
	ListNotificationPreferences(ctx context.Context, projectID string) ([]NotificationPreference, error)
	SetNotificationPreference(ctx context.Context, projectID, eventKey, channel string, enabled bool) error
	MarkNotificationRead(ctx context.Context, id string) error
	MarkNotificationUnread(ctx context.Context, id string) error
	DismissNotification(ctx context.Context, id string) error
	SnoozeNotification(ctx context.Context, id, until string) error
	UnsnoozeNotification(ctx context.Context, id string) error
	ClearNotification(ctx context.Context, id string) error
	RestoreNotification(ctx context.Context, id string) error
	ResolveNotification(ctx context.Context, id, action string) error
	// ResolveNotificationsForResource resolves every notification about one
	// resource (POST /api/notifications/resolve-resource), e.g. clearing a
	// graph_branch's review-ready notifications once the branch is merged.
	ResolveNotificationsForResource(ctx context.Context, resourceType, resourceID string) error
	MarkAllNotificationsRead(ctx context.Context, scope, projectID string) error
	// NotificationEventStream opens the session's real-time notification stream
	// (account when projectID is empty, else that project's).
	NotificationEventStream(ctx context.Context, projectID string) (io.ReadCloser, error)
}

// compile-time assertion that MemoryClient satisfies the interface.
var _ MemoryBackend = (*MemoryClient)(nil)
