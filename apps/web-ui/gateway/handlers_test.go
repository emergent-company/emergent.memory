package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// triggerAgentCall captures one TriggerAgent invocation for assertions.
type triggerAgentCall struct {
	AgentID string
	Prompt  string
	Context map[string]string
}

// fakeMemory is an in-memory MemoryBackend for tests.
type fakeMemory struct {
	agents  []AgentDefinitionSummary
	defs    map[string]*AgentDefinition // full definitions keyed by id
	convs   []Conversation
	servers []MCPServer

	// MCP relay (external nodes): relaySessions is returned by
	// ListRelaySessions; relayTools holds per-instance tool lists (an empty
	// value serves a node with no tools). relaySessionsErr /
	// relayToolsErr simulate whole-endpoint failures.
	relaySessions       []RelaySession
	relaySessionsErr    error
	relayTools          map[string][]RelayTool
	relayToolsErr       error                           // failure for every GetRelaySessionTools call
	histories           map[string]*ConversationHistory // history per conversation id
	details             map[string]*ConversationDetail  // detail per conversation id
	createConvID        string                          // id returned by CreateObjectConversation
	createConvErr       error                           // CreateObjectConversation failure
	convCanonicalID     string                          // last canonicalID passed to CreateObjectConversation
	convTitle           string                          // last title passed to CreateObjectConversation
	convMessage         string                          // last message passed to CreateObjectConversation
	memories            []Memory
	created             []AgentDefinition
	updatedAgent        *AgentDefinition // last agent passed to UpdateAgentDefinition
	chatMsg             string
	chatStream          string // SSE body returned by ChatStream when non-empty
	chatErr             error  // ChatStream failure
	questionID          string // question id received via RespondQuestion
	questionResponse    string // response received via RespondQuestion
	questionMessage     string // optional reject message received via RespondQuestion
	questionCancelID    string // question id received via CancelQuestion
	approvals           []ToolApprovalItem
	convErr             error // ListConversations failure
	histErr             error // GetConversationHistory failure
	memErr              error // SearchMemories/ListMemories failure
	documents           []Document
	chunks              []Chunk
	docErr              error                      // ListDocuments/GetDocument/ListChunks failure
	extractErr          error                      // CreateExtractionJob failure
	uploaded            []string                   // filenames received via UploadDocument
	uploadResult        *UploadResult              // non-nil overrides the UploadDocument result
	extracted           []string                   // document ids passed to CreateExtractionJob
	deletedDocuments    []string                   // document ids passed to DeleteDocument, in order
	deleteDocErr        error                      // DeleteDocument failure
	summary             *ExtractionSummary         // returned by GetExtractionSummary
	objects             []GraphObject              // returned by ListExtractionObjects
	relns               []GraphRelationship        // returned by ListRelationships
	branches            []Branch                   // returned by ListBranches
	similar             []SimilarObject            // returned by GetSimilarObjects
	similarErr          error                      // GetSimilarObjects failure
	updateResult        *GraphObject               // returned by UpdateObject
	updateObjectErr     error                      // UpdateObject failure
	lastUpdateID        string                     // last id passed to UpdateObject
	lastUpdateReq       *UpdateObjectRequest       // last request passed to UpdateObject
	createObjectResult  *GraphObject               // returned by CreateObject
	createObjectErr     error                      // CreateObject failure
	lastCreateObjectReq *CreateObjectRequest       // last request passed to CreateObject
	createRelErr        error                      // CreateRelationship failure
	lastCreateRelReq    *CreateRelationshipRequest // last request passed to CreateRelationship
	ftsResults          []GraphObject              // returned by SearchObjectsFTS
	ftsErr              error                      // SearchObjectsFTS failure
	ftsQuery            string                     // last query passed to SearchObjectsFTS
	ftsTypeFilter       string                     // last type filter passed to SearchObjectsFTS

	compiled     *CompiledSchemaTypes
	blueprintErr error // failure for any blueprint method

	blueprintVersions   []BlueprintRecord       // returned by ListBlueprintVersions
	blueprints          []BlueprintRecord       // returned by ListBlueprints
	blueprint           *BlueprintRecord        // returned by GetBlueprint
	createdBlueprint    *createBlueprintRequest // last CreateBlueprint req
	publishedBlueprint  []string                // ids passed to PublishBlueprint
	appliedBlueprint    []string                // ids passed to ApplyBlueprint
	applyBlueprintRes   *BlueprintApplyResult   // returned by ApplyBlueprint
	appliedBlueprints   []AppliedBlueprint      // returned by ListAppliedBlueprints
	unappliedBlueprint  []string                // ids passed to UnapplyBlueprint
	unapplyBlueprintRes *BlueprintUnapplyResult // returned by UnapplyBlueprint

	schemas []SchemaInfo
	schema  *BlueprintSchema

	// schema pack mutations (design D1).
	createdSchemaPack     *SchemaPackWriteRequest // last CreateSchemaPack req
	createdSchemaPackRes  *BlueprintSchema        // returned by CreateSchemaPack
	updatedSchemaPackID   string                  // last pack id passed to UpdateSchemaPack
	updatedSchemaPackEdit *ObjectTypeEdit         // last edit passed to UpdateSchemaPack
	assignedSchemaPacks   []string                // ids passed to AssignSchemaPack
	schemaPackErr         error                   // failure for any pack mutation

	// blueprint derivation.
	blueprintByID              map[string]*BlueprintRecord // overrides GetBlueprint when set
	createdBlueprintVersion    *CreateBlueprintVersionRequest
	createdBlueprintVersionRes *BlueprintRecord
	createdBlueprintVersionID  string
	updatedBlueprintID         string
	updatedBlueprintReq        *UpdateBlueprintRequest
	updatedBlueprintRes        *BlueprintRecord
	blueprintVersionErr        error

	history          []SchemaHistoryItem
	migrationPreview *MigrationPreviewResult
	migrationExecute *MigrationExecuteResult
	migrationJob     *MigrationJob
	validation       *SchemaValidationResult
	validateFn       func(ctx context.Context) (*SchemaValidationResult, error) // overrides validation when set (per-call results)
	migrationErr     error                                                      // failure for any migration method
	migrationFrom    string
	migrationTo      string
	migrationForce   bool
	migrationVersion string // to_version / through_version passed
	migrationJobID   string

	skills         []Skill
	skillErr       error   // failure for any skill method
	createdSkills  []Skill // skills passed to CreateSkill
	updatedSkillID string  // last id passed to UpdateSkill
	deletedSkillID string  // last id passed to DeleteSkill

	backups           []Backup            // returned by ListBackups
	backupErr         error               // failure for any backup read/list method
	createdBackupReq  CreateBackupRequest // last create request forwarded
	createdBackup     *Backup             // returned by CreateBackup
	createdBackupErr  error               // failure for CreateBackup
	downloadURL       string              // returned by DownloadBackup
	downloadBackupErr error               // failure for DownloadBackup
	deletedBackupID   string              // last id passed to DeleteBackup
	deletedBackupErr  error               // failure for DeleteBackup

	project          *Project
	projectErr       error          // GetCurrentProject failure
	updateErr        error          // UpdateProject failure
	updatedProject   *ProjectUpdate // last update passed to UpdateProject
	overrides        []AgentOverrideEntry
	overrideErr      error                                // failure for any agent-override method
	overrideAgent    string                               // last agentName passed to Set/DeleteAgentOverride
	overrideInput    *AgentOverrideInput                  // last input passed to SetAgentOverride
	settings         map[string]map[string]map[string]any // category → key → value store
	settingErr       error                                // failure for any project-setting method
	settingWrites    []projectSettingWrite                // every SetProjectSetting call, in order
	lastSettingCat   string
	lastSettingKey   string
	lastSettingValue map[string]any
	lastDeletedCat   string
	lastDeletedKey   string

	sandboxConfig *AgentSandboxConfig // returned by GetAgentSandboxConfig
	providers     []SandboxProvider   // returned by ListSandboxProviders
	images        []SandboxImage      // returned by ListSandboxImages

	scheduledAgents    []ScheduledAgent               // returned by ListScheduledAgents
	scheduledRuns      map[string][]ScheduledAgentRun // runs per agent id, returned by ListScheduledAgentRuns
	schedErr           error                          // failure for any scheduled-agent method
	createdScheduled   []ScheduledAgent               // agents passed to CreateScheduledAgent
	updatedScheduled   *ScheduledAgent                // last agent passed to UpdateScheduledAgent
	updatedSchedID     string                         // last id passed to UpdateScheduledAgent
	enabledSchedID     string                         // last id passed to EnableScheduledAgent
	setEnabledSchedID  string                         // last id passed to SetScheduledAgentEnabled
	setEnabledSchedVal *bool                          // last enabled passed to SetScheduledAgentEnabled
	deletedSchedID     string                         // last id passed to DeleteScheduledAgent
	triggeredSchedID   string                         // last id passed to TriggerScheduledAgent
	triggerResult      *TriggerResult                 // returned by TriggerScheduledAgent
	triggerAgentCalls  chan triggerAgentCall          // receives each TriggerAgent call (webhook tests; nil = drop)
	triggerAgentErr    error                          // failure for TriggerAgent
	runFull            *AgentRunFull                  // returned by GetRunFull
	runFullErr         error                          // failure for GetRunFull
	runQuestions       []AgentQuestionItem            // returned by GetRunQuestions
	runQuestionsErr    error                          // failure for GetRunQuestions
	agentRuns          map[string]*AgentRun           // returned by GetAgentRun per run id
	agentRunErr        error                          // failure for GetAgentRun

	usageSummary *UsageSummaryResponse    // returned by GetProjectUsageSummary
	usageSeries  *UsageTimeSeriesResponse // returned by GetProjectUsageTimeSeries
	usageErr     error                    // failure for the usage methods

	projectProviders []ProjectProviderConfig             // returned by ListProjectProviders
	modelsByProvider map[string][]ProviderSupportedModel // returned by ListProviderModels per provider
	pricing          []ProviderPricing                   // returned by ListPricing
	pricingOverrides []ProjectCustomPricing              // returned by ListProjectPricingOverrides
	providerErr      error                               // failure for the providers/pricing/overrides reads
	modelErr         error                               // failure for ListProviderModels
	overrideWrites   []ProjectCustomPricing              // every UpsertProjectPricingOverride call, in order
	deletedOverrides []string                            // "provider/model" keys passed to DeleteProjectPricingOverride

	providerConfigInput    ProviderConfigInput // last UpsertProjectProviderConfig input
	lastProviderConfig     string              // provider name of the last upsert
	deletedProviderConfigs []string            // provider names passed to DeleteProjectProviderConfig
	providerTestResult     *ProviderTestResult // returned by TestProjectProvider
	modelConfig            *ProjectModelConfig // returned by GetProjectModelConfig
	lastModelConfig        *ProjectModelConfig // last UpsertProjectModelConfig

	orgs               []Org                     // returned by ListOrgs
	createdOrgs        []Org                     // every CreateOrg request, in order
	createOrgID        int                       // id counter for created orgs
	createOrgErr       error                     // failure for CreateOrg
	getOrgErr          error                     // failure for GetOrg
	updateOrgErr       error                     // failure for UpdateOrg
	renamedOrgID       string                    // last org id passed to UpdateOrg
	renamedOrgName     string                    // last name passed to UpdateOrg
	deletedOrgID       string                    // last org id passed to DeleteOrg
	deleteOrgErr       error                     // failure for DeleteOrg
	deletedProjectIDs  []string                  // project ids passed to DeleteProject, in order
	deleteProjectErr   error                     // failure for DeleteProject
	restoredProjectIDs []string                  // project ids passed to RestoreProject, in order
	restoreProjectErr  error                     // failure for RestoreProject
	transferCount      int                       // number of TransferProject calls
	transferredID      string                    // last project id passed to TransferProject
	transferredToOrg   string                    // last destination org id passed to TransferProject
	transferProjectErr error                     // failure for TransferProject
	orgMembers         []OrgMemberDto            // returned by ListOrgMembers
	orgToolSettings    []OrgToolSettingDto       // returned by ListOrgToolSettings
	toolSettingErr     error                     // failure for any org tool-setting method
	toolSettingOrg     string                    // last org id passed to Upsert/DeleteOrgToolSetting
	toolSettingTool    string                    // last tool name passed to Upsert/DeleteOrgToolSetting
	toolSettingIn      UpsertOrgToolSettingInput // last input passed to UpsertOrgToolSetting
	orgsAndProjects    []OrgWithProjectsDto      // returned by GetOrgsAndProjects
	projects           []ProjectRef              // returned by ListProjects
	createdProjects    []ProjectRef              // every CreateProject request, in order
	createProjectID    int                       // id counter for created projects

	members           []ProjectMemberDto    // returned by ListMembers
	removedMember     string                // last user id passed to RemoveMember
	removeMemberErr   error                 // failure for RemoveMember
	sentInvites       []SentInviteDto       // returned by ListInvites
	createdInvites    []CreateInviteDto     // every CreateInvite request, in order
	createInviteID    int                   // id counter for created invites
	createInviteErr   error                 // failure for CreateInvite
	acceptedTokens    []string              // tokens passed to AcceptInvite
	pendingInvites    []PendingInviteDto    // returned by ListPendingInvites
	declinedInvite    string                // last invite id passed to DeclineInvite
	declineInviteErr  error                 // failure for DeclineInvite
	canceledInvite    string                // last invite id passed to CancelInvite
	searchUsers       []UserSearchResultDto // returned by SearchUsers
	searchErr         error                 // failure for SearchUsers
	lastSearchTerm    string                // last email passed to SearchUsers
	profile           *UserProfileDto       // returned by GetProfile
	updatedProfile    UpdateUserProfileDto  // last update passed to UpdateProfile
	avatarUploads     []string              // filenames received via UploadAvatar
	avatarProfile     *UserProfileDto       // returned by UploadAvatar/DeleteAvatar (mirrors avatar state)
	avatarErr         error                 // failure for any avatar method
	avatarBody        []byte                // raw bytes returned by GetAvatar
	avatarContentType string                // Content-Type returned by GetAvatar

	// API tokens: a small stateful store so handler flows (create → list,
	// revoke → relist, regenerate → replacement) can be exercised end-to-end.
	apiTokens                 []APIToken        // project-scoped store
	apiTokenSecrets           map[string]string // plaintext per token id
	apiTokenErr               error             // failure for any project api-token method
	apiTokenRevealPlaintext   bool              // GetAPIToken returns plaintext when true (encryption configured)
	lastAPITokenID            string            // last project token id passed to any api-token op
	lastAPITokenScopes        []string          // scopes of the last create/update/regenerate
	apiTokenSeq               int               // project token id counter
	accountAPITokens          []APIToken        // account-scoped store
	accountAPITokenErr        error             // failure for any account api-token method
	accountAPITokenReveal     bool              // GetAccountAPIToken returns plaintext when true
	lastAccountAPITokenID     string            // last account token id passed to any api-token op
	lastAccountAPITokenScopes []string          // scopes of the last account create/update/regenerate
	accountAPITokenSeq        int               // account token id counter

	// Per-agent MCP shares: agentShares is returned by the list methods;
	// agentShareErr / agentShareCreateErr drive read / create failures;
	// agentShareToken / agentShareMCPURL are the one-time secret returned by
	// create and rotate. The last* fields record what the handlers forwarded.
	agentShares           []AgentMCPShare
	agentShareErr         error // ListAgentMCPShares / ListProjectAgentMCPShares failure
	agentShareCreateErr   error // CreateAgentMCPShare failure
	agentShareToken       string
	agentShareMCPURL      string
	lastAgentShareAgent   string
	lastAgentShareInput   *AgentMCPShareInput
	lastRotatedAgentShare string
	lastRevokedAgentShare string
}

// projectSettingWrite records one SetProjectSetting call on the fake.
type projectSettingWrite struct {
	Category string
	Key      string
	Value    map[string]any
}

func (f *fakeMemory) ListAgentDefinitions(ctx context.Context) ([]AgentDefinitionSummary, error) {
	return f.agents, nil
}

func (f *fakeMemory) GetAgentDefinition(ctx context.Context, id string) (*AgentDefinition, error) {
	if d, ok := f.defs[id]; ok {
		return d, nil
	}
	for _, a := range f.agents {
		if a.ID == id {
			return &AgentDefinition{ID: a.ID, Name: a.Name}, nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (f *fakeMemory) CreateAgentDefinition(ctx context.Context, in *AgentDefinition) (*AgentDefinition, error) {
	in.ID = "new-id"
	f.created = append(f.created, *in)
	f.agents = append(f.agents, AgentDefinitionSummary{ID: in.ID, Name: in.Name})
	return in, nil
}

func (f *fakeMemory) UpdateAgentDefinition(ctx context.Context, id string, in *AgentDefinition) (*AgentDefinition, error) {
	in.ID = id
	f.updatedAgent = in
	return in, nil
}

func (f *fakeMemory) DeleteAgentDefinition(ctx context.Context, id string) error { return nil }

func (f *fakeMemory) ListScheduledAgents(ctx context.Context) ([]ScheduledAgent, error) {
	if f.schedErr != nil {
		return nil, f.schedErr
	}
	return f.scheduledAgents, nil
}

func (f *fakeMemory) GetScheduledAgent(ctx context.Context, id string) (*ScheduledAgent, error) {
	if f.schedErr != nil {
		return nil, f.schedErr
	}
	for i := range f.scheduledAgents {
		if f.scheduledAgents[i].ID == id {
			return &f.scheduledAgents[i], nil
		}
	}
	return nil, fmt.Errorf("memory 404 not_found: agent %s not found", id)
}

func (f *fakeMemory) CreateScheduledAgent(ctx context.Context, in *ScheduledAgent) (*ScheduledAgent, error) {
	if f.schedErr != nil {
		return nil, f.schedErr
	}
	in.ID = "sched-new-id"
	f.createdScheduled = append(f.createdScheduled, *in)
	f.scheduledAgents = append(f.scheduledAgents, *in)
	return in, nil
}

func (f *fakeMemory) UpdateScheduledAgent(ctx context.Context, id string, in *ScheduledAgent) (*ScheduledAgent, error) {
	if f.schedErr != nil {
		return nil, f.schedErr
	}
	in.ID = id
	f.updatedScheduled = in
	f.updatedSchedID = id
	return in, nil
}

func (f *fakeMemory) EnableScheduledAgent(ctx context.Context, id string) (*ScheduledAgent, error) {
	if f.schedErr != nil {
		return nil, f.schedErr
	}
	f.enabledSchedID = id
	for i := range f.scheduledAgents {
		if f.scheduledAgents[i].ID == id {
			f.scheduledAgents[i].Enabled = true
			return &f.scheduledAgents[i], nil
		}
	}
	return nil, fmt.Errorf("memory 404 not_found: agent %s not found", id)
}

func (f *fakeMemory) SetScheduledAgentEnabled(ctx context.Context, id string, enabled bool) (*ScheduledAgent, error) {
	if f.schedErr != nil {
		return nil, f.schedErr
	}
	f.setEnabledSchedID = id
	f.setEnabledSchedVal = &enabled
	for i := range f.scheduledAgents {
		if f.scheduledAgents[i].ID == id {
			f.scheduledAgents[i].Enabled = enabled
			return &f.scheduledAgents[i], nil
		}
	}
	return nil, fmt.Errorf("memory 404 not_found: agent %s not found", id)
}

func (f *fakeMemory) DeleteScheduledAgent(ctx context.Context, id string) error {
	if f.schedErr != nil {
		return f.schedErr
	}
	f.deletedSchedID = id
	return nil
}

func (f *fakeMemory) TriggerScheduledAgent(ctx context.Context, id string) (*TriggerResult, error) {
	if f.schedErr != nil {
		return nil, f.schedErr
	}
	f.triggeredSchedID = id
	if f.triggerResult != nil {
		return f.triggerResult, nil
	}
	runID := "run-" + id
	return &TriggerResult{Success: true, RunID: &runID}, nil
}

func (f *fakeMemory) ListScheduledAgentRuns(ctx context.Context, id string) ([]ScheduledAgentRun, error) {
	if f.schedErr != nil {
		return nil, f.schedErr
	}
	return f.scheduledRuns[id], nil
}

func (f *fakeMemory) TriggerAgent(ctx context.Context, agentID, prompt string, ctxValues map[string]string) (*TriggerResult, error) {
	if f.triggerAgentErr != nil {
		return nil, f.triggerAgentErr
	}
	if f.triggerAgentCalls != nil {
		f.triggerAgentCalls <- triggerAgentCall{AgentID: agentID, Prompt: prompt, Context: ctxValues}
	}
	return &TriggerResult{Success: true}, nil
}

func (f *fakeMemory) GetRunFull(ctx context.Context, runID string) (*AgentRunFull, error) {
	if f.runFullErr != nil {
		return nil, f.runFullErr
	}
	if f.runFull == nil {
		return nil, fmt.Errorf("memory 404 not_found: run %s not found", runID)
	}
	return f.runFull, nil
}

func (f *fakeMemory) GetRunQuestions(ctx context.Context, runID string) ([]AgentQuestionItem, error) {
	if f.runQuestionsErr != nil {
		return nil, f.runQuestionsErr
	}
	return f.runQuestions, nil
}

func (f *fakeMemory) GetAgentRun(ctx context.Context, runID string) (*AgentRun, error) {
	if f.agentRunErr != nil {
		return nil, f.agentRunErr
	}
	if r, ok := f.agentRuns[runID]; ok {
		return r, nil
	}
	return nil, fmt.Errorf("memory 404 not_found: run %s not found", runID)
}

func (f *fakeMemory) GetAgentSandboxConfig(ctx context.Context, id string) (*AgentSandboxConfig, error) {
	if f.sandboxConfig == nil {
		return &AgentSandboxConfig{}, nil
	}
	return f.sandboxConfig, nil
}

func (f *fakeMemory) SetAgentSandboxConfig(ctx context.Context, id string, cfg *AgentSandboxConfig) (*AgentSandboxConfig, error) {
	f.sandboxConfig = cfg
	return cfg, nil
}

func (f *fakeMemory) ListSandboxProviders(ctx context.Context) ([]SandboxProvider, error) {
	return f.providers, nil
}
func (f *fakeMemory) ListSandboxImages(ctx context.Context) ([]SandboxImage, error) {
	return f.images, nil
}

func (f *fakeMemory) ChatStream(ctx context.Context, req ChatRequest) (io.ReadCloser, error) {
	f.chatMsg = req.Message
	if f.chatErr != nil {
		return nil, f.chatErr
	}
	if f.chatStream != "" {
		return io.NopCloser(strings.NewReader(f.chatStream)), nil
	}
	return io.NopCloser(strings.NewReader(`data: {"type":"done"}` + "\n\n")), nil
}

func (f *fakeMemory) RespondQuestion(ctx context.Context, questionID, response, message string) (*RespondQuestionResult, error) {
	f.questionID = questionID
	f.questionResponse = response
	f.questionMessage = message
	return &RespondQuestionResult{QuestionID: questionID, ResumeRunID: "resume-1"}, nil
}

func (f *fakeMemory) CancelQuestion(ctx context.Context, questionID string) (*RespondQuestionResult, error) {
	f.questionCancelID = questionID
	return &RespondQuestionResult{QuestionID: questionID, ResumeRunID: "resume-1"}, nil
}

func (f *fakeMemory) ListToolApprovals(ctx context.Context) ([]ToolApprovalItem, error) {
	return f.approvals, nil
}

func (f *fakeMemory) ListAgentQuestions(ctx context.Context) ([]AgentQuestionItem, error) {
	return nil, nil
}

func (f *fakeMemory) ListConversations(ctx context.Context) (*ConversationList, error) {
	if f.convErr != nil {
		return nil, f.convErr
	}
	return &ConversationList{Conversations: f.convs, Total: len(f.convs)}, nil
}

func (f *fakeMemory) CreateObjectConversation(ctx context.Context, canonicalID, title, message string) (string, error) {
	if f.createConvErr != nil {
		return "", f.createConvErr
	}
	f.convCanonicalID = canonicalID
	f.convTitle = title
	f.convMessage = message
	return f.createConvID, nil
}

func (f *fakeMemory) GetConversation(ctx context.Context, id string) (*ConversationDetail, error) {
	if d, ok := f.details[id]; ok {
		return d, nil
	}
	return &ConversationDetail{ID: id, Messages: []Message{}}, nil
}

func (f *fakeMemory) GetConversationHistory(ctx context.Context, id string) (*ConversationHistory, error) {
	if f.histErr != nil {
		return nil, f.histErr
	}
	if h, ok := f.histories[id]; ok {
		return h, nil
	}
	return &ConversationHistory{ConversationID: id, Items: []json.RawMessage{}}, nil
}

func (f *fakeMemory) ListMCPServers(ctx context.Context) ([]MCPServer, error) {
	return f.servers, nil
}

func (f *fakeMemory) GetMCPServer(ctx context.Context, id string) (*MCPServer, error) {
	return &MCPServer{ID: id}, nil
}

func (f *fakeMemory) CreateMCPServer(ctx context.Context, in *MCPServer) (*MCPServer, error) {
	in.ID = "new-id"
	return in, nil
}

func (f *fakeMemory) UpdateMCPServer(ctx context.Context, id string, in *MCPServer) (*MCPServer, error) {
	in.ID = id
	return in, nil
}

func (f *fakeMemory) DeleteMCPServer(ctx context.Context, id string) error { return nil }

func (f *fakeMemory) SyncMCPServer(ctx context.Context, id string) error { return nil }

func (f *fakeMemory) InspectMCPServer(ctx context.Context, id string) (*MCPInspectResult, error) {
	return &MCPInspectResult{ServerID: id, Status: "ok"}, nil
}

func (f *fakeMemory) ListMCPServerTools(ctx context.Context, id string) ([]MCPTool, error) {
	return nil, nil
}

func (f *fakeMemory) SetMCPServerToolEnabled(ctx context.Context, id string, toolID string, enabled bool) error {
	return nil
}

// MCP share instances: the base fake returns zero values so unrelated tests
// compile; share-handler tests use mcpShareTestBackend (mcp_shares_handlers_test.go),
// which overrides these with real semantics.
func (f *fakeMemory) ListMCPShareInstances(ctx context.Context) ([]MCPShareInstance, error) {
	return nil, nil
}

func (f *fakeMemory) CreateMCPShareInstance(ctx context.Context, in *MCPShareInput) (*MCPShareCreated, error) {
	return &MCPShareCreated{MCPShareInstance: MCPShareInstance{ID: "share-new", Name: in.Name}}, nil
}

func (f *fakeMemory) GetMCPShareInstance(ctx context.Context, id string) (*MCPShareInstance, error) {
	return &MCPShareInstance{ID: id}, nil
}

func (f *fakeMemory) UpdateMCPShareInstance(ctx context.Context, id string, in *MCPShareInput) (*MCPShareInstance, error) {
	return &MCPShareInstance{ID: id, Name: in.Name}, nil
}

func (f *fakeMemory) RevokeMCPShareInstance(ctx context.Context, id string) error { return nil }

func (f *fakeMemory) RotateMCPShareInstance(ctx context.Context, id string) (*MCPShareCreated, error) {
	return &MCPShareCreated{}, nil
}

func (f *fakeMemory) ListMCPShareTools(ctx context.Context) ([]MCPShareTool, error) { return nil, nil }

// Per-agent MCP shares: stateful fake used by the agent MCP share handler
// tests (agent_mcp_shares_handlers_test.go). The list methods return the
// seeded agentShares; create/rotate carry the configured one-time secret.
func (f *fakeMemory) ListAgentMCPShares(ctx context.Context, agentID string) ([]AgentMCPShare, error) {
	if f.agentShareErr != nil {
		return nil, f.agentShareErr
	}
	return append([]AgentMCPShare(nil), f.agentShares...), nil
}

func (f *fakeMemory) ListProjectAgentMCPShares(ctx context.Context) ([]AgentMCPShare, error) {
	if f.agentShareErr != nil {
		return nil, f.agentShareErr
	}
	return append([]AgentMCPShare(nil), f.agentShares...), nil
}

func (f *fakeMemory) CreateAgentMCPShare(ctx context.Context, agentID string, in *AgentMCPShareInput) (*AgentMCPShareCreated, error) {
	f.lastAgentShareAgent = agentID
	f.lastAgentShareInput = in
	if f.agentShareCreateErr != nil {
		return nil, f.agentShareCreateErr
	}
	name := ""
	if in != nil {
		name = in.Name
	}
	return &AgentMCPShareCreated{
		Share:  AgentMCPShare{ID: "sh_new", AgentID: agentID, Name: name, Status: "active"},
		Secret: f.agentShareSecret(),
	}, nil
}

func (f *fakeMemory) RevokeAgentMCPShare(ctx context.Context, id string) error {
	f.lastRevokedAgentShare = id
	return nil
}

func (f *fakeMemory) RotateAgentMCPShare(ctx context.Context, id string) (*AgentMCPShareCreated, error) {
	f.lastRotatedAgentShare = id
	share := AgentMCPShare{ID: id, Status: "active"}
	if sh := agentMCPShareFindByID(f.agentShares, id); sh != nil {
		share = *sh
	}
	return &AgentMCPShareCreated{Share: share, Secret: f.agentShareSecret()}, nil
}

// agentShareSecret is the one-time secret the fake hands back: the configured
// token/URL, or a stable default token so tests need not set it.
func (f *fakeMemory) agentShareSecret() AgentMCPShareSecret {
	token := f.agentShareToken
	if token == "" {
		token = "tok_agent"
	}
	return AgentMCPShareSecret{Token: token, MCPURL: f.agentShareMCPURL}
}

func (f *fakeMemory) ListRelaySessions(ctx context.Context) ([]RelaySession, error) {
	if f.relaySessionsErr != nil {
		return nil, f.relaySessionsErr
	}
	return f.relaySessions, nil
}

func (f *fakeMemory) GetRelaySessionTools(ctx context.Context, instanceID string) ([]RelayTool, error) {
	if f.relayToolsErr != nil {
		return nil, f.relayToolsErr
	}
	return f.relayTools[instanceID], nil
}

func (f *fakeMemory) ListModels(ctx context.Context) ([]Model, error) {
	return []Model{{Provider: "deepseek", ModelName: "deepseek-v4-flash", DisplayName: "DeepSeek V4 Flash"}}, nil
}

func (f *fakeMemory) SearchMemories(ctx context.Context, query string) ([]Memory, error) {
	if f.memErr != nil {
		return nil, f.memErr
	}
	return f.memories, nil
}

func (f *fakeMemory) ListMemories(ctx context.Context) ([]Memory, error) {
	if f.memErr != nil {
		return nil, f.memErr
	}
	return f.memories, nil
}

func (f *fakeMemory) ListDocuments(ctx context.Context, cursor string) ([]Document, string, error) {
	if f.docErr != nil {
		return nil, "", f.docErr
	}
	return f.documents, "", nil
}

func (f *fakeMemory) GetDocument(ctx context.Context, id string) (*Document, error) {
	if f.docErr != nil {
		return nil, f.docErr
	}
	for i := range f.documents {
		if f.documents[i].ID == id {
			return &f.documents[i], nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (f *fakeMemory) DeleteDocument(ctx context.Context, id string) error {
	if f.deleteDocErr != nil {
		return f.deleteDocErr
	}
	f.deletedDocuments = append(f.deletedDocuments, id)
	kept := f.documents[:0]
	for _, d := range f.documents {
		if d.ID != id {
			kept = append(kept, d)
		}
	}
	f.documents = kept
	return nil
}

func (f *fakeMemory) ListChunks(ctx context.Context, documentID string) ([]Chunk, error) {
	if f.docErr != nil {
		return nil, f.docErr
	}
	return f.chunks, nil
}

func (f *fakeMemory) UploadDocument(ctx context.Context, filename string, r io.Reader) (UploadResult, error) {
	if f.docErr != nil {
		return UploadResult{}, f.docErr
	}
	f.uploaded = append(f.uploaded, filename)
	if f.uploadResult != nil {
		return *f.uploadResult, nil
	}
	return UploadResult{DocumentID: "new-doc-id"}, nil
}

func (f *fakeMemory) CreateExtractionJob(ctx context.Context, documentID string) (string, error) {
	if f.extractErr != nil {
		return "", f.extractErr
	}
	f.extracted = append(f.extracted, documentID)
	return "job-1", nil
}

func (f *fakeMemory) GetExtractionSummary(ctx context.Context, documentID string) (*ExtractionSummary, error) {
	if f.docErr != nil {
		return nil, f.docErr
	}
	return f.summary, nil
}

func (f *fakeMemory) GetGraphObject(ctx context.Context, objectID string) (*GraphObject, error) {
	if f.docErr != nil {
		return nil, f.docErr
	}
	for i := range f.objects {
		if f.objects[i].ID == objectID || f.objects[i].CanonicalID == objectID {
			return &f.objects[i], nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (f *fakeMemory) ListGraphObjects(ctx context.Context, branchID, typeFilter string, ids []string) ([]GraphObject, error) {
	if f.docErr != nil {
		return nil, f.docErr
	}
	var out []GraphObject
	for _, o := range f.objects {
		if o.BranchID != branchID {
			continue
		}
		if typeFilter != "" && o.Type != typeFilter {
			continue
		}
		out = append(out, o)
	}
	return out, nil
}

func (f *fakeMemory) ListGraphRelationships(ctx context.Context, branchID string) ([]GraphRelationship, error) {
	if f.docErr != nil {
		return nil, f.docErr
	}
	return f.relns, nil
}

func (f *fakeMemory) GetObjectEdges(ctx context.Context, objectID string) ([]GraphRelationship, error) {
	if f.docErr != nil {
		return nil, f.docErr
	}
	var edges []GraphRelationship
	for _, r := range f.relns {
		if r.SrcID == objectID || r.DstID == objectID {
			edges = append(edges, r)
		}
	}
	return edges, nil
}

func (f *fakeMemory) GetSimilarObjects(ctx context.Context, objectID string, limit int) ([]SimilarObject, error) {
	if f.similarErr != nil {
		return nil, f.similarErr
	}
	return f.similar, nil
}

func (f *fakeMemory) UpdateObject(ctx context.Context, id string, req *UpdateObjectRequest) (*GraphObject, error) {
	if f.updateObjectErr != nil {
		return nil, f.updateObjectErr
	}
	f.lastUpdateID = id
	f.lastUpdateReq = req
	if f.updateResult != nil {
		return f.updateResult, nil
	}
	return &GraphObject{ID: id}, nil
}

func (f *fakeMemory) CreateObject(ctx context.Context, req *CreateObjectRequest) (*GraphObject, error) {
	if f.createObjectErr != nil {
		return nil, f.createObjectErr
	}
	f.lastCreateObjectReq = req
	if f.createObjectResult != nil {
		return f.createObjectResult, nil
	}
	return &GraphObject{ID: "new-obj"}, nil
}

func (f *fakeMemory) CreateRelationship(ctx context.Context, req *CreateRelationshipRequest) error {
	f.lastCreateRelReq = req
	return f.createRelErr
}

func (f *fakeMemory) SearchObjectsFTS(ctx context.Context, query, typeFilter string) ([]GraphObject, error) {
	if f.ftsErr != nil {
		return nil, f.ftsErr
	}
	f.ftsQuery = query
	f.ftsTypeFilter = typeFilter
	return f.ftsResults, nil
}

func (f *fakeMemory) ListBranches(ctx context.Context) ([]Branch, error) {
	if f.docErr != nil {
		return nil, f.docErr
	}
	return f.branches, nil
}

func (f *fakeMemory) GetCompiledTypes(ctx context.Context) (*CompiledSchemaTypes, error) {
	if f.blueprintErr != nil {
		return nil, f.blueprintErr
	}
	if f.compiled == nil {
		return &CompiledSchemaTypes{}, nil
	}
	return f.compiled, nil
}

func (f *fakeMemory) ListBlueprintVersions(ctx context.Context, name string) ([]BlueprintRecord, error) {
	if f.blueprintErr != nil {
		return nil, f.blueprintErr
	}
	return f.blueprintVersions, nil
}

func (f *fakeMemory) ListBlueprints(ctx context.Context) ([]BlueprintRecord, error) {
	if f.blueprintErr != nil {
		return nil, f.blueprintErr
	}
	return f.blueprints, nil
}

func (f *fakeMemory) CreateBlueprint(ctx context.Context, req *createBlueprintRequest) (*BlueprintRecord, error) {
	if f.blueprintErr != nil {
		return nil, f.blueprintErr
	}
	f.createdBlueprint = req
	rec := &BlueprintRecord{ID: "bp-new", Name: req.Name, Version: req.Version, Status: "draft"}
	f.blueprint = rec
	return rec, nil
}

func (f *fakeMemory) GetBlueprint(ctx context.Context, id string) (*BlueprintRecord, error) {
	if f.blueprintErr != nil {
		return nil, f.blueprintErr
	}
	if rec, ok := f.blueprintByID[id]; ok {
		return rec, nil
	}
	if f.blueprint != nil && f.blueprint.ID == id {
		return f.blueprint, nil
	}
	return nil, fmt.Errorf("memory 404 not_found: blueprint not found")
}

func (f *fakeMemory) PublishBlueprint(ctx context.Context, id string) error {
	if f.blueprintErr != nil {
		return f.blueprintErr
	}
	f.publishedBlueprint = append(f.publishedBlueprint, id)
	return nil
}

func (f *fakeMemory) ApplyBlueprint(ctx context.Context, id string) (*BlueprintApplyResult, error) {
	if f.blueprintErr != nil {
		return nil, f.blueprintErr
	}
	f.appliedBlueprint = append(f.appliedBlueprint, id)
	if f.applyBlueprintRes != nil {
		return f.applyBlueprintRes, nil
	}
	return &BlueprintApplyResult{BlueprintID: id}, nil
}

func (f *fakeMemory) ListAppliedBlueprints(ctx context.Context) ([]AppliedBlueprint, error) {
	if f.blueprintErr != nil {
		return nil, f.blueprintErr
	}
	return f.appliedBlueprints, nil
}

func (f *fakeMemory) UnapplyBlueprint(ctx context.Context, id string) (*BlueprintUnapplyResult, error) {
	if f.blueprintErr != nil {
		return nil, f.blueprintErr
	}
	f.unappliedBlueprint = append(f.unappliedBlueprint, id)
	if f.unapplyBlueprintRes != nil {
		return f.unapplyBlueprintRes, nil
	}
	return &BlueprintUnapplyResult{BlueprintID: id, Status: "unapplied"}, nil
}

func (f *fakeMemory) ListAllSchemas(ctx context.Context) ([]SchemaInfo, error) {
	if f.blueprintErr != nil {
		return nil, f.blueprintErr
	}
	return f.schemas, nil
}

func (f *fakeMemory) GetSchema(ctx context.Context, packID string) (*BlueprintSchema, error) {
	if f.blueprintErr != nil {
		return nil, f.blueprintErr
	}
	if f.schema != nil {
		return f.schema, nil
	}
	return &BlueprintSchema{ID: packID, Name: packID}, nil
}

func (f *fakeMemory) CreateSchemaPack(ctx context.Context, req *SchemaPackWriteRequest) (*BlueprintSchema, error) {
	if f.schemaPackErr != nil {
		return nil, f.schemaPackErr
	}
	f.createdSchemaPack = req
	if f.createdSchemaPackRes != nil {
		return f.createdSchemaPackRes, nil
	}
	return &BlueprintSchema{ID: "pack-new", Name: req.Name, Version: req.Version}, nil
}

func (f *fakeMemory) UpdateSchemaPack(ctx context.Context, packID string, edit ObjectTypeEdit) error {
	if f.schemaPackErr != nil {
		return f.schemaPackErr
	}
	f.updatedSchemaPackID = packID
	f.updatedSchemaPackEdit = &edit
	return nil
}

func (f *fakeMemory) AssignSchemaPack(ctx context.Context, schemaID string) error {
	if f.schemaPackErr != nil {
		return f.schemaPackErr
	}
	f.assignedSchemaPacks = append(f.assignedSchemaPacks, schemaID)
	return nil
}

func (f *fakeMemory) CreateBlueprintVersion(ctx context.Context, id string, req *CreateBlueprintVersionRequest) (*BlueprintRecord, error) {
	if f.blueprintErr != nil {
		return nil, f.blueprintErr
	}
	if f.blueprintVersionErr != nil {
		return nil, f.blueprintVersionErr
	}
	f.createdBlueprintVersionID = id
	f.createdBlueprintVersion = req
	if f.createdBlueprintVersionRes != nil {
		return f.createdBlueprintVersionRes, nil
	}
	return &BlueprintRecord{ID: "bp-version", Name: "derived", Version: req.Version, Status: "draft"}, nil
}

func (f *fakeMemory) UpdateBlueprint(ctx context.Context, id string, req *UpdateBlueprintRequest) (*BlueprintRecord, error) {
	if f.blueprintErr != nil {
		return nil, f.blueprintErr
	}
	f.updatedBlueprintID = id
	f.updatedBlueprintReq = req
	if f.updatedBlueprintRes != nil {
		return f.updatedBlueprintRes, nil
	}
	return &BlueprintRecord{ID: id, Name: req.Name, Version: req.Version, Status: "draft"}, nil
}

func (f *fakeMemory) GetSchemaHistory(ctx context.Context) ([]SchemaHistoryItem, error) {
	if f.migrationErr != nil {
		return nil, f.migrationErr
	}
	return f.history, nil
}

func (f *fakeMemory) PreviewMigration(ctx context.Context, fromSchemaID, toSchemaID string) (*MigrationPreviewResult, error) {
	if f.migrationErr != nil {
		return nil, f.migrationErr
	}
	f.migrationFrom, f.migrationTo = fromSchemaID, toSchemaID
	if f.migrationPreview != nil {
		return f.migrationPreview, nil
	}
	return &MigrationPreviewResult{FromSchemaID: fromSchemaID, ToSchemaID: toSchemaID, OverallRiskLevel: "safe", CanProceed: true}, nil
}

func (f *fakeMemory) ExecuteMigration(ctx context.Context, fromSchemaID, toSchemaID string, force bool) (*MigrationExecuteResult, error) {
	if f.migrationErr != nil {
		return nil, f.migrationErr
	}
	f.migrationFrom, f.migrationTo, f.migrationForce = fromSchemaID, toSchemaID, force
	if f.migrationExecute != nil {
		return f.migrationExecute, nil
	}
	return &MigrationExecuteResult{FromSchemaID: fromSchemaID, ToSchemaID: toSchemaID}, nil
}

func (f *fakeMemory) RollbackMigration(ctx context.Context, toVersion string) (*MigrationRollbackResult, error) {
	if f.migrationErr != nil {
		return nil, f.migrationErr
	}
	f.migrationVersion = toVersion
	return &MigrationRollbackResult{ToVersion: toVersion}, nil
}

func (f *fakeMemory) CommitMigrationArchive(ctx context.Context, throughVersion string) (*MigrationCommitResult, error) {
	if f.migrationErr != nil {
		return nil, f.migrationErr
	}
	f.migrationVersion = throughVersion
	return &MigrationCommitResult{ThroughVersion: throughVersion}, nil
}

func (f *fakeMemory) GetMigrationJobStatus(ctx context.Context, jobID string) (*MigrationJob, error) {
	if f.migrationErr != nil {
		return nil, f.migrationErr
	}
	f.migrationJobID = jobID
	if f.migrationJob != nil {
		return f.migrationJob, nil
	}
	return &MigrationJob{ID: jobID, Status: "completed"}, nil
}

func (f *fakeMemory) ValidateSchemas(ctx context.Context) (*SchemaValidationResult, error) {
	if f.migrationErr != nil {
		return nil, f.migrationErr
	}
	if f.validateFn != nil {
		return f.validateFn(ctx)
	}
	if f.validation != nil {
		return f.validation, nil
	}
	return &SchemaValidationResult{}, nil
}

func (f *fakeMemory) ListSkills(ctx context.Context) ([]Skill, error) {
	if f.skillErr != nil {
		return nil, f.skillErr
	}
	return f.skills, nil
}

func (f *fakeMemory) GetSkill(ctx context.Context, id string) (*Skill, error) {
	if f.skillErr != nil {
		return nil, f.skillErr
	}
	for i := range f.skills {
		if f.skills[i].ID == id {
			return &f.skills[i], nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (f *fakeMemory) CreateSkill(ctx context.Context, name, description, content string) (*Skill, error) {
	if f.skillErr != nil {
		return nil, f.skillErr
	}
	sk := &Skill{ID: "new-skill-id", Name: name, Description: description, Content: content, Scope: "project"}
	f.createdSkills = append(f.createdSkills, *sk)
	return sk, nil
}

func (f *fakeMemory) UpdateSkill(ctx context.Context, id, description, content string) (*Skill, error) {
	if f.skillErr != nil {
		return nil, f.skillErr
	}
	f.updatedSkillID = id
	return &Skill{ID: id, Name: "updated", Description: description, Content: content}, nil
}

func (f *fakeMemory) DeleteSkill(ctx context.Context, id string) error {
	if f.skillErr != nil {
		return f.skillErr
	}
	f.deletedSkillID = id
	return nil
}

func (f *fakeMemory) ListBackups(ctx context.Context, orgID string) ([]Backup, int, error) {
	if f.backupErr != nil {
		return nil, 0, f.backupErr
	}
	return f.backups, len(f.backups), nil
}

func (f *fakeMemory) GetBackup(ctx context.Context, orgID, backupID string) (*Backup, error) {
	if f.backupErr != nil {
		return nil, f.backupErr
	}
	for i := range f.backups {
		if f.backups[i].ID == backupID {
			return &f.backups[i], nil
		}
	}
	return nil, fmt.Errorf("memory 404 not_found: backup %s not found", backupID)
}

func (f *fakeMemory) CreateBackup(ctx context.Context, includeDeleted, includeChat bool, retentionDays int) (*Backup, error) {
	if f.createdBackupErr != nil {
		return nil, f.createdBackupErr
	}
	f.createdBackupReq = CreateBackupRequest{
		IncludeDeleted: includeDeleted,
		IncludeChat:    includeChat,
		RetentionDays:  retentionDays,
	}
	if f.createdBackup != nil {
		return f.createdBackup, nil
	}
	return &Backup{ID: "b-new", Status: backupStatusCreating}, nil
}

func (f *fakeMemory) DownloadBackup(ctx context.Context, orgID, backupID string) (string, error) {
	if f.downloadBackupErr != nil {
		return "", f.downloadBackupErr
	}
	return f.downloadURL, nil
}

func (f *fakeMemory) DeleteBackup(ctx context.Context, orgID, backupID string) error {
	if f.deletedBackupErr != nil {
		return f.deletedBackupErr
	}
	f.deletedBackupID = backupID
	return nil
}

func (f *fakeMemory) GetCurrentProject(ctx context.Context) (*Project, error) {
	return f.project, f.projectErr
}

func (f *fakeMemory) UpdateProject(ctx context.Context, id string, in *ProjectUpdate) (*Project, error) {
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	f.updatedProject = in
	return f.project, nil
}

func (f *fakeMemory) ListAgentOverrides(ctx context.Context) ([]AgentOverrideEntry, error) {
	if f.overrideErr != nil {
		return nil, f.overrideErr
	}
	return f.overrides, nil
}

func (f *fakeMemory) SetAgentOverride(ctx context.Context, agentName string, in *AgentOverrideInput) error {
	if f.overrideErr != nil {
		return f.overrideErr
	}
	f.overrideAgent = agentName
	f.overrideInput = in
	return nil
}

func (f *fakeMemory) DeleteAgentOverride(ctx context.Context, agentName string) error {
	if f.overrideErr != nil {
		return f.overrideErr
	}
	f.overrideAgent = agentName
	return nil
}

func (f *fakeMemory) GetProjectSetting(ctx context.Context, category, key string) (*ProjectSetting, error) {
	if f.settingErr != nil {
		return nil, f.settingErr
	}
	if vals, ok := f.settings[category]; ok {
		if v, ok := vals[key]; ok {
			return &ProjectSetting{Category: category, Key: key, Value: v}, nil
		}
	}
	return nil, nil
}

func (f *fakeMemory) SetProjectSetting(ctx context.Context, category, key string, value map[string]any) error {
	if f.settingErr != nil {
		return f.settingErr
	}
	if f.settings == nil {
		f.settings = map[string]map[string]map[string]any{}
	}
	if f.settings[category] == nil {
		f.settings[category] = map[string]map[string]any{}
	}
	f.settings[category][key] = value
	f.settingWrites = append(f.settingWrites, projectSettingWrite{Category: category, Key: key, Value: value})
	f.lastSettingCat, f.lastSettingKey, f.lastSettingValue = category, key, value
	return nil
}

func (f *fakeMemory) DeleteProjectSetting(ctx context.Context, category, key string) error {
	if f.settingErr != nil {
		return f.settingErr
	}
	if f.settings != nil && f.settings[category] != nil {
		delete(f.settings[category], key)
	}
	f.lastDeletedCat, f.lastDeletedKey = category, key
	return nil
}

func (f *fakeMemory) GetProjectUsageSummary(ctx context.Context, since, until time.Time) (*UsageSummaryResponse, error) {
	if f.usageErr != nil {
		return nil, f.usageErr
	}
	if f.usageSummary == nil {
		return &UsageSummaryResponse{}, nil
	}
	return f.usageSummary, nil
}

func (f *fakeMemory) GetProjectUsageTimeSeries(ctx context.Context, granularity string, since, until time.Time) (*UsageTimeSeriesResponse, error) {
	if f.usageErr != nil {
		return nil, f.usageErr
	}
	if f.usageSeries == nil {
		return &UsageTimeSeriesResponse{}, nil
	}
	return f.usageSeries, nil
}

func (f *fakeMemory) ListProjectProviders(ctx context.Context) ([]ProjectProviderConfig, error) {
	if f.providerErr != nil {
		return nil, f.providerErr
	}
	return f.projectProviders, nil
}

func (f *fakeMemory) ListProviderModels(ctx context.Context, provider string) ([]ProviderSupportedModel, error) {
	if f.modelErr != nil {
		return nil, f.modelErr
	}
	return f.modelsByProvider[provider], nil
}

func (f *fakeMemory) ListPricing(ctx context.Context) ([]ProviderPricing, error) {
	if f.providerErr != nil {
		return nil, f.providerErr
	}
	return f.pricing, nil
}

func (f *fakeMemory) ListProjectPricingOverrides(ctx context.Context) ([]ProjectCustomPricing, error) {
	if f.providerErr != nil {
		return nil, f.providerErr
	}
	return f.pricingOverrides, nil
}

func (f *fakeMemory) UpsertProjectPricingOverride(ctx context.Context, provider, model string, rates modelPriceRates) (*ProjectCustomPricing, error) {
	if f.providerErr != nil {
		return nil, f.providerErr
	}
	o := &ProjectCustomPricing{ProjectID: "p1", Provider: provider, Model: model, modelPriceRates: rates}
	f.overrideWrites = append(f.overrideWrites, *o)
	// Reflect the write so a subsequent ListProjectPricingOverrides sees it.
	replaced := false
	for i := range f.pricingOverrides {
		if f.pricingOverrides[i].Provider == provider && f.pricingOverrides[i].Model == model {
			f.pricingOverrides[i] = *o
			replaced = true
			break
		}
	}
	if !replaced {
		f.pricingOverrides = append(f.pricingOverrides, *o)
	}
	return o, nil
}

func (f *fakeMemory) DeleteProjectPricingOverride(ctx context.Context, provider, model string) error {
	if f.providerErr != nil {
		return f.providerErr
	}
	f.deletedOverrides = append(f.deletedOverrides, providerModelKey(provider, model))
	kept := f.pricingOverrides[:0]
	for _, o := range f.pricingOverrides {
		if o.Provider != provider || o.Model != model {
			kept = append(kept, o)
		}
	}
	f.pricingOverrides = kept
	return nil
}

func (f *fakeMemory) UpsertProjectProviderConfig(ctx context.Context, provider string, in ProviderConfigInput) (*ProjectProviderConfig, error) {
	if f.providerErr != nil {
		return nil, f.providerErr
	}
	f.providerConfigInput = in
	f.lastProviderConfig = provider
	out := &ProjectProviderConfig{ProjectID: "p1", Provider: provider}
	out.GenerativeModel = in.GenerativeModel
	out.EmbeddingModel = in.EmbeddingModel
	out.BaseURL = in.BaseURL
	out.GCPProject = in.GCPProject
	out.Location = in.Location
	// Reflect the write so a subsequent ListProjectProviders sees it.
	replaced := false
	for i := range f.projectProviders {
		if f.projectProviders[i].Provider == provider {
			f.projectProviders[i] = *out
			replaced = true
			break
		}
	}
	if !replaced {
		f.projectProviders = append(f.projectProviders, *out)
	}
	return out, nil
}

func (f *fakeMemory) DeleteProjectProviderConfig(ctx context.Context, provider string) error {
	if f.providerErr != nil {
		return f.providerErr
	}
	f.deletedProviderConfigs = append(f.deletedProviderConfigs, provider)
	kept := f.projectProviders[:0]
	for _, p := range f.projectProviders {
		if p.Provider != provider {
			kept = append(kept, p)
		}
	}
	f.projectProviders = kept
	return nil
}

func (f *fakeMemory) TestProjectProvider(ctx context.Context, provider string) (*ProviderTestResult, error) {
	if f.providerErr != nil {
		return nil, f.providerErr
	}
	if f.providerTestResult != nil {
		return f.providerTestResult, nil
	}
	return &ProviderTestResult{Provider: provider, Model: "deepseek-v4-pro", Reply: "hello", LatencyMs: 42}, nil
}

func (f *fakeMemory) GetProjectModelConfig(ctx context.Context) (*ProjectModelConfig, error) {
	if f.providerErr != nil {
		return nil, f.providerErr
	}
	return f.modelConfig, nil
}

func (f *fakeMemory) UpsertProjectModelConfig(ctx context.Context, generativeModel, embeddingModel string) (*ProjectModelConfig, error) {
	if f.providerErr != nil {
		return nil, f.providerErr
	}
	cfg := &ProjectModelConfig{GenerativeModel: generativeModel, EmbeddingModel: embeddingModel}
	f.lastModelConfig = cfg
	f.modelConfig = cfg
	return cfg, nil
}

func newTestServer(f *fakeMemory) (*Server, *echo.Echo) {
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	s.hub = newConversationHub(s)
	e := echo.New()
	api := e.Group("/api")
	api.GET("/health", s.health)
	api.GET("/agents", s.listAgents)
	api.POST("/agents", s.createAgent)
	api.POST("/chat", s.chat)
	api.POST("/chat/questions/:questionId/respond", s.respondQuestion)
	api.POST("/chat/questions/:questionId/cancel", s.cancelQuestion)
	api.GET("/conversations", s.listConversations)
	api.GET("/conversations/:id/history", s.getConversationHistory)
	api.GET("/conversations/:id/events", s.conversationEvents)
	e.GET("/settings/approvals", s.uiApprovals)
	return s, e
}

func TestHealth(t *testing.T) {
	_, e := newTestServer(&fakeMemory{})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("health = %d", rec.Code)
	}
}

func TestListAgents(t *testing.T) {
	f := &fakeMemory{agents: []AgentDefinitionSummary{{ID: "1", Name: "diane"}, {ID: "2", Name: "memory"}}}
	_, e := newTestServer(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/agents", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got []AgentDefinitionSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 agents, got %d", len(got))
	}
}

func TestCreateAgent(t *testing.T) {
	f := &fakeMemory{}
	_, e := newTestServer(f)
	req := httptest.NewRequest(http.MethodPost, "/api/agents", strings.NewReader(`{"name":"bob","systemPrompt":"hi"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if len(f.created) != 1 || f.created[0].Name != "bob" {
		t.Fatalf("create not forwarded: %+v", f.created)
	}
}

func TestChatRequiresMessage(t *testing.T) {
	_, e := newTestServer(&fakeMemory{})
	req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestChatStreams(t *testing.T) {
	f := &fakeMemory{}
	_, e := newTestServer(f)
	req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(`{"message":"hi there"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if f.chatMsg != "hi there" {
		t.Fatalf("message not forwarded: %q", f.chatMsg)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("content-type = %q", ct)
	}
}

func TestChatMemoryErrorSurfaced(t *testing.T) {
	f := &fakeMemory{chatErr: errors.New("memory 503 provider_error: LLM provider credentials could not be decrypted")}
	_, e := newTestServer(f)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(`{"message":"hi"}`))
	req.Header.Set("Content-Type", "application/json")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusBadGateway, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "LLM provider credentials could not be decrypted") {
		t.Fatalf("real memory error not surfaced in body: %s", body)
	}
	if strings.Contains(body, "memory service unavailable") {
		t.Fatalf("generic error leaked in body: %s", body)
	}
}

func TestChatStreamsMarkdown(t *testing.T) {
	f := &fakeMemory{chatStream: `data: {"type":"token","token":"**hi**"}` + "\n\n" + `data: {"type":"done"}` + "\n\n"}
	_, e := newTestServer(f)
	req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(`{"message":"hi"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"type":"html"`) {
		t.Fatalf("no html event in body: %s", body)
	}
	if !strings.Contains(body, "<strong>hi</strong>") {
		t.Fatalf("markdown not rendered in body: %s", body)
	}
}

func TestChatStreamsAskUserQuestion(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"type":"meta","conversationId":"c1"}`,
		`data: {"type":"mcp_tool","tool":"ask_user","status":"started","result":{"question":"Scope?","options":[{"label":"Full","value":"full"}],"interaction_type":"buttons"}}`,
		`data: {"type":"mcp_tool","tool":"ask_user","status":"completed","result":{"question_id":"q1","status":"pausing","message":"sent"}}`,
		`data: {"type":"done"}`,
	}, "\n\n") + "\n\n"
	f := &fakeMemory{chatStream: stream}
	_, e := newTestServer(f)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(`{"message":"hi"}`))
	req.Header.Set("Content-Type", "application/json")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"type":"question"`) {
		t.Fatalf("no question event in body: %s", body)
	}
	if !strings.Contains(body, `"questionId":"q1"`) {
		t.Fatalf("questionId missing in body: %s", body)
	}
	if !strings.Contains(body, `"interactionType":"buttons"`) {
		t.Fatalf("interactionType missing in body: %s", body)
	}
	// the raw ask_user mcp_tool events are suppressed in favor of the card
	if strings.Contains(body, `"tool":"ask_user"`) {
		t.Fatalf("raw ask_user mcp_tool event leaked: %s", body)
	}
}

func TestChatStreamsAskUserErrorPassthrough(t *testing.T) {
	// ask_user finishing without a question_id (e.g. bad options) surfaces as a chip
	stream := `data: {"type":"mcp_tool","tool":"ask_user","status":"error","result":{"error":"options required"}}` + "\n\n" + `data: {"type":"done"}` + "\n\n"
	f := &fakeMemory{chatStream: stream}
	_, e := newTestServer(f)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(`{"message":"hi"}`))
	req.Header.Set("Content-Type", "application/json")
	e.ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, `"tool":"ask_user"`) {
		t.Fatalf("errored ask_user event should pass through: %s", body)
	}
	if strings.Contains(body, `"type":"question"`) {
		t.Fatalf("no question event expected on error: %s", body)
	}
}

func TestRespondQuestion(t *testing.T) {
	f := &fakeMemory{}
	_, e := newTestServer(f)
	req := httptest.NewRequest(http.MethodPost, "/api/chat/questions/q1/respond", strings.NewReader(`{"response":"full"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if f.questionID != "q1" || f.questionResponse != "full" {
		t.Fatalf("question not forwarded: id=%q response=%q", f.questionID, f.questionResponse)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"resumeRunId":"resume-1"`) {
		t.Fatalf("resumeRunId missing: %s", body)
	}
}

func TestRespondQuestionWithMessage(t *testing.T) {
	f := &fakeMemory{}
	_, e := newTestServer(f)
	req := httptest.NewRequest(http.MethodPost, "/api/chat/questions/q1/respond", strings.NewReader(`{"response":"reject","message":"too long"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if f.questionID != "q1" || f.questionResponse != "reject" || f.questionMessage != "too long" {
		t.Fatalf("not forwarded: id=%q response=%q message=%q", f.questionID, f.questionResponse, f.questionMessage)
	}
}

func TestCancelQuestion(t *testing.T) {
	f := &fakeMemory{}
	_, e := newTestServer(f)
	req := httptest.NewRequest(http.MethodPost, "/api/chat/questions/q1/cancel", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if f.questionCancelID != "q1" {
		t.Fatalf("cancel not forwarded: id=%q", f.questionCancelID)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"resumeRunId":"resume-1"`) {
		t.Fatalf("resumeRunId missing: %s", body)
	}
}

func TestApprovalsPage(t *testing.T) {
	f := &fakeMemory{approvals: []ToolApprovalItem{
		{ID: "a1", ToolName: "create_note", Decision: "approved", ArgsSummary: map[string]any{"title": "string"}},
		{ID: "a2", ToolName: "set_field", Decision: "rejected", Message: "too long"},
	}}
	_, e := newTestServer(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/approvals", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "create_note") || !strings.Contains(body, "approved") {
		t.Fatalf("approved row missing: %s", body)
	}
	if !strings.Contains(body, "set_field") || !strings.Contains(body, "rejected") || !strings.Contains(body, "too long") {
		t.Fatalf("reject message missing: %s", body)
	}
}

func TestChatStreamsApprovalPassthrough(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"type":"meta","conversationId":"c1"}`,
		`data: {"type":"approval","tool":"create_note","input":{"title":"draft"},"questionId":"q1"}`,
		`data: {"type":"done"}`,
	}, "\n\n") + "\n\n"
	f := &fakeMemory{chatStream: stream}
	_, e := newTestServer(f)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(`{"message":"hi"}`))
	req.Header.Set("Content-Type", "application/json")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"type":"approval"`) {
		t.Fatalf("approval event not passed through: %s", body)
	}
	if !strings.Contains(body, `"questionId":"q1"`) {
		t.Fatalf("questionId missing in body: %s", body)
	}
}

func TestHistoryHTMLInjection(t *testing.T) {
	f := &fakeMemory{histories: map[string]*ConversationHistory{
		"conv-1": {
			ConversationID: "conv-1",
			Items: []json.RawMessage{
				json.RawMessage(`{"kind":"message","role":"diane","content":{"text":"**hello**"}}`),
			},
		},
	}}
	_, e := newTestServer(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/conversations/conv-1/history", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"html"`) {
		t.Fatalf("no html field in body: %s", body)
	}
	if !strings.Contains(body, "<strong>hello</strong>") {
		t.Fatalf("markdown not rendered in body: %s", body)
	}
}

func TestHistoryHTMLStripsReasoning(t *testing.T) {
	f := &fakeMemory{histories: map[string]*ConversationHistory{
		"conv-3": {
			ConversationID: "conv-3",
			Items: []json.RawMessage{
				json.RawMessage(`{"kind":"message","role":"memory","content":{"text":"I should fetch the page.\nThe **title** is here"}}`),
			},
		},
	}}
	_, e := newTestServer(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/conversations/conv-3/history", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		Items []struct {
			Content struct {
				HTML string `json:"html"`
			} `json:"content"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Items) != 1 || out.Items[0].Content.HTML == "" {
		t.Fatalf("expected one message with html, got %+v", out)
	}
	h := out.Items[0].Content.HTML
	if !strings.Contains(h, "<strong>title</strong>") {
		t.Fatalf("markdown not rendered: %s", h)
	}
	if strings.Contains(h, "fetch the page") {
		t.Fatalf("chain-of-thought leaked into rendered html: %s", h)
	}
}

func TestHistoryFallsBackToMessages(t *testing.T) {
	f := &fakeMemory{
		details: map[string]*ConversationDetail{
			"conv-2": {
				ID: "conv-2",
				Messages: []Message{
					{Role: "user", Content: "The user really likes ice cream."},
				},
			},
		},
	}
	_, e := newTestServer(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/conversations/conv-2/history", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"kind":"message"`) {
		t.Fatalf("no synthesized message item in body: %s", body)
	}
	if !strings.Contains(body, "The user really likes ice cream.") {
		t.Fatalf("message text missing in body: %s", body)
	}
}

func TestRoomAllowed(t *testing.T) {
	f := &fakeMemory{agents: []AgentDefinitionSummary{{Name: "memory"}, {Name: "diane"}}}
	s := &Server{cfg: Config{}, memory: f}
	cases := []struct {
		room string
		want bool
	}{
		{"memory-ios-8f3a", true}, // agent-name prefix
		{"diane-mac-1c2d", true},  // agent-name prefix
		{"memory-legacy", true},   // legacy prefix
		{"evil-room", false},
		{"", false},
	}
	for _, c := range cases {
		got, err := s.roomAllowed(context.Background(), c.room)
		if err != nil {
			t.Fatalf("roomAllowed(%q): %v", c.room, err)
		}
		if got != c.want {
			t.Errorf("roomAllowed(%q) = %v, want %v", c.room, got, c.want)
		}
	}
}

func TestRoomAllowedExplicitList(t *testing.T) {
	s := &Server{cfg: Config{TokenAllowedRooms: "room-a, room-b"}, memory: &fakeMemory{}}
	if ok, _ := s.roomAllowed(context.Background(), "room-a"); !ok {
		t.Error("room-a should be allowed")
	}
	if ok, _ := s.roomAllowed(context.Background(), "room-c"); ok {
		t.Error("room-c should be denied")
	}
}

// --- scheduled agents handlers ---

// newSchedulesEcho registers the /api/schedules proxy routes against a fake
// backend.
func newSchedulesEcho(f *fakeMemory) (*Server, *echo.Echo) {
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	api := e.Group("/api")
	api.GET("/schedules", s.listScheduledAgents)
	api.POST("/schedules", s.createScheduledAgent)
	api.GET("/schedules/:id", s.getScheduledAgent)
	api.PATCH("/schedules/:id", s.updateScheduledAgent)
	api.POST("/schedules/:id/enable", s.enableScheduledAgent)
	api.DELETE("/schedules/:id", s.deleteScheduledAgent)
	api.POST("/schedules/:id/trigger", s.triggerScheduledAgent)
	api.GET("/schedules/:id/runs", s.listScheduledAgentRuns)
	return s, e
}

func TestListScheduledAgents(t *testing.T) {
	f := &fakeMemory{scheduledAgents: []ScheduledAgent{
		{ID: "a1", Name: "Daily briefing", CronSchedule: "0 0 8 * * *", Enabled: true},
		{ID: "a2", Name: "Nightly cleanup", CronSchedule: "0 0 2 * * *", Enabled: false},
	}}
	_, e := newSchedulesEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/schedules", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got []ScheduledAgent
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "Daily briefing" || got[1].Enabled {
		t.Errorf("agents = %+v", got)
	}
}

func TestListScheduledAgentsError(t *testing.T) {
	f := &fakeMemory{schedErr: fmt.Errorf("memory 503: service down")}
	_, e := newSchedulesEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/schedules", nil))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
}

func TestCreateScheduledAgent(t *testing.T) {
	f := &fakeMemory{}
	_, e := newSchedulesEcho(f)
	req := httptest.NewRequest(http.MethodPost, "/api/schedules",
		strings.NewReader(`{"name":"Daily briefing","prompt":"summarize","cronSchedule":"0 0 8 * * *","agentDefinitionId":"def-1"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if len(f.createdScheduled) != 1 {
		t.Fatalf("create not forwarded: %+v", f.createdScheduled)
	}
	c := f.createdScheduled[0]
	if c.Name != "Daily briefing" || c.CronSchedule != "0 0 8 * * *" {
		t.Errorf("created = %+v", c)
	}
	if c.StrategyType != "definition" || c.TriggerType != "schedule" {
		t.Errorf("strategy/trigger not defaulted: %+v", c)
	}
	if c.AgentDefinitionID == nil || *c.AgentDefinitionID != "def-1" {
		t.Errorf("agentDefinitionId = %v", c.AgentDefinitionID)
	}
	if !c.Enabled {
		t.Error("new agent should default to enabled")
	}
	if c.Prompt == nil || *c.Prompt != "summarize" {
		t.Errorf("prompt = %v", c.Prompt)
	}
}

func TestCreateScheduledAgentValidation(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"missing name", `{"cronSchedule":"0 0 8 * * *","agentDefinitionId":"def-1"}`},
		{"missing cron", `{"name":"Daily","agentDefinitionId":"def-1"}`},
		{"missing agentDefinitionId", `{"name":"Daily","cronSchedule":"0 0 8 * * *"}`},
		{"invalid body", `{`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &fakeMemory{}
			_, e := newSchedulesEcho(f)
			req := httptest.NewRequest(http.MethodPost, "/api/schedules", strings.NewReader(c.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400, body=%s", rec.Code, rec.Body.String())
			}
			if len(f.createdScheduled) != 0 {
				t.Errorf("agent created despite invalid body: %+v", f.createdScheduled)
			}
		})
	}
}

func TestGetScheduledAgent(t *testing.T) {
	f := &fakeMemory{scheduledAgents: []ScheduledAgent{{ID: "a1", Name: "Daily briefing"}}}
	_, e := newSchedulesEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/schedules/a1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got ScheduledAgent
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != "a1" {
		t.Errorf("agent = %+v", got)
	}
}

func TestGetScheduledAgentNotFound(t *testing.T) {
	f := &fakeMemory{}
	_, e := newSchedulesEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/schedules/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body=%s", rec.Code, rec.Body.String())
	}
}

func TestUpdateScheduledAgent(t *testing.T) {
	f := &fakeMemory{}
	_, e := newSchedulesEcho(f)
	req := httptest.NewRequest(http.MethodPatch, "/api/schedules/a1",
		strings.NewReader(`{"name":"Renamed","cronSchedule":"0 30 7 * * *","agentDefinitionId":"def-2","enabled":false}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if f.updatedSchedID != "a1" || f.updatedScheduled == nil {
		t.Fatalf("update not forwarded: id=%q agent=%+v", f.updatedSchedID, f.updatedScheduled)
	}
	if f.updatedScheduled.Name != "Renamed" || f.updatedScheduled.CronSchedule != "0 30 7 * * *" {
		t.Errorf("updated = %+v", f.updatedScheduled)
	}
	if f.updatedScheduled.Enabled {
		t.Error("enabled=false from the body should win")
	}
}

func TestUpdateScheduledAgentNotFound(t *testing.T) {
	f := &fakeMemory{schedErr: fmt.Errorf("memory 404 not_found: agent nope not found")}
	_, e := newSchedulesEcho(f)
	req := httptest.NewRequest(http.MethodPatch, "/api/schedules/nope",
		strings.NewReader(`{"name":"X","cronSchedule":"0 0 8 * * *","agentDefinitionId":"def-1"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestDeleteScheduledAgent(t *testing.T) {
	f := &fakeMemory{}
	_, e := newSchedulesEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/schedules/a1", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if f.deletedSchedID != "a1" {
		t.Errorf("delete not forwarded: id=%q", f.deletedSchedID)
	}
}

func TestEnableScheduledAgent(t *testing.T) {
	f := &fakeMemory{scheduledAgents: []ScheduledAgent{{ID: "a1", Name: "Daily", Enabled: false}}}
	_, e := newSchedulesEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/schedules/a1/enable", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if f.enabledSchedID != "a1" {
		t.Errorf("enable not forwarded: id=%q", f.enabledSchedID)
	}
	var got ScheduledAgent
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Enabled {
		t.Error("enable response should be enabled=true")
	}
}

func TestTriggerScheduledAgent(t *testing.T) {
	f := &fakeMemory{}
	_, e := newSchedulesEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/schedules/a1/trigger", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if f.triggeredSchedID != "a1" {
		t.Errorf("trigger not forwarded: id=%q", f.triggeredSchedID)
	}
	var got TriggerResult
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Success || got.RunID == nil || *got.RunID != "run-a1" {
		t.Errorf("trigger response = %+v", got)
	}
}

func TestTriggerScheduledAgentMemoryError(t *testing.T) {
	f := &fakeMemory{schedErr: fmt.Errorf("memory 503: boom")}
	_, e := newSchedulesEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/schedules/a1/trigger", nil))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
}

func TestListScheduledAgentRuns(t *testing.T) {
	f := &fakeMemory{scheduledRuns: map[string][]ScheduledAgentRun{
		"a1": {{ID: "r1", Status: "completed", StartedAt: "2026-08-26T08:00:00Z"}},
	}}
	_, e := newSchedulesEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/schedules/a1/runs", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got []ScheduledAgentRun
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "r1" {
		t.Errorf("runs = %+v", got)
	}
}

// --- orgs & projects (user tenancy) ---

func (f *fakeMemory) ListOrgs(ctx context.Context) ([]Org, error) {
	return f.orgs, nil
}

func (f *fakeMemory) ListProjects(ctx context.Context) ([]ProjectRef, error) {
	return f.projects, nil
}

func (f *fakeMemory) ListProjectsIncludingPending(ctx context.Context) ([]ProjectRef, error) {
	return f.projects, nil
}

func (f *fakeMemory) CreateProject(ctx context.Context, name, orgID string) (*ProjectRef, error) {
	f.createProjectID++
	p := ProjectRef{ID: fmt.Sprintf("proj-%d", f.createProjectID), Name: name, OrgID: orgID}
	f.projects = append(f.projects, p)
	f.createdProjects = append(f.createdProjects, p)
	return &p, nil
}

func (f *fakeMemory) DeleteProject(ctx context.Context, projectID string) error {
	if f.deleteProjectErr != nil {
		return f.deleteProjectErr
	}
	f.deletedProjectIDs = append(f.deletedProjectIDs, projectID)
	return nil
}

func (f *fakeMemory) RestoreProject(ctx context.Context, projectID string) error {
	if f.restoreProjectErr != nil {
		return f.restoreProjectErr
	}
	f.restoredProjectIDs = append(f.restoredProjectIDs, projectID)
	return nil
}

func (f *fakeMemory) TransferProject(ctx context.Context, projectID, destinationOrgID string) error {
	if f.transferProjectErr != nil {
		return f.transferProjectErr
	}
	f.transferCount++
	f.transferredID = projectID
	f.transferredToOrg = destinationOrgID
	return nil
}

func (f *fakeMemory) CreateOrg(ctx context.Context, name string) (*Org, error) {
	if f.createOrgErr != nil {
		return nil, f.createOrgErr
	}
	f.createOrgID++
	o := Org{ID: fmt.Sprintf("org-%d", f.createOrgID), Name: name}
	f.orgs = append(f.orgs, o)
	f.createdOrgs = append(f.createdOrgs, o)
	return &o, nil
}

func (f *fakeMemory) GetOrgsAndProjects(ctx context.Context) ([]OrgWithProjectsDto, error) {
	return f.orgsAndProjects, nil
}

func (f *fakeMemory) GetOrg(ctx context.Context, orgID string) (*Org, error) {
	if f.getOrgErr != nil {
		return nil, f.getOrgErr
	}
	for i := range f.orgs {
		if f.orgs[i].ID == orgID {
			return &f.orgs[i], nil
		}
	}
	return nil, nil
}

func (f *fakeMemory) UpdateOrg(ctx context.Context, orgID, name string) (*Org, error) {
	if f.updateOrgErr != nil {
		return nil, f.updateOrgErr
	}
	f.renamedOrgID = orgID
	f.renamedOrgName = name
	for i := range f.orgs {
		if f.orgs[i].ID == orgID {
			f.orgs[i].Name = name
			return &f.orgs[i], nil
		}
	}
	return &Org{ID: orgID, Name: name}, nil
}

func (f *fakeMemory) DeleteOrg(ctx context.Context, orgID string) error {
	if f.deleteOrgErr != nil {
		return f.deleteOrgErr
	}
	f.deletedOrgID = orgID
	return nil
}

func (f *fakeMemory) ListOrgMembers(ctx context.Context, orgID string) ([]OrgMemberDto, error) {
	return f.orgMembers, nil
}

func (f *fakeMemory) ListOrgToolSettings(ctx context.Context, orgID string) ([]OrgToolSettingDto, error) {
	if f.toolSettingErr != nil {
		return nil, f.toolSettingErr
	}
	return f.orgToolSettings, nil
}

func (f *fakeMemory) UpsertOrgToolSetting(ctx context.Context, orgID, toolName string, in UpsertOrgToolSettingInput) (*OrgToolSettingDto, error) {
	if f.toolSettingErr != nil {
		return nil, f.toolSettingErr
	}
	f.toolSettingOrg = orgID
	f.toolSettingTool = toolName
	f.toolSettingIn = in
	return &OrgToolSettingDto{
		ID:        "ts-new",
		OrgID:     orgID,
		ToolName:  toolName,
		Enabled:   in.Enabled,
		Config:    in.Config,
		CreatedAt: "2024-01-01T00:00:00Z",
		UpdatedAt: "2024-01-01T00:00:00Z",
	}, nil
}

func (f *fakeMemory) DeleteOrgToolSetting(ctx context.Context, orgID, toolName string) error {
	if f.toolSettingErr != nil {
		return f.toolSettingErr
	}
	f.toolSettingOrg = orgID
	f.toolSettingTool = toolName
	return nil
}

func (f *fakeMemory) ListMembers(ctx context.Context) ([]ProjectMemberDto, error) {
	return f.members, nil
}

func (f *fakeMemory) RemoveMember(ctx context.Context, userID string) error {
	if f.removeMemberErr != nil {
		return f.removeMemberErr
	}
	f.removedMember = userID
	return nil
}

func (f *fakeMemory) ListInvites(ctx context.Context) ([]SentInviteDto, error) {
	return f.sentInvites, nil
}

func (f *fakeMemory) CreateInvite(ctx context.Context, in CreateInviteDto) (*Invite, error) {
	if f.createInviteErr != nil {
		return nil, f.createInviteErr
	}
	f.createInviteID++
	inv := Invite{
		ID:             fmt.Sprintf("invite-%d", f.createInviteID),
		OrganizationID: in.OrgID,
		ProjectID:      in.ProjectID,
		Email:          in.Email,
		Role:           in.Role,
		Token:          fmt.Sprintf("tok-%d", f.createInviteID),
		Status:         "pending",
	}
	f.createdInvites = append(f.createdInvites, in)
	return &inv, nil
}

func (f *fakeMemory) AcceptInvite(ctx context.Context, token string) error {
	f.acceptedTokens = append(f.acceptedTokens, token)
	return nil
}

func (f *fakeMemory) ListPendingInvites(ctx context.Context) ([]PendingInviteDto, error) {
	return f.pendingInvites, nil
}

func (f *fakeMemory) DeclineInvite(ctx context.Context, inviteID string) error {
	if f.declineInviteErr != nil {
		return f.declineInviteErr
	}
	f.declinedInvite = inviteID
	return nil
}

func (f *fakeMemory) CancelInvite(ctx context.Context, inviteID string) error {
	f.canceledInvite = inviteID
	return nil
}

func (f *fakeMemory) SearchUsers(ctx context.Context, email string) ([]UserSearchResultDto, error) {
	if f.searchErr != nil {
		return nil, f.searchErr
	}
	f.lastSearchTerm = email
	return f.searchUsers, nil
}

func (f *fakeMemory) GetProfile(ctx context.Context) (*UserProfileDto, error) {
	return f.profile, nil
}

func (f *fakeMemory) UpdateProfile(ctx context.Context, in UpdateUserProfileDto) (*UserProfileDto, error) {
	f.updatedProfile = in
	if f.profile != nil {
		p := *f.profile
		if in.FirstName != "" {
			p.FirstName = in.FirstName
		}
		if in.LastName != "" {
			p.LastName = in.LastName
		}
		if in.DisplayName != "" {
			p.DisplayName = in.DisplayName
		}
		if in.PhoneE164 != "" {
			p.PhoneE164 = in.PhoneE164
		}
		f.profile = &p
		return f.profile, nil
	}
	return &UserProfileDto{
		ID:          "u1",
		FirstName:   in.FirstName,
		LastName:    in.LastName,
		DisplayName: in.DisplayName,
		PhoneE164:   in.PhoneE164,
	}, nil
}

func (f *fakeMemory) UploadAvatar(ctx context.Context, filename string, r io.Reader) (*UserProfileDto, error) {
	if f.avatarErr != nil {
		return nil, f.avatarErr
	}
	f.avatarUploads = append(f.avatarUploads, filename)
	if _, err := io.Copy(io.Discard, r); err != nil {
		return nil, err
	}
	dto := &UserProfileDto{ID: "u1"}
	if f.avatarProfile != nil {
		*dto = *f.avatarProfile
	}
	dto.AvatarUrl = "/api/user/avatar?v=key-new"
	f.avatarProfile = dto
	if f.profile != nil {
		p := *f.profile
		p.AvatarUrl = dto.AvatarUrl
		f.profile = &p
	}
	return dto, nil
}

func (f *fakeMemory) DeleteAvatar(ctx context.Context) (*UserProfileDto, error) {
	if f.avatarErr != nil {
		return nil, f.avatarErr
	}
	dto := &UserProfileDto{ID: "u1"}
	if f.avatarProfile != nil {
		*dto = *f.avatarProfile
	}
	dto.AvatarUrl = ""
	f.avatarProfile = dto
	if f.profile != nil {
		p := *f.profile
		p.AvatarUrl = ""
		f.profile = &p
	}
	return dto, nil
}

func (f *fakeMemory) GetAvatar(ctx context.Context) (io.ReadCloser, string, error) {
	if f.avatarErr != nil {
		return nil, "", f.avatarErr
	}
	if len(f.avatarBody) == 0 {
		return nil, "", fmt.Errorf("memory 404: avatar not found")
	}
	contentType := f.avatarContentType
	if contentType == "" {
		contentType = "image/png"
	}
	return io.NopCloser(bytes.NewReader(f.avatarBody)), contentType, nil
}

// --- API-token fake methods (project + account) ---
//
// The fake is a small stateful store so handler flows exercise create → list,
// revoke → relist, regenerate → replacement the way the real Memory service
// behaves. Secrets are kept per token id in apiTokenSecrets and surfaced only
// on create/regenerate (always) and get (when the plaintext flag is set),
// mirroring the encryption-configuration gate.

const fakeAPITokenCreatedAt = "2026-09-01T10:00:00Z"

// fakeAPITokenSecret builds the deterministic plaintext for a token.
func fakeAPITokenSecret(name, id string) string {
	return "emt_" + name + "_" + id + "_" + strings.Repeat("x", 40)
}

// fakeAPITokenPrefix derives the display prefix (first 12 chars) from a secret.
func fakeAPITokenPrefix(secret string) string {
	if len(secret) < 12 {
		return secret
	}
	return secret[:12]
}

// fakeAPITokenFind returns the index of a token in a store by id, or -1.
func fakeAPITokenFind(tokens []APIToken, id string) int {
	for i := range tokens {
		if tokens[i].ID == id {
			return i
		}
	}
	return -1
}

func (f *fakeMemory) ListAPITokens(ctx context.Context) ([]APIToken, error) {
	if f.apiTokenErr != nil {
		return nil, f.apiTokenErr
	}
	return append([]APIToken(nil), f.apiTokens...), nil
}

func (f *fakeMemory) CreateAPIToken(ctx context.Context, name string, scopes []string) (*APITokenCreateResponse, error) {
	if f.apiTokenErr != nil {
		return nil, f.apiTokenErr
	}
	for _, t := range f.apiTokens {
		if t.Name == name {
			return nil, fmt.Errorf("memory 409 token_name_exists: an API token named %q already exists", name)
		}
	}
	if f.apiTokenSecrets == nil {
		f.apiTokenSecrets = map[string]string{}
	}
	f.apiTokenSeq++
	id := fmt.Sprintf("tok-%d", f.apiTokenSeq)
	secret := fakeAPITokenSecret(name, id)
	f.apiTokenSecrets[id] = secret
	tok := APIToken{
		ID:          id,
		Name:        name,
		TokenPrefix: fakeAPITokenPrefix(secret),
		Scopes:      append([]string(nil), scopes...),
		CreatedAt:   fakeAPITokenCreatedAt,
	}
	f.apiTokens = append(f.apiTokens, tok)
	f.lastAPITokenID = id
	f.lastAPITokenScopes = append([]string(nil), scopes...)
	out := APITokenCreateResponse{APIToken: tok}
	out.Token = secret
	return &out, nil
}

func (f *fakeMemory) GetAPIToken(ctx context.Context, tokenID string) (*APIToken, error) {
	if f.apiTokenErr != nil {
		return nil, f.apiTokenErr
	}
	i := fakeAPITokenFind(f.apiTokens, tokenID)
	if i < 0 {
		return nil, fmt.Errorf("memory 404 not_found: API token %q not found", tokenID)
	}
	out := f.apiTokens[i]
	if f.apiTokenRevealPlaintext {
		out.Token = f.apiTokenSecrets[tokenID]
	}
	return &out, nil
}

func (f *fakeMemory) UpdateAPITokenScopes(ctx context.Context, tokenID string, scopes []string) (*APIToken, error) {
	if f.apiTokenErr != nil {
		return nil, f.apiTokenErr
	}
	i := fakeAPITokenFind(f.apiTokens, tokenID)
	if i < 0 {
		return nil, fmt.Errorf("memory 404 not_found: API token %q not found", tokenID)
	}
	if f.apiTokens[i].IsRevoked {
		return nil, fmt.Errorf("memory 409 token_already_revoked: cannot update a revoked API token")
	}
	f.apiTokens[i].Scopes = append([]string(nil), scopes...)
	f.lastAPITokenID = tokenID
	f.lastAPITokenScopes = append([]string(nil), scopes...)
	out := f.apiTokens[i]
	return &out, nil
}

func (f *fakeMemory) RevokeAPIToken(ctx context.Context, tokenID string) error {
	if f.apiTokenErr != nil {
		return f.apiTokenErr
	}
	i := fakeAPITokenFind(f.apiTokens, tokenID)
	if i < 0 {
		return fmt.Errorf("memory 404 not_found: API token %q not found", tokenID)
	}
	if f.apiTokens[i].IsRevoked {
		return fmt.Errorf("memory 409 token_already_revoked: API token already revoked")
	}
	f.apiTokens[i].IsRevoked = true
	f.lastAPITokenID = tokenID
	return nil
}

func (f *fakeMemory) RegenerateAPIToken(ctx context.Context, tokenID string) (*APITokenCreateResponse, error) {
	if f.apiTokenErr != nil {
		return nil, f.apiTokenErr
	}
	i := fakeAPITokenFind(f.apiTokens, tokenID)
	if i < 0 {
		return nil, fmt.Errorf("memory 404 not_found: API token %q not found", tokenID)
	}
	old := f.apiTokens[i]
	if old.IsRevoked {
		return nil, fmt.Errorf("memory 409 token_already_revoked: cannot regenerate a revoked API token")
	}
	f.apiTokens[i].IsRevoked = true
	f.apiTokenSeq++
	id := fmt.Sprintf("tok-%d", f.apiTokenSeq)
	secret := fakeAPITokenSecret(old.Name, id)
	f.apiTokenSecrets[id] = secret
	tok := APIToken{
		ID:          id,
		Name:        old.Name,
		TokenPrefix: fakeAPITokenPrefix(secret),
		Scopes:      append([]string(nil), old.Scopes...),
		CreatedAt:   fakeAPITokenCreatedAt,
	}
	f.apiTokens = append(f.apiTokens, tok)
	f.lastAPITokenID = id
	f.lastAPITokenScopes = append([]string(nil), old.Scopes...)
	out := APITokenCreateResponse{APIToken: tok}
	out.Token = secret
	return &out, nil
}

func (f *fakeMemory) ListAccountAPITokens(ctx context.Context) ([]APIToken, error) {
	if f.accountAPITokenErr != nil {
		return nil, f.accountAPITokenErr
	}
	return append([]APIToken(nil), f.accountAPITokens...), nil
}

func (f *fakeMemory) CreateAccountAPIToken(ctx context.Context, name string, scopes []string) (*APITokenCreateResponse, error) {
	if f.accountAPITokenErr != nil {
		return nil, f.accountAPITokenErr
	}
	for _, t := range f.accountAPITokens {
		if t.Name == name {
			return nil, fmt.Errorf("memory 409 token_name_exists: an API token named %q already exists", name)
		}
	}
	if f.apiTokenSecrets == nil {
		f.apiTokenSecrets = map[string]string{}
	}
	f.accountAPITokenSeq++
	id := fmt.Sprintf("acct-%d", f.accountAPITokenSeq)
	secret := fakeAPITokenSecret(name, id)
	f.apiTokenSecrets[id] = secret
	tok := APIToken{
		ID:          id,
		Name:        name,
		TokenPrefix: fakeAPITokenPrefix(secret),
		Scopes:      append([]string(nil), scopes...),
		CreatedAt:   fakeAPITokenCreatedAt,
	}
	f.accountAPITokens = append(f.accountAPITokens, tok)
	f.lastAccountAPITokenID = id
	f.lastAccountAPITokenScopes = append([]string(nil), scopes...)
	out := APITokenCreateResponse{APIToken: tok}
	out.Token = secret
	return &out, nil
}

func (f *fakeMemory) GetAccountAPIToken(ctx context.Context, tokenID string) (*APIToken, error) {
	if f.accountAPITokenErr != nil {
		return nil, f.accountAPITokenErr
	}
	i := fakeAPITokenFind(f.accountAPITokens, tokenID)
	if i < 0 {
		return nil, fmt.Errorf("memory 404 not_found: API token %q not found", tokenID)
	}
	out := f.accountAPITokens[i]
	if f.accountAPITokenReveal {
		out.Token = f.apiTokenSecrets[tokenID]
	}
	return &out, nil
}

func (f *fakeMemory) UpdateAccountAPITokenScopes(ctx context.Context, tokenID string, scopes []string) (*APIToken, error) {
	if f.accountAPITokenErr != nil {
		return nil, f.accountAPITokenErr
	}
	i := fakeAPITokenFind(f.accountAPITokens, tokenID)
	if i < 0 {
		return nil, fmt.Errorf("memory 404 not_found: API token %q not found", tokenID)
	}
	if f.accountAPITokens[i].IsRevoked {
		return nil, fmt.Errorf("memory 409 token_already_revoked: cannot update a revoked API token")
	}
	f.accountAPITokens[i].Scopes = append([]string(nil), scopes...)
	f.lastAccountAPITokenID = tokenID
	f.lastAccountAPITokenScopes = append([]string(nil), scopes...)
	out := f.accountAPITokens[i]
	return &out, nil
}

func (f *fakeMemory) RevokeAccountAPIToken(ctx context.Context, tokenID string) error {
	if f.accountAPITokenErr != nil {
		return f.accountAPITokenErr
	}
	i := fakeAPITokenFind(f.accountAPITokens, tokenID)
	if i < 0 {
		return fmt.Errorf("memory 404 not_found: API token %q not found", tokenID)
	}
	if f.accountAPITokens[i].IsRevoked {
		return fmt.Errorf("memory 409 token_already_revoked: API token already revoked")
	}
	f.accountAPITokens[i].IsRevoked = true
	f.lastAccountAPITokenID = tokenID
	return nil
}

func (f *fakeMemory) RegenerateAccountAPIToken(ctx context.Context, tokenID string) (*APITokenCreateResponse, error) {
	if f.accountAPITokenErr != nil {
		return nil, f.accountAPITokenErr
	}
	i := fakeAPITokenFind(f.accountAPITokens, tokenID)
	if i < 0 {
		return nil, fmt.Errorf("memory 404 not_found: API token %q not found", tokenID)
	}
	old := f.accountAPITokens[i]
	if old.IsRevoked {
		return nil, fmt.Errorf("memory 409 token_already_revoked: cannot regenerate a revoked API token")
	}
	f.accountAPITokens[i].IsRevoked = true
	f.accountAPITokenSeq++
	id := fmt.Sprintf("acct-%d", f.accountAPITokenSeq)
	secret := fakeAPITokenSecret(old.Name, id)
	f.apiTokenSecrets[id] = secret
	tok := APIToken{
		ID:          id,
		Name:        old.Name,
		TokenPrefix: fakeAPITokenPrefix(secret),
		Scopes:      append([]string(nil), old.Scopes...),
		CreatedAt:   fakeAPITokenCreatedAt,
	}
	f.accountAPITokens = append(f.accountAPITokens, tok)
	f.lastAccountAPITokenID = id
	f.lastAccountAPITokenScopes = append([]string(nil), old.Scopes...)
	out := APITokenCreateResponse{APIToken: tok}
	out.Token = secret
	return &out, nil
}
