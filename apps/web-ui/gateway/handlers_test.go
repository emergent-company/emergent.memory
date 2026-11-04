package main

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
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

// recordCatalog bumps the named catalog fetch counter (see catalogCalls).
// Guarded by catalogMu: some handlers (e.g. uiAgents) fetch catalogs from
// concurrent goroutines.
func (f *fakeMemory) recordCatalog(method string) {
	f.catalogMu.Lock()
	defer f.catalogMu.Unlock()
	if f.catalogCalls == nil {
		f.catalogCalls = map[string]int{}
	}
	f.catalogCalls[method]++
}

// catalogCallCount reports how many times method was fetched.
func (f *fakeMemory) catalogCallCount(method string) int {
	f.catalogMu.Lock()
	defer f.catalogMu.Unlock()
	return f.catalogCalls[method]
}

// definitionFetches returns a copy of the ids passed to GetAgentDefinition, in
// order, so a test can assert a request count without racing the handler.
func (f *fakeMemory) definitionFetches() []string {
	f.defMu.Lock()
	defer f.defMu.Unlock()
	return append([]string(nil), f.defFetches...)
}

// modelTestCall records one TestProjectModel invocation.
type modelTestCall struct {
	Provider  string
	Model     string
	ModelType string
}

// fakeMemory is an in-memory MemoryBackend for tests.
type fakeMemory struct {
	orgPresence     *PresenceDto
	projectPresence *PresenceDto
	agents          []AgentDefinitionSummary
	defs            map[string]*AgentDefinition // full definitions keyed by id
	// defFetches records ids passed to GetAgentDefinition, in order, so tests
	// can pin how many definition requests a render costs (#2181). Guarded by
	// defMu: chatModelWarnings fetches definitions from concurrent goroutines.
	defMu      sync.Mutex
	defFetches []string
	// defErrs forces GetAgentDefinition to fail for an id, so a test can pin the
	// unloadable-definition path (the fallback below otherwise synthesises a
	// definition for every listed agent).
	defErrs map[string]error
	// processing problems (needs-attention panel)
	problems             *ProcessingProblemsReport
	problemsErr          error
	retriedJobIDs        []string
	retriedParsingJobIDs []string
	retryJobErr          error
	bulkRetried          int
	// bulkIncludeNonRetryable records the flag each bulk retry arrived with, so
	// a test can pin that the panel's explicit "after fixing the cause" button
	// is the only caller that sets it.
	bulkIncludeNonRetryable []bool
	embeddingsRetriggered   int
	// embeddingIncludeNonRetryable records the flag each project embeddings
	// retrigger arrived with. The server filters retryable failures by default,
	// so a handler that drops this flag makes the panel's after-fixing action a
	// no-op on the configuration-only project it exists for.
	embeddingIncludeNonRetryable []bool
	convs                        []Conversation
	// session lifecycle (chat-conversation-lifecycle).
	convIncludeArchived bool     // last includeArchived flag received by ListConversations
	archivedConvs       []string // conversation ids passed to ArchiveConversation, in order
	unarchivedConvs     []string // conversation ids passed to UnarchiveConversation, in order
	deletedConvs        []string // conversation ids passed to DeleteConversation, in order
	archiveConvErr      error    // ArchiveConversation failure
	unarchiveConvErr    error    // UnarchiveConversation failure
	deleteConvErr       error    // DeleteConversation failure
	servers             []MCPServer
	mcpServerCreateErr  error // CreateMCPServer failure

	// owner share-session surface (ListShareSessionsByProject /
	// GetShareSessionTranscript).
	shareSessions      []ShareOwnerSession
	shareMessages      []ShareMessage
	shareSessionsErr   error
	shareTranscriptErr error

	// public agent-share surface (share.go handlers).
	shareDetail           *ShareSessionDetail // ShareGetSession result
	shareDetailErr        error
	sharePublicSessions   []ShareSession // ShareListSessions result
	sharePublicListFilter string         // last filter received by ShareListSessions
	shareUnarchivedIDs    []string       // ids received by ShareUnarchiveSession
	shareDeletedIDs       []string       // ids received by ShareDeleteSession
	shareCancelledIDs     []string       // question ids received by ShareCancelApproval
	shareCancelledSess    []string       // session ids received by ShareCancelApproval

	// catalogCalls counts backend catalog fetches (keyed by method name) so
	// tests can assert the settings loader only fetches a section's catalogs.
	// Writes go through recordCatalog; reads through catalogCallCount.
	catalogCalls map[string]int
	catalogMu    sync.Mutex

	// MCP relay (external nodes): relaySessions is returned by
	// ListRelaySessions; relayTools holds per-instance tool lists (an empty
	// value serves a node with no tools). relaySessionsErr /
	// relayToolsErr simulate whole-endpoint failures.
	relaySessions    []RelaySession
	relaySessionsErr error
	relayTools       map[string][]RelayTool
	relayToolsErr    error                           // failure for every GetRelaySessionTools call
	histories        map[string]*ConversationHistory // history per conversation id
	details          map[string]*ConversationDetail  // detail per conversation id
	detailErr        error                           // GetConversation failure
	approvalsErr     error                           // ListToolApprovals failure
	createConvID     string                          // id returned by CreateObjectConversation
	createConvErr    error                           // CreateObjectConversation failure
	convCanonicalID  string                          // last canonicalID passed to CreateObjectConversation
	convTitle        string                          // last title passed to CreateObjectConversation
	convMessage      string                          // last message passed to CreateObjectConversation
	created          []AgentDefinition
	updatedAgent     *AgentDefinition // last agent passed to UpdateAgentDefinition
	chatMsg          string
	chatStream       string        // SSE body returned by ChatStream when non-empty
	chatStreamReader io.ReadCloser // overrides ChatStream's return body when non-nil
	chatErr          error         // ChatStream failure
	questionID       string        // question id received via RespondQuestion
	questionResponse string        // response received via RespondQuestion
	questionMessage  string        // optional reject message received via RespondQuestion
	questionCancelID string        // question id received via CancelQuestion
	approvals        []ToolApprovalItem
	questions        []AgentQuestionItem
	convErr          error // ListConversations failure
	histErr          error // GetConversationHistory failure
	documents        []Document
	chunks           []Chunk
	docErr           error // ListDocuments/GetDocument/ListChunks failure
	// documentsTotal/documentStatusCounts/documentsNextCursor are what the
	// listing returns beyond the rows themselves. Zero values mean "one page,
	// no more" — what a single-page project looks like.
	documentsTotal         int
	documentStatusCounts   DocumentStatusCounts
	documentsNextCursor    string
	lastDocumentListParams DocumentListParams // params of the last ListDocuments call
	docSourceCounts        []DocumentSourceCount
	docSourceCountsErr     error

	// schema discovery (Schema page card + per-document trigger)
	discoveryJobs      []discoveryJob
	discoveryErr       error
	discoveryStarted   int      // project-wide starts
	discoveryDocStarts []string // document ids passed to StartDocumentDiscovery
	discoveryRetries   []string // job ids passed to RetryDiscoveryJob
	discoveryCancels   []string // job ids passed to CancelDiscoveryJob
	discoveryStartErr  error
	extractErr         error               // CreateExtractionJob failure
	uploaded           []string            // filenames received via UploadDocument
	uploadResult       *UploadResult       // non-nil overrides the UploadDocument result
	extracted          []string            // document ids passed to CreateExtractionJob
	deletedDocuments   []string            // document ids passed to DeleteDocument, in order
	deleteDocErr       error               // DeleteDocument failure
	summary            *ExtractionSummary  // returned by GetExtractionSummary
	docContent         *string             // returned by GetDocumentContent (nil → "")
	objects            []GraphObject       // returned by ListExtractionObjects
	lookupObjects      []GraphObject       // returned by LookupGraphObjectByKey
	lookupErr          error               // LookupGraphObjectByKey failure
	lastLookupType     string              // last typeFilter passed to LookupGraphObjectByKey
	lastLookupKey      string              // last key passed to LookupGraphObjectByKey
	relns              []GraphRelationship // returned by ListRelationships
	branches           []Branch            // returned by ListBranches
	extractionJobs     []ExtractionJob     // returned by ListExtractionJobs
	extractionJobsErr  error               // ListProjectExtractionJobs failure
	extractionJob      *ExtractionJob      // returned by GetExtractionJob (rich DTO with spans/cost/error details)
	downloadDocBody    []byte              // raw bytes returned by DownloadDocument
	downloadDocCT      string              // Content-Type returned by DownloadDocument
	downloadDocErr     error               // DownloadDocument failure
	// Document version history (GET /api/documents/:id/versions).
	documentVersions    []DocumentVersion
	documentVersionsErr error
	// Data-source integrations (see memory_datasources.go).
	dsProviders       []DSProvider
	dsSchema          *DSProviderSchema
	dsSourceTypes     []DSSourceType
	dsIntegrations    []DSIntegration
	dsCredentials     []DSOAuthCredentialDescription
	dsErr             error // failure for any DS read/list method
	dsCreateErr       error
	dsUpdateErr       error
	dsDeleteErr       error
	dsTestConfigResp  *DSTestConnectionResponse
	dsTestConfigErr   error
	dsTestConnResp    *DSTestConnectionResponse
	dsTestConnErr     error
	dsTriggerResp     *DSTriggerSyncResponse
	dsTriggerErr      error
	dsSyncJobs        map[string][]DSSyncJob // jobs per source id
	dsSyncJobsErr     error
	dsCancelResp      *DSCancelSyncJobResponse
	dsCancelErr       error
	dsCreatedReq      *DSCreateIntegrationRequest // last create request forwarded
	dsUpdatedReq      *DSUpdateIntegrationRequest // last update request forwarded
	dsUpdatedID       string                      // last id passed to UpdateDSIntegration
	dsDeletedID       string                      // last id passed to DeleteDSIntegration
	dsLastTestConfig  *DSTestConfigRequest        // last test-config request forwarded
	dsLastTestConnID  string                      // last id passed to TestDSConnection
	dsLastTriggerID   string                      // last id passed to TriggerDSSync
	dsLastCancelJobID string                      // last job id passed to CancelDSSyncJob
	// OAuth connect + org OAuth-client config (see memory_datasources.go).
	dsOAuthAuthorizeResp         *DSOAuthAuthorizeResponse // returned by StartDSOAuth
	dsOAuthAuthorizeErr          error                     // StartDSOAuth failure
	dsLastOAuthProvider          string                    // last providerType passed to StartDSOAuth
	dsLastOAuthConfig            map[string]any            // last config passed to StartDSOAuth
	dsLastOAuthReconnectSourceID string                    // last reconnect source id passed to StartDSOAuth
	dsOrgOAuthClients            []DSOAuthClientConfig     // returned by List/GetDSOAuthClient
	dsOrgOAuthClientErr          error                     // any org OAuth-client method failure
	dsUpsertedOAuthReq           *DSSetOAuthClientRequest  // last request passed to UpsertDSOAuthClient
	dsUpsertedOAuthOrg           string                    // last org id passed to UpsertDSOAuthClient
	dsUpsertedOAuthProv          string                    // last provider passed to UpsertDSOAuthClient
	dsDeletedOAuthOrg            string                    // last org id passed to DeleteDSOAuthClient
	dsDeletedOAuthProv           string                    // last provider passed to DeleteDSOAuthClient
	// Project-scoped OAuth-client config (see datasources_ui.go).
	dsProjectOAuthClients      []DSOAuthClientConfig    // returned by ListProjectDSOAuthClients
	dsProjectOAuthClientErr    error                    // any project OAuth-client method failure
	dsUpsertedProjectOAuthID   string                   // last project id passed to UpsertProjectDSOAuthClient
	dsUpsertedProjectOAuthProv string                   // last provider passed to UpsertProjectDSOAuthClient
	dsUpsertedProjectOAuthReq  *DSSetOAuthClientRequest // last request passed to UpsertProjectDSOAuthClient
	dsDeletedProjectOAuthID    string                   // last project id passed to DeleteProjectDSOAuthClient
	dsDeletedProjectOAuthProv  string                   // last provider passed to DeleteProjectDSOAuthClient
	dsResources                []DSResource             // returned by ListDSResources
	dsResourcesTruncated       bool                     // truncated flag returned by ListDSResources
	dsResourcesErr             error                    // ListDSResources failure
	dsResourcesQuery           string                   // last query passed to ListDSResources
	dsResourcesCallCount       int                      // number of ListDSResources calls
	dsProviderResources        []DSResource             // returned by ListDSProviderResources
	dsProviderResourcesTrunc   bool                     // truncated flag for ListDSProviderResources
	dsProviderResourcesErr     error                    // ListDSProviderResources failure
	dsProviderRefs             []DSResource             // returned by ListDSProviderRefs
	dsProviderRefsTrunc        bool                     // truncated flag for ListDSProviderRefs
	dsProviderRefsErr          error                    // ListDSProviderRefs failure
	dsListedProviderType       string                   // last provider type passed to the listing methods
	dsListedCredentialID       string                   // last credential id passed to the listing methods
	dsListedConfig             map[string]any           // last config passed to the listing methods
	dsSavedResourceID          string                   // last id passed to SaveDSResource
	dsSavedResourceCfg         map[string]any           // last cfg passed to SaveDSResource
	// Saved, project-scoped credentials (see datasources_ui.go).
	dsSavedCredentials       []DSSavedCredential             // returned by ListSavedDSCredentials
	dsSavedCredentialErr     error                           // any saved-credential method failure
	dsCreatedSavedReq        *DSSavedCredentialRequest       // last create request forwarded
	dsUpdatedSavedID         string                          // last id passed to UpdateSavedDSCredential
	dsUpdatedSavedReq        *DSSavedCredentialUpdateRequest // last update request forwarded
	dsDeletedSavedID         string                          // last id passed to DeleteSavedDSCredential
	dsListedSavedProvider    string                          // last providerType passed to ListSavedDSCredentials
	mergeReadiness           *MergeReadiness                 // returned by GetMergeReadiness
	mergeResult              *BranchMergeResult              // returned by MergeBranch
	mergeErr                 error                           // GetMergeReadiness/MergeBranch failure
	lastMergeBranchID        string                          // last sourceBranchID passed to MergeBranch
	lastMergePolicy          string                          // last policy passed to MergeBranch
	previewMergeResult       *BranchMergeResult              // returned by PreviewMergeBranch
	previewMergeErr          error                           // PreviewMergeBranch failure
	lastPreviewMergeBranchID string                          // last branchID passed to PreviewMergeBranch
	lastPreviewMergePolicy   string                          // last policy passed to PreviewMergeBranch
	resolvedResourceType     string                          // last resourceType passed to ResolveNotificationsForResource
	resolvedResourceID       string                          // last resourceID passed to ResolveNotificationsForResource
	resolveResourceErr       error                           // ResolveNotificationsForResource failure
	similar                  []SimilarObject                 // returned by GetSimilarObjects
	similarErr               error                           // GetSimilarObjects failure
	objectHistory            []GraphObject                   // returned by GetObjectHistory
	objectHistoryErr         error                           // GetObjectHistory failure
	updateResult             *GraphObject                    // returned by UpdateObject
	updateObjectErr          error                           // UpdateObject failure
	lastUpdateID             string                          // last id passed to UpdateObject
	lastUpdateReq            *UpdateObjectRequest            // last request passed to UpdateObject
	createObjectResult       *GraphObject                    // returned by CreateObject
	createObjectErr          error                           // CreateObject failure
	lastCreateObjectReq      *CreateObjectRequest            // last request passed to CreateObject
	bulkDeleteErr            error                           // DeleteGraphObject failure
	deletedObjectIDs         []string                        // ids passed to DeleteGraphObject, in order
	deletedObjectBranches    []string                        // branch args passed to DeleteGraphObject, in order
	bulkStatusErr            error                           // BulkUpdateStatus failure
	bulkStatusResult         *BulkUpdateStatusResponse       // returned by BulkUpdateStatus (nil → Success: len(ids))
	bulkStatusIDs            []string                        // ids passed to BulkUpdateStatus, in order
	bulkStatusValue          string                          // status passed to BulkUpdateStatus
	createRelErr             error                           // CreateRelationship failure
	lastCreateRelReq         *CreateRelationshipRequest      // last request passed to CreateRelationship
	ftsResults               []GraphObject                   // returned by SearchObjectsFTS
	ftsErr                   error                           // SearchObjectsFTS failure
	ftsQuery                 string                          // last query passed to SearchObjectsFTS
	ftsTypeFilter            string                          // last type filter passed to SearchObjectsFTS

	// Work items (object-driven work / Kanban board).
	workItems         []WorkItem      // returned by ListWorkItems
	workItemsErr      error           // ListWorkItems failure
	boardRuns         []BoardRun      // returned by ListBoardRuns
	boardRunsErr      error           // ListBoardRuns failure
	workItemDetail    *WorkItemDetail // returned by GetWorkItem
	workItemDetailErr error           // GetWorkItem failure
	workAction        *WorkItemAction // returned by action methods
	workActionErr     error           // action failure
	lastWorkItemID    string          // last canonicalId passed to an action
	lastReassignTo    string          // last assignee passed to ReassignWorkItem
	lastStatusTo      string          // last status passed to SetWorkItemStatus
	lastFeedback      string          // last feedback passed to RequestChangesWorkItem
	lastCommentBody   string          // last body passed to AddWorkComment

	// objects browser: cursor pagination + count + search.
	pageObjects        []GraphObject        // returned by ListGraphObjectsPage
	nextPageCursor     string               // returned by ListGraphObjectsPage
	pageErr            error                // ListGraphObjectsPage failure
	lastPageCursor     string               // last cursor passed to ListGraphObjectsPage
	lastPageBranch     string               // last branchID passed to ListGraphObjectsPage
	lastPageType       string               // last typeFilter passed to ListGraphObjectsPage
	lastPageLimit      int                  // last limit passed to ListGraphObjectsPage
	lastPageActorType  string               // last actorType passed to ListGraphObjectsPage
	lastPageActorID    string               // last actorID passed to ListGraphObjectsPage
	lastPageProvenance string               // last provenance passed to ListGraphObjectsPage
	objectCount        int                  // returned by CountObjects
	objectCountErr     error                // CountObjects failure
	lastCountBranch    string               // last branchID passed to CountObjects
	searchResults      []ObjectSearchResult // returned by SearchObjects
	searchHasMore      bool                 // returned by SearchObjects
	searchObjectsErr   error                // SearchObjects failure
	lastSearchMode     string               // last mode passed to SearchObjects
	lastSearchQuery    string               // last query passed to SearchObjects
	lastSearchTypes    string               // last types passed to SearchObjects
	lastSearchBranch   string               // last branchID passed to SearchObjects
	lastSearchLimit    int                  // last limit passed to SearchObjects
	lastSearchOffset   int                  // last offset passed to SearchObjects
	unifiedResults     []ObjectSearchResult // returned by SearchObjectsUnified
	unifiedErr         error                // SearchObjectsUnified failure
	lastUnifiedQuery   string               // last query passed to SearchObjectsUnified
	lastUnifiedTypes   string               // last types passed to SearchObjectsUnified
	lastUnifiedBrnch   string               // last branchID passed to SearchObjectsUnified
	lastUnifiedLimit   int                  // last limit passed to SearchObjectsUnified
	searchUnifiedResp  *UnifiedSearchResponse
	searchUnifiedErr   error
	lastSearchUnified  UnifiedSearchParams // last params passed to SearchUnified
	knowledgeAnswer    string              // returned by QueryKnowledge
	knowledgeSession   string              // returned by QueryKnowledge
	knowledgeErr       error               // QueryKnowledge failure
	lastKnowledgeQ     string              // last question passed to QueryKnowledge
	lastKnowledgeBr    string              // last branch passed to QueryKnowledge

	embeddingProgress *EmbeddingProgress // returned by GetEmbeddingProgress
	embeddingProgErr  error              // GetEmbeddingProgress failure
	embeddingStatus   *EmbeddingStatus   // returned by GetEmbeddingStatus
	embeddingStatErr  error              // GetEmbeddingStatus failure

	embeddingCoverage    *EmbeddingCoverageResponse // returned by GetEmbeddingCoverage
	embeddingCoverageErr error                      // GetEmbeddingCoverage failure

	effectiveModel      *EffectiveModelConfig // returned by GetEffectiveModelConfig
	effectiveModelErr   error                 // GetEffectiveModelConfig failure
	effectiveModelCalls int                   // number of GetEffectiveModelConfig calls

	compiled     *CompiledSchemaTypes
	blueprintErr error // failure for any blueprint method

	blueprintVersions   []BlueprintRecord       // returned by ListBlueprintVersions
	blueprints          []BlueprintRecord       // returned by ListBlueprints
	blueprint           *BlueprintRecord        // returned by GetBlueprint
	createdBlueprint    *createBlueprintRequest // last CreateBlueprint req
	publishedBlueprint  []string                // ids passed to PublishBlueprint
	appliedBlueprint    []string                // ids passed to ApplyBlueprint
	applyBlueprintRes   *BlueprintApplyResult   // returned by ApplyBlueprint
	applyOverrides      map[string]string       // last overrides passed to ApplyBlueprintWithOverrides
	modelOverrides      map[string]string       // returned by GetModelOverrides, when set
	modelOverridesErr   error                   // GetModelOverrides failure
	modelOverridesName  string                  // last name passed to GetModelOverrides
	appliedBlueprints   []AppliedBlueprint      // returned by ListAppliedBlueprints
	unappliedBlueprint  []string                // ids passed to UnapplyBlueprint
	unapplyBlueprintRes *BlueprintUnapplyResult // returned by UnapplyBlueprint
	checkedUpdates      []string                // names passed to CheckBlueprintUpdates
	appliedUpdates      []string                // names passed to ApplyBlueprintUpdate
	blueprintUpdates    *BlueprintUpdateCheck   // returned by GetBlueprintUpdates, when set
	savedGithubToken    *string                 // last token passed to SetGithubCredential
	deletedBlueprints   []string                // ids passed to DeleteBlueprint
	deleteBlueprintErr  error                   // DeleteBlueprint failure

	importBlueprintReq *importBlueprintRequest // last request passed to ImportBlueprint
	importBlueprintRes *BlueprintRecord        // returned by ImportBlueprint, when set
	importBlueprintErr error                   // ImportBlueprint failure

	previewImportBlueprintRes *BlueprintPreview       // returned by PreviewImportBlueprint, when set
	importAndApplyReq         *importBlueprintRequest // last request passed to ImportAndApplyBlueprint
	importAndApplyCalls       int                     // ImportAndApplyBlueprint call count
	importAndApplyRes         *BlueprintRecord        // returned by ImportAndApplyBlueprint, when set

	schemas []SchemaInfo
	schema  *BlueprintSchema
	// schemasByID, when non-nil, is the schema catalog: a lookup for an id it
	// does not hold fails, which is how the in-memory API answers for a pack the
	// project does not have.
	schemasByID map[string]*BlueprintSchema

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
	providersErr  error               // failure for ListSandboxProviders
	images        []SandboxImage      // returned by ListSandboxImages

	scheduledAgents          []ScheduledAgent               // returned by ListScheduledAgents
	listScheduledAgentsCalls int                            // number of ListScheduledAgents invocations
	scheduledRuns            map[string][]ScheduledAgentRun // runs per agent id, returned by ListScheduledAgentRuns
	schedErr                 error                          // failure for any scheduled-agent method
	createdScheduled         []ScheduledAgent               // agents passed to CreateScheduledAgent
	updatedScheduled         *ScheduledAgent                // last agent passed to UpdateScheduledAgent
	updatedSchedID           string                         // last id passed to UpdateScheduledAgent
	enabledSchedID           string                         // last id passed to EnableScheduledAgent
	setEnabledSchedID        string                         // last id passed to SetScheduledAgentEnabled
	setEnabledSchedVal       *bool                          // last enabled passed to SetScheduledAgentEnabled
	deletedSchedID           string                         // last id passed to DeleteScheduledAgent
	triggeredSchedID         string                         // last id passed to TriggerScheduledAgent
	triggerResult            *TriggerResult                 // returned by TriggerScheduledAgent
	triggerAgentCalls        chan triggerAgentCall          // receives each TriggerAgent call (webhook tests; nil = drop)
	triggerAgentErr          error                          // failure for TriggerAgent
	runFull                  *AgentRunFull                  // returned by GetRunFull
	runFullErr               error                          // failure for GetRunFull
	runQuestions             []AgentQuestionItem            // returned by GetRunQuestions
	runQuestionsErr          error                          // failure for GetRunQuestions
	agentRuns                map[string]*AgentRun           // returned by GetAgentRun per run id
	agentRunErr              error                          // failure for GetAgentRun
	sessionTodos             []SessionTodo                  // returned by ListSessionTodos
	sessionTodosErr          error                          // failure for ListSessionTodos
	lastTodosSessionID       string                         // last session id passed to ListSessionTodos
	cancelAgentID            string                         // last agent id passed to CancelAgentRun
	cancelRunID              string                         // last run id passed to CancelAgentRun
	cancelAgentRunErr        error                          // failure for CancelAgentRun
	// cancelAgentRunCancelled overrides the "row actually cancelled" result of
	// CancelAgentRun (nil → true, the common case).
	cancelAgentRunCancelled *bool

	usageSummary *UsageSummaryResponse    // returned by GetProjectUsageSummary
	usageSeries  *UsageTimeSeriesResponse // returned by GetProjectUsageTimeSeries
	usageErr     error                    // failure for the usage methods

	projectProviders    []ProjectProviderConfig             // returned by ListProjectProviders
	providerDefinitions []ProviderDefinition                // returned by ListProviderDefinitions
	modelsByProvider    map[string][]ProviderSupportedModel // returned by ListProviderModels per provider
	pricing             []ProviderPricing                   // returned by ListPricing
	pricingOverrides    []ProjectCustomPricing              // returned by ListProjectPricingOverrides
	providerErr         error                               // failure for the providers/pricing/overrides reads
	modelErr            error                               // failure for ListProviderModels
	overrideWrites      []ProjectCustomPricing              // every UpsertProjectPricingOverride call, in order
	deletedOverrides    []string                            // "provider/model" keys passed to DeleteProjectPricingOverride

	providerConfigInput    ProviderConfigInput // last UpsertProjectProviderConfig input
	lastProviderConfig     string              // provider name of the last upsert
	deletedProviderConfigs []string            // provider names passed to DeleteProjectProviderConfig
	providerTestResult     *ProviderTestResult // returned by TestProjectProvider
	modelTestResult        *ProviderTestResult // returned by TestProjectModel
	modelTestCalls         []modelTestCall     // every TestProjectModel call, in order
	modelConfig            *ProjectModelConfig // returned by GetProjectModelConfig
	lastModelConfig        *ProjectModelConfig // last UpsertProjectModelConfig

	orgs                 []Org          // returned by ListOrgs
	listOrgsErr          error          // failure for ListOrgs
	createdOrgs          []Org          // every CreateOrg request, in order
	createOrgID          int            // id counter for created orgs
	createOrgErr         error          // failure for CreateOrg
	getOrgErr            error          // failure for GetOrg
	updateOrgErr         error          // failure for UpdateOrg
	renamedOrgID         string         // last org id passed to UpdateOrg
	renamedOrgName       string         // last name passed to UpdateOrg
	deletedOrgID         string         // last org id passed to DeleteOrg
	deleteOrgErr         error          // failure for DeleteOrg
	deletedProjectIDs    []string       // project ids passed to DeleteProject, in order
	deleteProjectErr     error          // failure for DeleteProject
	restoredProjectIDs   []string       // project ids passed to RestoreProject, in order
	restoreProjectErr    error          // failure for RestoreProject
	transferCount        int            // number of TransferProject calls
	transferredID        string         // last project id passed to TransferProject
	transferredToOrg     string         // last destination org id passed to TransferProject
	transferProjectErr   error          // failure for TransferProject
	orgMembers           []OrgMemberDto // returned by ListOrgMembers
	addedOrgMemberEmail  string
	addedOrgMemberRole   string
	addOrgMemberErr      error
	updatedOrgMemberID   string
	updatedOrgMemberRole string
	updateOrgMemberErr   error
	removedOrgMemberID   string
	removeOrgMemberErr   error
	orgInvites           []SentInviteDto // returned by ListOrgInvites
	orgInvitesErr        error
	orgToolSettings      []OrgToolSettingDto       // returned by ListOrgToolSettings
	toolSettingErr       error                     // failure for any org tool-setting method
	toolSettingOrg       string                    // last org id passed to Upsert/DeleteOrgToolSetting
	toolSettingTool      string                    // last tool name passed to Upsert/DeleteOrgToolSetting
	toolSettingIn        UpsertOrgToolSettingInput // last input passed to UpsertOrgToolSetting
	orgsAndProjects      []OrgWithProjectsDto      // returned by GetOrgsAndProjects
	orgsAndProjectsErr   error                     // failure for GetOrgsAndProjects
	orgsAndProjectsCalls int                       // number of GetOrgsAndProjects calls
	projects             []ProjectRef              // returned by ListProjects
	listProjectsErr      error                     // failure for ListProjects
	listProjectsInclErr  error                     // failure for ListProjectsIncludingPending
	createdProjects      []ProjectRef              // every CreateProject request, in order
	createProjectID      int                       // id counter for created projects

	members           []ProjectMemberDto    // returned by ListMembers
	listMembersCalls  int                   // number of ListMembers invocations
	removedMember     string                // last user id passed to RemoveMember
	removeMemberErr   error                 // failure for RemoveMember
	updatedMemberRole string                // last user id passed to UpdateMemberRole
	updatedRoleVal    string                // last role passed to UpdateMemberRole
	updateRoleErr     error                 // failure for UpdateMemberRole
	sentInvites       []SentInviteDto       // returned by ListInvites
	sentInvitesErr    error                 // ListInvites failure
	createdInvites    []CreateInviteDto     // every CreateInvite request, in order
	createInviteID    int                   // id counter for created invites
	createInviteErr   error                 // failure for CreateInvite
	acceptedTokens    []string              // tokens passed to AcceptInvite
	pendingInvites    []PendingInviteDto    // returned by ListPendingInvites
	declinedInvite    string                // last invite id passed to DeclineInvite
	declineInviteErr  error                 // failure for DeclineInvite
	canceledInvite    string                // last invite id passed to CancelInvite
	resentInvite      string                // last invite id passed to ResendInvite
	resendInviteErr   error                 // failure for ResendInvite
	resendOutcome     InviteResendOutcome   // outcome returned by ResendInvite (default "sent")
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
	apiTokens               []APIToken        // project-scoped store
	apiTokenSecrets         map[string]string // plaintext per token id
	apiTokenErr             error             // failure for any project api-token method
	apiTokenScopesErr       error             // failure for UpdateAPITokenScopes only
	apiTokenRevealPlaintext bool              // GetAPIToken returns plaintext when true (encryption configured)
	lastAPITokenID          string            // last project token id passed to any api-token op
	lastAPITokenScopes      []string          // scopes of the last create/update/regenerate
	apiTokenSeq             int               // project token id counter
	// Device credentials (web-device-credential): introspection + mint.
	introspectInfo            *deviceTokenInfo // returned by IntrospectDeviceToken when set
	introspectErr             error            // IntrospectDeviceToken failure
	introspectSeen            []string         // tokens presented to IntrospectDeviceToken
	lastDeviceTokenID         string           // id minted by CreateDeviceToken
	accountAPITokens          []APIToken       // account-scoped store
	accountAPITokenErr        error            // failure for any account api-token method
	accountAPITokenReveal     bool             // GetAccountAPIToken returns plaintext when true
	lastAccountAPITokenID     string           // last account token id passed to any api-token op
	lastAccountAPITokenScopes []string         // scopes of the last account create/update/regenerate
	accountAPITokenSeq        int              // account token id counter

	// Agent-owned MCP endpoint + labeled keys + external sessions
	// (agent-scoped-mcp-endpoint): mcpEndpoint is returned by
	// GetAgentMCPEndpoint (nil = none); the *Err fields drive read/mutation
	// failures; mcpToken / mcpURL are the one-time secret returned by key
	// create/rotate. The last* fields record what the handlers forwarded.
	mcpEndpoint          *AgentMCPEndpoint
	mcpEndpointErr       error
	mcpEndpointCreateErr error
	mcpKeys              []AgentMCPKey
	mcpKeyErr            error
	mcpKeyCreateErr      error
	mcpKeyRotateErr      error
	mcpSessions          []AgentMCPSession
	mcpSessionErr        error
	mcpToken             string
	mcpURL               string
	lastMCPAgentID       string
	lastMCPEndpointID    string
	lastMCPLabel         string
	lastMCPKeyID         string
	lastMCPSessionStatus string

	// Inbox / notifications (see notifications_handlers.go).
	notifications       []Notification
	notificationListErr error
	notificationCounts  *NotificationCounts
	// notificationCountsByScope overrides notificationCounts per "scope|project"
	// key, so a test can give the account and project inboxes distinct counts
	// (the #1342 scope-mismatch regression).
	notificationCountsByScope map[string]*NotificationCounts
	notificationCountErr      error
	notificationPrefs         []NotificationPreference
	notificationPrefsErr      error
	notificationMutations     []string // "action:id[:verb]" in call order
	notificationMutErr        error    // failure for every mutation/preference write
	lastListParams            NotificationListParams
	lastCountsScope           string
	lastCountsProject         string
	lastPrefsProject          string
	eventStreamBody           io.ReadCloser // returned by NotificationEventStream
	eventStreamErr            error
	lastEventStreamProject    string
}

// projectSettingWrite records one SetProjectSetting call on the fake.
type projectSettingWrite struct {
	Category string
	Key      string
	Value    map[string]any
}

func (f *fakeMemory) ListAgentDefinitions(ctx context.Context) ([]AgentDefinitionSummary, error) {
	f.recordCatalog("ListAgentDefinitions")
	return f.agents, nil
}

func (f *fakeMemory) GetAgentDefinition(ctx context.Context, id string) (*AgentDefinition, error) {
	f.defMu.Lock()
	f.defFetches = append(f.defFetches, id)
	f.defMu.Unlock()
	if err, ok := f.defErrs[id]; ok {
		return nil, err
	}
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
	f.listScheduledAgentsCalls++
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
	if f.providersErr != nil {
		return nil, f.providersErr
	}
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
	if f.chatStreamReader != nil {
		return f.chatStreamReader, nil
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
	if f.approvalsErr != nil {
		return nil, f.approvalsErr
	}
	return f.approvals, nil
}

func (f *fakeMemory) CancelAgentRun(ctx context.Context, agentID, runID string) (bool, error) {
	f.cancelAgentID = agentID
	f.cancelRunID = runID
	if f.cancelAgentRunErr != nil {
		return false, f.cancelAgentRunErr
	}
	if f.cancelAgentRunCancelled != nil {
		return *f.cancelAgentRunCancelled, nil
	}
	return true, nil
}

func (f *fakeMemory) ListSessionTodos(ctx context.Context, sessionID string) ([]SessionTodo, error) {
	f.lastTodosSessionID = sessionID
	if f.sessionTodosErr != nil {
		return nil, f.sessionTodosErr
	}
	return f.sessionTodos, nil
}

func (f *fakeMemory) ListAgentQuestions(ctx context.Context) ([]AgentQuestionItem, error) {
	return f.questions, nil
}

func (f *fakeMemory) ListConversations(ctx context.Context, includeArchived bool) (*ConversationList, error) {
	f.convIncludeArchived = includeArchived
	if f.convErr != nil {
		return nil, f.convErr
	}
	// Mirror memory's default-exclude contract so rail render tests can assert
	// archived rows are hidden unless the include-archived option is set.
	out := make([]Conversation, 0, len(f.convs))
	for _, c := range f.convs {
		if c.IsArchived && !includeArchived {
			continue
		}
		out = append(out, c)
	}
	return &ConversationList{Conversations: out, Total: len(out)}, nil
}

func (f *fakeMemory) ListConversationsWithAncestors(ctx context.Context, includeArchived bool) (*ConversationList, error) {
	return f.ListConversations(ctx, includeArchived)
}

func (f *fakeMemory) ArchiveConversation(ctx context.Context, id string) error {
	if f.archiveConvErr != nil {
		return f.archiveConvErr
	}
	f.archivedConvs = append(f.archivedConvs, id)
	return nil
}

func (f *fakeMemory) UnarchiveConversation(ctx context.Context, id string) error {
	if f.unarchiveConvErr != nil {
		return f.unarchiveConvErr
	}
	f.unarchivedConvs = append(f.unarchivedConvs, id)
	return nil
}

func (f *fakeMemory) DeleteConversation(ctx context.Context, id string) error {
	if f.deleteConvErr != nil {
		return f.deleteConvErr
	}
	f.deletedConvs = append(f.deletedConvs, id)
	return nil
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
	if f.detailErr != nil {
		return nil, f.detailErr
	}
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
	f.recordCatalog("ListMCPServers")
	return f.servers, nil
}

func (f *fakeMemory) GetMCPServer(ctx context.Context, id string) (*MCPServer, error) {
	return &MCPServer{ID: id}, nil
}

func (f *fakeMemory) CreateMCPServer(ctx context.Context, in *MCPServer) (*MCPServer, error) {
	if f.mcpServerCreateErr != nil {
		return nil, f.mcpServerCreateErr
	}
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

func (f *fakeMemory) TestMCPServer(ctx context.Context, in *MCPServer, serverID string) (*MCPInspectResult, error) {
	return &MCPInspectResult{ServerID: serverID, Status: "ok"}, nil
}

func (f *fakeMemory) ListMCPServerTools(ctx context.Context, id string) ([]MCPTool, error) {
	return nil, nil
}

func (f *fakeMemory) SetMCPServerToolEnabled(ctx context.Context, id string, toolID string, enabled bool) error {
	return nil
}

func (f *fakeMemory) CallMCPServerTool(ctx context.Context, id, toolName string, args map[string]any) (*MCPToolCallResult, error) {
	return &MCPToolCallResult{}, nil
}

func (f *fakeMemory) ListBuiltinToolGroups(ctx context.Context) ([]BuiltinToolGroup, error) {
	return nil, nil
}

// Registry: the base fake returns zero values so unrelated tests compile;
// registry handler tests use mcpRegistryTestBackend (mcp_registry_handlers_test.go).
func (f *fakeMemory) SearchRegistry(ctx context.Context, query string, limit int, cursor string) (*MCPRegistrySearchResult, error) {
	return &MCPRegistrySearchResult{}, nil
}

func (f *fakeMemory) InstallRegistryServer(ctx context.Context, registryName, version, name string) (*MCPRegistryInstallResult, error) {
	return &MCPRegistryInstallResult{}, nil
}

func (f *fakeMemory) InstallRegistryServerToDevice(ctx context.Context, registryName, version, name, instanceID string) (*MCPRegistryInstallResult, error) {
	return &MCPRegistryInstallResult{}, nil
}

// MCP OAuth connect: base fake returns zero values so unrelated tests compile;
// mcp-server handler tests override these via mcpTestBackend.
func (f *fakeMemory) AuthorizeMCPServer(ctx context.Context, id string) (*MCPOAuthAuthorize, error) {
	return &MCPOAuthAuthorize{AuthorizeURL: "https://www.figma.com/oauth/mcp?state=fake"}, nil
}

func (f *fakeMemory) DisconnectMCPServer(ctx context.Context, id string) error { return nil }

func (f *fakeMemory) SetMCPServerOAuthClient(ctx context.Context, id, clientID, clientSecret string) (*MCPOAuthStatus, error) {
	return &MCPOAuthStatus{ServerID: id, ClientConfigured: true}, nil
}

func (f *fakeMemory) ClearMCPServerOAuthClient(ctx context.Context, id string) error { return nil }

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

// Agent-owned MCP endpoint: stateful fake used by the MCP handler tests
// (agent_mcp_endpoint_handlers_test.go). Get returns the seeded endpoint (or
// nil), create/rotate carry the configured one-time secret, and the list
// methods return the seeded keys/sessions.
func (f *fakeMemory) GetAgentMCPEndpoint(ctx context.Context, agentID string) (*AgentMCPEndpoint, error) {
	f.lastMCPAgentID = agentID
	if f.mcpEndpointErr != nil {
		return nil, f.mcpEndpointErr
	}
	if f.mcpEndpoint == nil {
		return nil, nil
	}
	ep := *f.mcpEndpoint
	return &ep, nil
}

func (f *fakeMemory) CreateAgentMCPEndpoint(ctx context.Context, agentID string) (*AgentMCPEndpoint, error) {
	f.lastMCPAgentID = agentID
	if f.mcpEndpointCreateErr != nil {
		return nil, f.mcpEndpointCreateErr
	}
	ep := AgentMCPEndpoint{ID: "ep_new", AgentID: agentID, Status: "active", CreatedAt: "2026-08-01T10:00:00Z", MCPURL: f.mcpURLValue(agentID)}
	f.mcpEndpoint = &ep
	return &ep, nil
}

func (f *fakeMemory) RevokeAgentMCPEndpoint(ctx context.Context, endpointID string) error {
	f.lastMCPEndpointID = endpointID
	f.mcpEndpoint = nil
	f.mcpKeys = nil
	return nil
}

func (f *fakeMemory) ListAgentMCPKeys(ctx context.Context, endpointID string) ([]AgentMCPKey, error) {
	f.lastMCPEndpointID = endpointID
	if f.mcpKeyErr != nil {
		return nil, f.mcpKeyErr
	}
	return append([]AgentMCPKey(nil), f.mcpKeys...), nil
}

func (f *fakeMemory) CreateAgentMCPKey(ctx context.Context, endpointID, label string) (*AgentMCPKeySecret, error) {
	f.lastMCPEndpointID = endpointID
	f.lastMCPLabel = label
	if f.mcpKeyCreateErr != nil {
		return nil, f.mcpKeyCreateErr
	}
	key := AgentMCPKey{ID: "key_new", EndpointID: endpointID, Label: label, Status: "active", CreatedAt: "2026-08-01T10:00:00Z"}
	f.mcpKeys = append(f.mcpKeys, key)
	return f.mcpSecret(key), nil
}

func (f *fakeMemory) RevokeAgentMCPKey(ctx context.Context, keyID string) error {
	f.lastMCPKeyID = keyID
	for i := range f.mcpKeys {
		if f.mcpKeys[i].ID == keyID {
			f.mcpKeys[i].Status = "revoked"
		}
	}
	return nil
}

func (f *fakeMemory) RotateAgentMCPKey(ctx context.Context, keyID string) (*AgentMCPKeySecret, error) {
	f.lastMCPKeyID = keyID
	if f.mcpKeyRotateErr != nil {
		return nil, f.mcpKeyRotateErr
	}
	key := AgentMCPKey{ID: keyID, Status: "active"}
	for _, k := range f.mcpKeys {
		if k.ID == keyID {
			key = k
		}
	}
	return f.mcpSecret(key), nil
}

func (f *fakeMemory) ListAgentMCPSessions(ctx context.Context, endpointID, status string) ([]AgentMCPSession, error) {
	f.lastMCPEndpointID = endpointID
	f.lastMCPSessionStatus = status
	if f.mcpSessionErr != nil {
		return nil, f.mcpSessionErr
	}
	if status == "" {
		return append([]AgentMCPSession(nil), f.mcpSessions...), nil
	}
	out := make([]AgentMCPSession, 0, len(f.mcpSessions))
	for _, s := range f.mcpSessions {
		if s.Status == status {
			out = append(out, s)
		}
	}
	return out, nil
}

// mcpSecret builds a create/rotate response carrying the configured one-time
// token (or a stable default) and endpoint URL.
func (f *fakeMemory) mcpSecret(key AgentMCPKey) *AgentMCPKeySecret {
	token := f.mcpToken
	if token == "" {
		token = "emt_key"
	}
	return &AgentMCPKeySecret{AgentMCPKey: key, Token: token, MCPURL: f.mcpURLValue(key.AgentID)}
}

// mcpURLValue is the endpoint URL the fake reports, or a stable default derived
// from the agent id.
func (f *fakeMemory) mcpURLValue(agentID string) string {
	if f.mcpURL != "" {
		return f.mcpURL
	}
	return "https://mem.example/api/mcp/agents/" + agentID
}

func (f *fakeMemory) ListRelaySessions(ctx context.Context) ([]RelaySession, error) {
	f.recordCatalog("ListRelaySessions")
	if f.relaySessionsErr != nil {
		return nil, f.relaySessionsErr
	}
	return f.relaySessions, nil
}

func (f *fakeMemory) GetRelaySessionTools(ctx context.Context, instanceID string) ([]RelayTool, error) {
	f.recordCatalog("GetRelaySessionTools")
	if f.relayToolsErr != nil {
		return nil, f.relayToolsErr
	}
	return f.relayTools[instanceID], nil
}

func (f *fakeMemory) ListModels(ctx context.Context) ([]Model, error) {
	f.recordCatalog("ListModels")
	return []Model{{Provider: "deepseek", ModelName: "deepseek-v4-flash", DisplayName: "DeepSeek V4 Flash"}}, nil
}

func (f *fakeMemory) ListDocuments(ctx context.Context, params DocumentListParams) (*DocumentPage, error) {
	if f.docErr != nil {
		return nil, f.docErr
	}
	f.lastDocumentListParams = params
	return &DocumentPage{
		Documents:    f.documents,
		Total:        f.documentsTotal,
		StatusCounts: f.documentStatusCounts,
		NextCursor:   f.documentsNextCursor,
	}, nil
}

func (f *fakeMemory) ListDiscoveryJobs(ctx context.Context) ([]discoveryJob, error) {
	if f.discoveryErr != nil {
		return nil, f.discoveryErr
	}
	return f.discoveryJobs, nil
}

func (f *fakeMemory) StartProjectDiscovery(ctx context.Context) (*DiscoveryStartResult, error) {
	if f.discoveryStartErr != nil {
		return nil, f.discoveryStartErr
	}
	f.discoveryStarted++
	return &DiscoveryStartResult{
		JobID:          "job-project",
		DocumentCount:  len(f.documents),
		TotalDocuments: f.documentsTotal,
	}, nil
}

func (f *fakeMemory) StartDocumentDiscovery(ctx context.Context, documentID string) (*DiscoveryStartResult, error) {
	if f.discoveryStartErr != nil {
		return nil, f.discoveryStartErr
	}
	f.discoveryDocStarts = append(f.discoveryDocStarts, documentID)
	return &DiscoveryStartResult{JobID: "job-document", DocumentCount: 1, TotalDocuments: 1}, nil
}

func (f *fakeMemory) RetryDiscoveryJob(ctx context.Context, jobID string) (*DiscoveryStartResult, error) {
	if f.discoveryStartErr != nil {
		return nil, f.discoveryStartErr
	}
	f.discoveryRetries = append(f.discoveryRetries, jobID)
	return &DiscoveryStartResult{JobID: "job-retry"}, nil
}

func (f *fakeMemory) CancelDiscoveryJob(ctx context.Context, jobID string) error {
	if f.discoveryStartErr != nil {
		return f.discoveryStartErr
	}
	f.discoveryCancels = append(f.discoveryCancels, jobID)
	return nil
}

func (f *fakeMemory) ListDocumentSourceCounts(ctx context.Context) ([]DocumentSourceCount, error) {
	f.recordCatalog("ListDocumentSourceCounts")
	if f.docSourceCountsErr != nil {
		return nil, f.docSourceCountsErr
	}
	return f.docSourceCounts, nil
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

func (f *fakeMemory) EditChunk(ctx context.Context, chunkID, text string) (ChunkEditResult, error) {
	if f.docErr != nil {
		return ChunkEditResult{}, f.docErr
	}
	return ChunkEditResult{ChunkID: chunkID, Revision: 2}, nil
}

func (f *fakeMemory) ListChunkRevisions(ctx context.Context, chunkID string) ([]ChunkRevision, error) {
	if f.docErr != nil {
		return nil, f.docErr
	}
	return nil, nil
}

func (f *fakeMemory) DiffChunkRevisions(ctx context.Context, chunkID string, from, to int) (ChunkDiff, error) {
	if f.docErr != nil {
		return ChunkDiff{}, f.docErr
	}
	return ChunkDiff{From: from, To: to}, nil
}

func (f *fakeMemory) RollbackChunk(ctx context.Context, chunkID string, seq int) (ChunkEditResult, error) {
	if f.docErr != nil {
		return ChunkEditResult{}, f.docErr
	}
	return ChunkEditResult{ChunkID: chunkID, Revision: seq + 1}, nil
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

func (f *fakeMemory) GetDocumentContent(ctx context.Context, documentID string) (string, error) {
	if f.docErr != nil {
		return "", f.docErr
	}
	if f.docContent == nil {
		return "", nil
	}
	return *f.docContent, nil
}

func (f *fakeMemory) ListExtractionJobs(ctx context.Context, documentID string) ([]ExtractionJob, error) {
	if f.docErr != nil {
		return nil, f.docErr
	}
	return f.extractionJobs, nil
}

func (f *fakeMemory) ListProjectExtractionJobs(ctx context.Context, limit int) ([]ExtractionJob, error) {
	if f.extractionJobsErr != nil {
		return nil, f.extractionJobsErr
	}
	return f.extractionJobs, nil
}

func (f *fakeMemory) GetExtractionJob(ctx context.Context, jobID string) (*ExtractionJob, error) {
	if f.docErr != nil {
		return nil, f.docErr
	}
	if f.extractionJob != nil && f.extractionJob.ID == jobID {
		return f.extractionJob, nil
	}
	for i := range f.extractionJobs {
		if f.extractionJobs[i].ID == jobID {
			return &f.extractionJobs[i], nil
		}
	}
	return nil, fmt.Errorf("extraction job %s not found", jobID)
}

func (f *fakeMemory) DownloadDocument(ctx context.Context, id string) (io.ReadCloser, string, error) {
	if f.downloadDocErr != nil {
		return nil, "", f.downloadDocErr
	}
	if len(f.downloadDocBody) == 0 {
		return nil, "", fmt.Errorf("memory 404: document not found")
	}
	contentType := f.downloadDocCT
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	return io.NopCloser(bytes.NewReader(f.downloadDocBody)), contentType, nil
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

func (f *fakeMemory) GetObjectHistory(ctx context.Context, objectID string) ([]GraphObject, error) {
	if f.objectHistoryErr != nil {
		return nil, f.objectHistoryErr
	}
	return f.objectHistory, nil
}

func (f *fakeMemory) LookupGraphObjectByKey(ctx context.Context, typeFilter, key string) (*GraphObject, error) {
	if f.lookupErr != nil {
		return nil, f.lookupErr
	}
	f.lastLookupType = typeFilter
	f.lastLookupKey = key
	if len(f.lookupObjects) == 0 {
		return nil, nil
	}
	return &f.lookupObjects[0], nil
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

// DeleteGraphObject records the deleted id and the branch it was scoped to;
// bulkDeleteErr forces a failure.
func (f *fakeMemory) DeleteGraphObject(ctx context.Context, id, branchID string) error {
	if f.bulkDeleteErr != nil {
		return f.bulkDeleteErr
	}
	f.deletedObjectIDs = append(f.deletedObjectIDs, id)
	f.deletedObjectBranches = append(f.deletedObjectBranches, branchID)
	return nil
}

// BulkUpdateStatus records the last chunk and status; bulkStatusErr forces a
// failure, and bulkStatusResult overrides the success/failed counts.
func (f *fakeMemory) BulkUpdateStatus(ctx context.Context, ids []string, status string) (*BulkUpdateStatusResponse, error) {
	if f.bulkStatusErr != nil {
		return nil, f.bulkStatusErr
	}
	f.bulkStatusIDs = append(f.bulkStatusIDs, ids...)
	f.bulkStatusValue = status
	if f.bulkStatusResult != nil {
		return f.bulkStatusResult, nil
	}
	return &BulkUpdateStatusResponse{Success: len(ids)}, nil
}

func (f *fakeMemory) CreateRelationship(ctx context.Context, req *CreateRelationshipRequest) error {
	f.lastCreateRelReq = req
	return f.createRelErr
}

func (f *fakeMemory) ListWorkItems(ctx context.Context, status, typeName string, limit int) ([]WorkItem, error) {
	if f.workItemsErr != nil {
		return nil, f.workItemsErr
	}
	return f.workItems, nil
}

func (f *fakeMemory) ListBoardRuns(ctx context.Context, limit int) ([]BoardRun, error) {
	if f.boardRunsErr != nil {
		return nil, f.boardRunsErr
	}
	return f.boardRuns, nil
}

func (f *fakeMemory) GetWorkItem(ctx context.Context, canonicalID string) (*WorkItemDetail, error) {
	if f.workItemDetailErr != nil {
		return nil, f.workItemDetailErr
	}
	return f.workItemDetail, nil
}

func (f *fakeMemory) ApproveWorkItem(ctx context.Context, canonicalID string) (*WorkItemAction, error) {
	f.lastWorkItemID = canonicalID
	return f.workAction, f.workActionErr
}

func (f *fakeMemory) RequestChangesWorkItem(ctx context.Context, canonicalID, feedback string) (*WorkItemAction, error) {
	f.lastWorkItemID = canonicalID
	f.lastFeedback = feedback
	return f.workAction, f.workActionErr
}

func (f *fakeMemory) AddWorkComment(ctx context.Context, canonicalID, body string) (*WorkItemAction, error) {
	f.lastWorkItemID = canonicalID
	f.lastCommentBody = body
	return f.workAction, f.workActionErr
}

func (f *fakeMemory) RetryWorkItem(ctx context.Context, canonicalID string) (*WorkItemAction, error) {
	f.lastWorkItemID = canonicalID
	return f.workAction, f.workActionErr
}

func (f *fakeMemory) ReassignWorkItem(ctx context.Context, canonicalID, assignee string) (*WorkItemAction, error) {
	f.lastWorkItemID = canonicalID
	f.lastReassignTo = assignee
	return f.workAction, f.workActionErr
}

func (f *fakeMemory) SetWorkItemStatus(ctx context.Context, canonicalID, status string) (*WorkItemAction, error) {
	f.lastWorkItemID = canonicalID
	f.lastStatusTo = status
	return f.workAction, f.workActionErr
}

func (f *fakeMemory) CancelWorkItem(ctx context.Context, canonicalID string) (*WorkItemAction, error) {
	f.lastWorkItemID = canonicalID
	return f.workAction, f.workActionErr
}

func (f *fakeMemory) SearchObjectsFTS(ctx context.Context, query, typeFilter string) ([]GraphObject, error) {
	if f.ftsErr != nil {
		return nil, f.ftsErr
	}
	f.ftsQuery = query
	f.ftsTypeFilter = typeFilter
	return f.ftsResults, nil
}

func (f *fakeMemory) ListGraphObjectsPage(ctx context.Context, p ObjectListParams) ([]GraphObject, string, error) {
	if f.pageErr != nil {
		return nil, "", f.pageErr
	}
	f.lastPageBranch = p.BranchID
	f.lastPageType = p.TypeFilter
	f.lastPageCursor = p.Cursor
	f.lastPageLimit = p.Limit
	f.lastPageActorType = p.ActorType
	f.lastPageActorID = p.ActorID
	f.lastPageProvenance = p.Provenance
	// Filter pageObjects by branch/type (mirrors ListGraphObjects) so route
	// tests can assert filter behaviour with a plain slice.
	var out []GraphObject
	for _, o := range f.pageObjects {
		if o.BranchID != p.BranchID {
			continue
		}
		if p.TypeFilter != "" && o.Type != p.TypeFilter {
			continue
		}
		out = append(out, o)
	}
	if out == nil {
		out = []GraphObject{}
	}
	return out, f.nextPageCursor, nil
}

func (f *fakeMemory) CountObjects(ctx context.Context, branchID string) (int, error) {
	if f.objectCountErr != nil {
		return 0, f.objectCountErr
	}
	f.lastCountBranch = branchID
	return f.objectCount, nil
}

func (f *fakeMemory) SearchObjects(ctx context.Context, mode, query, types, branchID string, limit, offset int) ([]ObjectSearchResult, bool, error) {
	if f.searchObjectsErr != nil {
		return nil, false, f.searchObjectsErr
	}
	f.lastSearchMode = mode
	f.lastSearchQuery = query
	f.lastSearchTypes = types
	f.lastSearchBranch = branchID
	f.lastSearchLimit = limit
	f.lastSearchOffset = offset
	return f.searchResults, f.searchHasMore, nil
}

func (f *fakeMemory) SearchObjectsUnified(ctx context.Context, query, types, branchID string, limit int) ([]ObjectSearchResult, error) {
	if f.unifiedErr != nil {
		return nil, f.unifiedErr
	}
	f.lastUnifiedQuery = query
	f.lastUnifiedTypes = types
	f.lastUnifiedBrnch = branchID
	f.lastUnifiedLimit = limit
	return f.unifiedResults, nil
}

func (f *fakeMemory) SearchUnified(ctx context.Context, params UnifiedSearchParams) (*UnifiedSearchResponse, error) {
	f.lastSearchUnified = params
	if f.searchUnifiedErr != nil {
		return nil, f.searchUnifiedErr
	}
	if f.searchUnifiedResp != nil {
		return f.searchUnifiedResp, nil
	}
	return &UnifiedSearchResponse{}, nil
}

func (f *fakeMemory) QueryKnowledge(ctx context.Context, question, branch string) (string, string, error) {
	if f.knowledgeErr != nil {
		return "", "", f.knowledgeErr
	}
	f.lastKnowledgeQ = question
	f.lastKnowledgeBr = branch
	return f.knowledgeAnswer, f.knowledgeSession, nil
}

func (f *fakeMemory) GetEmbeddingProgress(ctx context.Context) (*EmbeddingProgress, error) {
	if f.embeddingProgErr != nil {
		return nil, f.embeddingProgErr
	}
	return f.embeddingProgress, nil
}

func (f *fakeMemory) GetEmbeddingStatus(ctx context.Context) (*EmbeddingStatus, error) {
	if f.embeddingStatErr != nil {
		return nil, f.embeddingStatErr
	}
	return f.embeddingStatus, nil
}

func (f *fakeMemory) GetEmbeddingCoverage(ctx context.Context) (*EmbeddingCoverageResponse, error) {
	if f.embeddingCoverageErr != nil {
		return nil, f.embeddingCoverageErr
	}
	return f.embeddingCoverage, nil
}

func (f *fakeMemory) GetEffectiveModelConfig(ctx context.Context) (*EffectiveModelConfig, error) {
	f.effectiveModelCalls++
	if f.effectiveModelErr != nil {
		return nil, f.effectiveModelErr
	}
	return f.effectiveModel, nil
}

func (f *fakeMemory) ListBranches(ctx context.Context) ([]Branch, error) {
	if f.docErr != nil {
		return nil, f.docErr
	}
	return f.branches, nil
}

func (f *fakeMemory) GetMergeReadiness(ctx context.Context, branchID string) (*MergeReadiness, error) {
	if f.mergeErr != nil {
		return nil, f.mergeErr
	}
	return f.mergeReadiness, nil
}

func (f *fakeMemory) MergeBranch(ctx context.Context, sourceBranchID, policy string) (*BranchMergeResult, error) {
	if f.mergeErr != nil {
		return nil, f.mergeErr
	}
	f.lastMergeBranchID = sourceBranchID
	f.lastMergePolicy = policy
	return f.mergeResult, nil
}

func (f *fakeMemory) PreviewMergeBranch(ctx context.Context, branchID, policy string) (*BranchMergeResult, error) {
	if f.previewMergeErr != nil {
		return nil, f.previewMergeErr
	}
	f.lastPreviewMergeBranchID = branchID
	f.lastPreviewMergePolicy = policy
	return f.previewMergeResult, nil
}

func (f *fakeMemory) ResolveNotificationsForResource(ctx context.Context, resourceType, resourceID string) error {
	if f.resolveResourceErr != nil {
		return f.resolveResourceErr
	}
	f.resolvedResourceType = resourceType
	f.resolvedResourceID = resourceID
	return nil
}

func (f *fakeMemory) GetCompiledTypes(ctx context.Context) (*CompiledSchemaTypes, error) {
	f.recordCatalog("GetCompiledTypes")
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

func (f *fakeMemory) ImportBlueprint(ctx context.Context, url, ref, token string) (*BlueprintRecord, error) {
	if f.importBlueprintErr != nil {
		return nil, f.importBlueprintErr
	}
	f.importBlueprintReq = &importBlueprintRequest{URL: url, Ref: ref, Token: token}
	if f.importBlueprintRes != nil {
		return f.importBlueprintRes, nil
	}
	rec := &BlueprintRecord{ID: "bp-imported", Status: "published"}
	f.blueprint = rec
	return rec, nil
}

func (f *fakeMemory) PreviewImportBlueprint(ctx context.Context, url, ref, token string) (*BlueprintPreview, error) {
	if f.importBlueprintErr != nil {
		return nil, f.importBlueprintErr
	}
	f.importBlueprintReq = &importBlueprintRequest{URL: url, Ref: ref, Token: token}
	if f.previewImportBlueprintRes != nil {
		return f.previewImportBlueprintRes, nil
	}
	return &BlueprintPreview{}, nil
}

func (f *fakeMemory) ImportAndApplyBlueprint(ctx context.Context, url, ref, token string, overrides map[string]string) (*BlueprintRecord, error) {
	if f.importBlueprintErr != nil {
		return nil, f.importBlueprintErr
	}
	f.importAndApplyReq = &importBlueprintRequest{URL: url, Ref: ref, Token: token, Apply: true, ModelOverrides: overrides}
	f.importAndApplyCalls++
	if f.importAndApplyRes != nil {
		return f.importAndApplyRes, nil
	}
	rec := &BlueprintRecord{ID: "bp-imported", Status: "published"}
	f.blueprint = rec
	return rec, nil
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

func (f *fakeMemory) ApplyBlueprintWithOverrides(ctx context.Context, id string, overrides map[string]string) (*BlueprintApplyResult, error) {
	if f.blueprintErr != nil {
		return nil, f.blueprintErr
	}
	f.appliedBlueprint = append(f.appliedBlueprint, id)
	f.applyOverrides = overrides
	if f.applyBlueprintRes != nil {
		return f.applyBlueprintRes, nil
	}
	return &BlueprintApplyResult{BlueprintID: id}, nil
}

func (f *fakeMemory) GetModelOverrides(ctx context.Context, name string) (map[string]string, error) {
	if f.modelOverridesErr != nil {
		return nil, f.modelOverridesErr
	}
	f.modelOverridesName = name
	if f.modelOverrides != nil {
		return f.modelOverrides, nil
	}
	return map[string]string{}, nil
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

func (f *fakeMemory) GetBlueprintUpdates(ctx context.Context, name string) (*BlueprintUpdateCheck, error) {
	if f.blueprintErr != nil {
		return nil, f.blueprintErr
	}
	if f.blueprintUpdates != nil {
		return f.blueprintUpdates, nil
	}
	return &BlueprintUpdateCheck{BlueprintName: name}, nil
}

func (f *fakeMemory) CheckBlueprintUpdates(ctx context.Context, name string) (*BlueprintUpdateJob, error) {
	if f.blueprintErr != nil {
		return nil, f.blueprintErr
	}
	f.checkedUpdates = append(f.checkedUpdates, name)
	return &BlueprintUpdateJob{ID: "job-1", Action: "check", Status: "pending"}, nil
}

func (f *fakeMemory) ApplyBlueprintUpdate(ctx context.Context, name string) (*BlueprintUpdateJob, error) {
	if f.blueprintErr != nil {
		return nil, f.blueprintErr
	}
	f.appliedUpdates = append(f.appliedUpdates, name)
	return &BlueprintUpdateJob{ID: "job-2", Action: "update", Status: "pending"}, nil
}

func (f *fakeMemory) SetGithubCredential(ctx context.Context, token string) error {
	if f.blueprintErr != nil {
		return f.blueprintErr
	}
	f.savedGithubToken = &token
	return nil
}

func (f *fakeMemory) DeleteBlueprint(ctx context.Context, id string) error {
	if f.deleteBlueprintErr != nil {
		return f.deleteBlueprintErr
	}
	if f.blueprintErr != nil {
		return f.blueprintErr
	}
	f.deletedBlueprints = append(f.deletedBlueprints, id)
	return nil
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
	if f.schemasByID != nil {
		if s, ok := f.schemasByID[packID]; ok {
			return s, nil
		}
		return nil, fmt.Errorf("schema %s not found", packID)
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
	f.recordCatalog("ListSkills")
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
	f.recordCatalog("ListProjectProviders")
	if f.providerErr != nil {
		return nil, f.providerErr
	}
	return f.projectProviders, nil
}

func (f *fakeMemory) ListProviderDefinitions(ctx context.Context) ([]ProviderDefinition, error) {
	if f.providerDefinitions != nil {
		return f.providerDefinitions, nil
	}
	// Default to the legacy vendor set so tests that predate the registry still
	// exercise the four historical providers without setting the field.
	return []ProviderDefinition{
		{Type: "google", DisplayName: "Google AI", Protocol: "google-genai"},
		{Type: "google-vertex", DisplayName: "Vertex AI", Protocol: "google-genai"},
		{Type: "openai", DisplayName: "OpenAI", Protocol: "openai-chat"},
		{Type: "deepseek", DisplayName: "DeepSeek", Protocol: "openai-chat"},
	}, nil
}

func (f *fakeMemory) ListProviderModels(ctx context.Context, provider string) ([]ProviderSupportedModel, error) {
	f.recordCatalog("ListProviderModels")
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

func (f *fakeMemory) UpsertProjectPricingOverride(ctx context.Context, provider, providerSlug, model string, rates modelPriceRates) (*ProjectCustomPricing, error) {
	if f.providerErr != nil {
		return nil, f.providerErr
	}
	o := &ProjectCustomPricing{ProjectID: "p1", Provider: provider, ProviderSlug: providerSlug, Model: model, modelPriceRates: rates}
	f.overrideWrites = append(f.overrideWrites, *o)
	// Reflect the write so a subsequent ListProjectPricingOverrides sees it.
	replaced := false
	for i := range f.pricingOverrides {
		if cmp.Or(f.pricingOverrides[i].ProviderSlug, f.pricingOverrides[i].Provider) == providerSlug && f.pricingOverrides[i].Model == model {
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
		if cmp.Or(o.ProviderSlug, o.Provider) != provider || o.Model != model {
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
	slug := cmp.Or(in.Slug, provider)
	out := &ProjectProviderConfig{ProjectID: "p1", Provider: provider, Slug: slug}
	out.GenerativeModel = in.GenerativeModel
	out.EmbeddingModel = in.EmbeddingModel
	out.BaseURL = in.BaseURL
	out.GCPProject = in.GCPProject
	out.Location = in.Location
	// Reflect the write so a subsequent ListProjectProviders sees it.
	replaced := false
	for i := range f.projectProviders {
		if cmp.Or(f.projectProviders[i].Slug, f.projectProviders[i].Provider) == slug {
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
		if cmp.Or(p.Slug, p.Provider) != provider {
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

func (f *fakeMemory) TestProjectModel(ctx context.Context, provider, model, modelType string) (*ProviderTestResult, error) {
	f.modelTestCalls = append(f.modelTestCalls, modelTestCall{Provider: provider, Model: model, ModelType: modelType})
	if f.providerErr != nil {
		return nil, f.providerErr
	}
	if f.modelTestResult != nil {
		return f.modelTestResult, nil
	}
	return &ProviderTestResult{Provider: provider, Model: model, Reply: "hello", LatencyMs: 42}, nil
}

func (f *fakeMemory) GetProjectModelConfig(ctx context.Context) (*ProjectModelConfig, error) {
	f.recordCatalog("GetProjectModelConfig")
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
	e.POST("/settings/approvals/:questionId/respond", s.uiApproveReject)
	e.POST("/settings/approvals/:questionId/cancel", s.uiCancelApproval)
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

func TestChatStreamsFlushIncrementally(t *testing.T) {
	pr, pw := io.Pipe()
	release := make(chan struct{})
	go func() {
		_, _ = io.WriteString(pw, ": ping\n\n")
		_, _ = io.WriteString(pw, `data: {"type":"token","token":"x"}`+"\n\n")
		<-release
		_, _ = io.WriteString(pw, `data: {"type":"done"}`+"\n\n")
		_ = pw.Close()
	}()

	f := &fakeMemory{chatStreamReader: pr}
	_, e := newTestServer(f)
	srv := httptest.NewServer(e)
	defer srv.Close()

	// Bounded client timeout: without the per-write flush, the server never
	// sends response headers until the handler returns, so Do blocks and
	// returns "Client.Timeout exceeded while awaiting headers" within 3s — a
	// fast, legible failure instead of a hung test.
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/chat", strings.NewReader(`{"message":"hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		close(release)
		t.Fatalf("POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("content-type = %q", ct)
	}

	// The first frame must arrive *before* release is closed: without the
	// per-write flush, net/http buffers it and the read below blocks until the
	// handler returns (i.e. until release is closed and the pipe closes), so
	// this fails fast on a regression instead of hanging.
	br := bufio.NewReader(resp.Body)
	type lineResult struct {
		line string
		err  error
	}
	got := make(chan lineResult, 1)
	go func() {
		line, rerr := br.ReadString('\n')
		got <- lineResult{line: line, err: rerr}
	}()
	select {
	case res := <-got:
		if res.err != nil {
			close(release)
			t.Fatalf("first line read error: %v", res.err)
		}
		if !strings.HasPrefix(res.line, ": ping") {
			close(release)
			t.Fatalf("first line = %q, want ': ping'", res.line)
		}
	case <-time.After(3 * time.Second):
		close(release)
		t.Fatal("first SSE frame did not flush within 3s — flush-per-write regression")
	}

	close(release)
	rest, err := io.ReadAll(br)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if !strings.Contains(string(rest), `"type":"done"`) {
		t.Fatalf("done event missing from body: %s", rest)
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
	// the raw token delta is forwarded unrendered, and markdown is rendered
	// exactly once as the authoritative snapshot emitted before done.
	if !strings.Contains(body, `"type":"token"`) || !strings.Contains(body, `"token":"**hi**"`) {
		t.Fatalf("raw token delta not forwarded: %s", body)
	}
	if !strings.Contains(body, `"type":"html"`) {
		t.Fatalf("no html event in body: %s", body)
	}
	if !strings.Contains(body, "<strong>hi</strong>") {
		t.Fatalf("markdown not rendered in body: %s", body)
	}
	// order preserved: token < html < done.
	iToken := strings.Index(body, `"type":"token"`)
	iHTML := strings.Index(body, `"type":"html"`)
	iDone := strings.Index(body, `"type":"done"`)
	if iToken < 0 || iHTML < 0 || iDone < 0 || iToken >= iHTML || iHTML >= iDone {
		t.Fatalf("event order not preserved (token=%d html=%d done=%d): %s", iToken, iHTML, iDone, body)
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

// TestApprovalsPageAnswersOpenEndedQuestion is the #1375 regression: a
// conversation-less open-ended ask_user question falls back to
// /settings/approvals (see agents.questionNotificationActionURL), so that page
// MUST render the question with an answer control and accept the answer through
// its respond route. Before the fix the page showed only tool approvals and the
// question was unreachable.
func TestApprovalsPageAnswersOpenEndedQuestion(t *testing.T) {
	const answer = "Call it Memory"
	f := &fakeMemory{questions: []AgentQuestionItem{{
		ID:              "q-open",
		RunID:           "run-1",
		Question:        "What should I name the project?",
		Status:          "pending",
		InteractionType: "text",
		Placeholder:     "Project name",
	}}}
	_, e := newTestServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/approvals", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "What should I name the project?") {
		t.Fatalf("pending open-ended question not rendered: %s", body)
	}
	if !strings.Contains(body, `action="/settings/approvals/q-open/respond"`) {
		t.Fatalf("answer form not rendered: %s", body)
	}
	if !strings.Contains(body, `name="response"`) {
		t.Fatalf("free-text answer input missing: %s", body)
	}

	// The rendered form target actually resolves the question.
	req := httptest.NewRequest(http.MethodPost, "/settings/approvals/q-open/respond",
		strings.NewReader("response=Call+it+Memory"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther && rec.Code != http.StatusOK {
		t.Fatalf("respond status = %d body=%s", rec.Code, rec.Body.String())
	}
	if f.questionID != "q-open" || f.questionResponse != answer {
		t.Fatalf("answer not forwarded: id=%q response=%q", f.questionID, f.questionResponse)
	}
}

// TestApprovalsPageRendersQuestionProposal is the #1425 regression: a pending
// ask_user question that carries a structured proposal must show the proposal
// card on the conversation-less fallback surface, so the user does not approve a
// schema/object change blind. The card is the same sanitized render path the
// inline chat transcript uses.
func TestApprovalsPageRendersQuestionProposal(t *testing.T) {
	f := &fakeMemory{questions: []AgentQuestionItem{{
		ID:       "q-prop",
		Question: "Apply these schema changes?",
		Status:   "pending",
		Proposal: json.RawMessage(proposalBlueprintJSON),
		Options: []AgentQuestionOption{
			{Label: "Approve", Value: "approve"},
			{Label: "Reject", Value: "reject"},
		},
	}}}
	_, e := newTestServer(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/approvals", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`data-testid="pending-question-proposal"`,
		`data-proposal-kind="blueprint"`,
		"Adds 2 object types, 1 relationship type",
		"Person",
		"assigned_to",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("proposal preview missing %q:\n%s", want, body)
		}
	}
	// The answer controls still render, after the proposal preview.
	if !strings.Contains(body, `action="/settings/approvals/q-prop/respond"`) {
		t.Fatalf("answer form missing after proposal: %s", body)
	}
}

// TestApprovalsPageEscapesHostileProposal guards the untrusted-agent-payload
// boundary: a proposal carrying markup must be escaped by the sanitized render
// path, never emitted as live HTML.
func TestApprovalsPageEscapesHostileProposal(t *testing.T) {
	const hostile = `<script>alert(1)</script>`
	raw := `{"kind":"skill","summary":"` + hostile + `","body":{"name":"` + hostile + `"}}`
	f := &fakeMemory{questions: []AgentQuestionItem{{
		ID:       "q-evil",
		Question: "Install this skill?",
		Status:   "pending",
		Proposal: json.RawMessage(raw),
	}}}
	_, e := newTestServer(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/approvals", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, hostile) {
		t.Fatalf("hostile proposal rendered unescaped:\n%s", body)
	}
	if !strings.Contains(body, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Fatalf("escaped proposal text missing:\n%s", body)
	}
}

// TestApprovalsPageChoiceQuestionRendersOptionButtons covers the button-shaped
// ask_user question: each option becomes a submit that posts its value, so a
// conversation-less multiple-choice question is answerable from the same page.
func TestApprovalsPageChoiceQuestionRendersOptionButtons(t *testing.T) {
	f := &fakeMemory{questions: []AgentQuestionItem{{
		ID:       "q-choice",
		Question: "Which environment?",
		Status:   "pending",
		Options: []AgentQuestionOption{
			{Label: "Staging", Value: "staging"},
			{Label: "Production", Value: "production"},
		},
	}}}
	_, e := newTestServer(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/approvals", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `action="/settings/approvals/q-choice/respond"`) {
		t.Fatalf("choice form missing: %s", body)
	}
	for _, want := range []string{`value="staging"`, `value="production"`, "Staging", "Production"} {
		if !strings.Contains(body, want) {
			t.Fatalf("option %q missing: %s", want, body)
		}
	}
}

// TestApprovalsPageMultiSelectQuestionRendersCheckboxesAndSubmitsJSON covers the
// multi_select interaction: the page renders one form with a `multi=1` marker and
// a checkbox per option, and the respond route serializes the checked values to a
// JSON array rather than a scalar.
func TestApprovalsPageMultiSelectQuestionRendersCheckboxesAndSubmitsJSON(t *testing.T) {
	f := &fakeMemory{questions: []AgentQuestionItem{{
		ID:              "q-multi",
		Question:        "Which environments?",
		Status:          "pending",
		InteractionType: "multi_select",
		Options: []AgentQuestionOption{
			{Label: "Staging", Value: "staging"},
			{Label: "Production", Value: "production"},
		},
	}}}
	_, e := newTestServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/approvals", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{`name="multi"`, `value="1"`, `value="staging"`, `value="production"`, `type="checkbox"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected %q in body: %s", want, body)
		}
	}
	// No checkbox may carry `required`: HTML marks the individual checkbox
	// required, not "one of the group", so it would force the user to select
	// the first option — corrupting the answer (#1375 follow-up).
	if strings.Contains(body, `checkbox checkbox-sm" required`) {
		t.Fatalf("multi-select checkbox must not be required: %s", body)
	}

	req := httptest.NewRequest(http.MethodPost, "/settings/approvals/q-multi/respond",
		strings.NewReader(url.Values{"multi": {"1"}, "response": {"staging", "production"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther && rec.Code != http.StatusOK {
		t.Fatalf("respond status = %d body=%s", rec.Code, rec.Body.String())
	}
	if f.questionID != "q-multi" {
		t.Fatalf("question id = %q, want q-multi", f.questionID)
	}
	if f.questionResponse != `["staging","production"]` {
		t.Fatalf("response = %q, want JSON array", f.questionResponse)
	}
}

// TestApprovalsPageMultiSelectEmptySelectionIsRejected guards that a multi-select
// submit with nothing checked does not resume the run with an empty answer.
func TestApprovalsPageMultiSelectEmptySelectionIsRejected(t *testing.T) {
	f := &fakeMemory{questions: []AgentQuestionItem{{
		ID:              "q-multi-empty",
		Question:        "Which environments?",
		Status:          "pending",
		InteractionType: "multi_select",
		Options: []AgentQuestionOption{
			{Label: "Staging", Value: "staging"},
			{Label: "Production", Value: "production"},
		},
	}}}
	_, e := newTestServer(f)

	req := httptest.NewRequest(http.MethodPost, "/settings/approvals/q-multi-empty/respond",
		strings.NewReader(url.Values{"multi": {"1"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("respond status = %d body=%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "err=") {
		t.Fatalf("expected error redirect, got %q", loc)
	}
	if f.questionID != "" {
		t.Fatalf("empty multi-select must not resume: questionID = %q", f.questionID)
	}
	if f.questionResponse != "" {
		t.Fatalf("empty multi-select must not post a response: %q", f.questionResponse)
	}
}

// TestApprovalsPageTextQuestionWithOptionsRendersFreeText guards that a `text`
// interaction renders a free-text input even when options are present (options
// alone no longer force the button branch).
func TestApprovalsPageTextQuestionWithOptionsRendersFreeText(t *testing.T) {
	f := &fakeMemory{questions: []AgentQuestionItem{{
		ID:              "q-text",
		Question:        "Which environment?",
		Status:          "pending",
		InteractionType: "text",
		Options: []AgentQuestionOption{
			{Label: "Staging", Value: "staging"},
		},
	}}}
	_, e := newTestServer(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/approvals", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `type="text"`) || !strings.Contains(body, `name="response"`) {
		t.Fatalf("free-text input missing: %s", body)
	}
	// A text question must not render checkbox inputs for its options. Scope the
	// check to the `response` input so the page shell's own spotlight/command-
	// palette checkbox (which has no `name="response"`) does not false-positive.
	if strings.Contains(body, `type="checkbox" name="response"`) {
		t.Fatalf("text question must not render checkboxes: %s", body)
	}
}

// TestApprovalsPageTextInputHonorsMaxLengthAndAriaLabel covers the free-text
// input's maxlength and aria-label wiring: maxlength renders only for a
// non-zero MaxLength, and the aria-label carries the question text.
func TestApprovalsPageTextInputHonorsMaxLengthAndAriaLabel(t *testing.T) {
	f := &fakeMemory{questions: []AgentQuestionItem{{
		ID:              "q-long",
		Question:        "Describe the change",
		Status:          "pending",
		InteractionType: "text",
		MaxLength:       5000,
	}}}
	_, e := newTestServer(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/approvals", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{`maxlength="5000"`, `aria-label="Describe the change"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected %q in body: %s", want, body)
		}
	}

	// A question with no MaxLength must omit the maxlength attribute.
	f2 := &fakeMemory{questions: []AgentQuestionItem{{
		ID:              "q-no-max",
		Question:        "Describe the change",
		Status:          "pending",
		InteractionType: "text",
	}}}
	_, e2 := newTestServer(f2)
	rec2 := httptest.NewRecorder()
	e2.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/settings/approvals", nil))
	if rec2.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec2.Code, rec2.Body.String())
	}
	if strings.Contains(rec2.Body.String(), "maxlength") {
		t.Fatalf("MaxLength=0 must not render maxlength: %s", rec2.Body.String())
	}
}

// TestApprovalsPageDedupesAndHidesQuestions guards the page's question filter:
// an answered question is not rendered, and a question already represented by a
// pending tool approval is not rendered twice.
func TestApprovalsPageDedupesAndHidesQuestions(t *testing.T) {
	f := &fakeMemory{
		approvals: []ToolApprovalItem{{ID: "a1", QuestionID: "q-tool", ToolName: "delete_all", Decision: "pending"}},
		questions: []AgentQuestionItem{
			{ID: "q-tool", Question: "Approve delete_all?", Status: "pending"},
			{ID: "q-done", Question: "Already answered?", Status: "answered"},
		},
	}
	_, e := newTestServer(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/approvals", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "Already answered?") {
		t.Fatalf("answered question must not render: %s", body)
	}
	if strings.Contains(body, "Approve delete_all?") {
		t.Fatalf("tool-confirmation question must not be duplicated: %s", body)
	}
	// The tool approval itself still renders with its action controls.
	if !strings.Contains(body, "delete_all") || !strings.Contains(body, `action="/settings/approvals/q-tool/respond"`) {
		t.Fatalf("pending approval row missing: %s", body)
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

// TestHistoryHTMLPrefersPersistedReasoning is the gateway regression test for
// issue #1263. A message persisted after the server fix already carries the
// chain-of-thought in content.reasoning and a clean multi-paragraph
// content.text. The renderer must use content.reasoning rather than running the
// legacy first-line heuristic, which would overwrite the real CoT and split the
// reply mid-answer.
func TestHistoryHTMLPrefersPersistedReasoning(t *testing.T) {
	const (
		cot    = "first CoT paragraph\n\nsecond CoT paragraph"
		answer = "## Short answer\n\nYes, the answer is 42."
	)
	item := json.RawMessage(fmt.Sprintf(
		`{"kind":"message","role":"memory","content":{"text":%q,"reasoning":%q}}`, answer, cot))
	f := &fakeMemory{histories: map[string]*ConversationHistory{
		"conv-4": {ConversationID: "conv-4", Items: []json.RawMessage{item}},
	}}
	_, e := newTestServer(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/conversations/conv-4/history", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		Items []struct {
			Content struct {
				HTML      string `json:"html"`
				Text      string `json:"text"`
				Reasoning string `json:"reasoning"`
			} `json:"content"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Items) != 1 {
		t.Fatalf("items = %d, want 1: %s", len(out.Items), rec.Body.String())
	}
	c := out.Items[0].Content
	if c.Reasoning != cot {
		t.Errorf("reasoning = %q, want the persisted CoT %q (legacy heuristic must not overwrite it)", c.Reasoning, cot)
	}
	if c.Text != answer {
		t.Errorf("text = %q, want %q (reply must not be re-split)", c.Text, answer)
	}
	if !strings.Contains(c.HTML, "Short answer") || !strings.Contains(c.HTML, "answer is 42") {
		t.Errorf("reply html must contain the full answer: %s", c.HTML)
	}
	if strings.Contains(c.HTML, "CoT paragraph") {
		t.Errorf("chain-of-thought leaked into rendered reply html: %s", c.HTML)
	}
}

// TestHistoryHTMLRendersReasoningOnlyFinalAsReply is the gateway regression test
// for the reasoning-only final response. Streaming promotes such an event's
// Thought text to a text delta, and the server now persists it the same way:
// content.text carries the answer and content.reasoning is empty. renderHistoryHTML
// must therefore emit reply html for it. Before the server fix the persisted
// message carried only content.reasoning with no text; renderHistoryHTML hit its
// `text == ""` skip and emitted no html, and the client's finishTurn replaced the
// correctly-streamed answer with an empty reply bubble.
func TestHistoryHTMLRendersReasoningOnlyFinalAsReply(t *testing.T) {
	const answer = "the whole answer, arrived as reasoning"
	item := json.RawMessage(fmt.Sprintf(
		`{"kind":"message","role":"memory","content":{"text":%q}}`, answer))
	f := &fakeMemory{histories: map[string]*ConversationHistory{
		"conv-cot": {ConversationID: "conv-cot", Items: []json.RawMessage{item}},
	}}
	_, e := newTestServer(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/conversations/conv-cot/history", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		Items []struct {
			Content struct {
				HTML      string `json:"html"`
				Text      string `json:"text"`
				Reasoning string `json:"reasoning"`
			} `json:"content"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Items) != 1 {
		t.Fatalf("items = %d, want 1: %s", len(out.Items), rec.Body.String())
	}
	c := out.Items[0].Content
	if c.Text != answer {
		t.Errorf("text = %q, want %q", c.Text, answer)
	}
	if c.HTML == "" {
		t.Fatalf("reasoning-only final rendered no html (empty reply bubble)")
	}
	if !strings.Contains(c.HTML, answer) {
		t.Errorf("reply html missing the promoted answer: %s", c.HTML)
	}
	if c.Reasoning != "" {
		t.Errorf("reasoning = %q, want empty (Thought became the reply, matching streaming)", c.Reasoning)
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
	if f.listOrgsErr != nil {
		return nil, f.listOrgsErr
	}
	return f.orgs, nil
}

func (f *fakeMemory) ListProjects(ctx context.Context) ([]ProjectRef, error) {
	if f.listProjectsErr != nil {
		return nil, f.listProjectsErr
	}
	return f.projects, nil
}

func (f *fakeMemory) ListProjectsIncludingPending(ctx context.Context) ([]ProjectRef, error) {
	if f.listProjectsInclErr != nil {
		return nil, f.listProjectsInclErr
	}
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
	f.orgsAndProjectsCalls++
	return f.orgsAndProjects, f.orgsAndProjectsErr
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

func (f *fakeMemory) AddOrgMember(ctx context.Context, orgID, email, role string) (*OrgMemberDto, error) {
	if f.addOrgMemberErr != nil {
		return nil, f.addOrgMemberErr
	}
	f.addedOrgMemberEmail = email
	f.addedOrgMemberRole = role
	return &OrgMemberDto{ID: "u-added", Email: email, Role: role}, nil
}

func (f *fakeMemory) UpdateOrgMemberRole(ctx context.Context, orgID, userID, role string) error {
	if f.updateOrgMemberErr != nil {
		return f.updateOrgMemberErr
	}
	f.updatedOrgMemberID = userID
	f.updatedOrgMemberRole = role
	return nil
}

func (f *fakeMemory) RemoveOrgMember(ctx context.Context, orgID, userID string) error {
	if f.removeOrgMemberErr != nil {
		return f.removeOrgMemberErr
	}
	f.removedOrgMemberID = userID
	return nil
}

func (f *fakeMemory) ListOrgInvites(ctx context.Context, orgID string) ([]SentInviteDto, error) {
	return f.orgInvites, f.orgInvitesErr
}

func (f *fakeMemory) GetOrgPresence(ctx context.Context, orgID string) (*PresenceDto, error) {
	return f.orgPresence, nil
}

func (f *fakeMemory) GetProjectPresence(ctx context.Context) (*PresenceDto, error) {
	return f.projectPresence, nil
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
	f.listMembersCalls++
	return f.members, nil
}

func (f *fakeMemory) RemoveMember(ctx context.Context, userID string) error {
	if f.removeMemberErr != nil {
		return f.removeMemberErr
	}
	f.removedMember = userID
	return nil
}

func (f *fakeMemory) UpdateMemberRole(ctx context.Context, userID, role string) error {
	if f.updateRoleErr != nil {
		return f.updateRoleErr
	}
	f.updatedMemberRole = userID
	f.updatedRoleVal = role
	return nil
}

func (f *fakeMemory) ListInvites(ctx context.Context) ([]SentInviteDto, error) {
	if f.sentInvitesErr != nil {
		return nil, f.sentInvitesErr
	}
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

func (f *fakeMemory) ResendInvite(ctx context.Context, inviteID string) (InviteResendOutcome, error) {
	if f.resendInviteErr != nil {
		return "", f.resendInviteErr
	}
	f.resentInvite = inviteID
	if f.resendOutcome == "" {
		return InviteResendSent, nil
	}
	return f.resendOutcome, nil
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

func (f *fakeMemory) IntrospectDeviceToken(ctx context.Context, token string) (*deviceTokenInfo, error) {
	f.introspectSeen = append(f.introspectSeen, token)
	if f.introspectErr != nil {
		return nil, f.introspectErr
	}
	if f.introspectInfo != nil {
		return f.introspectInfo, nil
	}
	return nil, fmt.Errorf("memory 401 invalid_token: unknown token")
}

func (f *fakeMemory) CreateDeviceToken(ctx context.Context, name string) (*APITokenCreateResponse, error) {
	if f.apiTokenErr != nil {
		return nil, f.apiTokenErr
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
		Scopes:      []string{"device:api", "agents:read", "data:read"},
		CreatedAt:   fakeAPITokenCreatedAt,
	}
	f.apiTokens = append(f.apiTokens, tok)
	f.lastAPITokenID = id
	f.lastDeviceTokenID = id
	out := APITokenCreateResponse{APIToken: tok}
	out.Token = secret
	return &out, nil
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
	if f.apiTokenScopesErr != nil {
		return nil, f.apiTokenScopesErr
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

// --- public agent-share stubs (share handlers are not exercised by these
// tests; the stubs satisfy MemoryBackend) ---

func (f *fakeMemory) SharePublicConfig(ctx context.Context, token string) (*SharePublicConfig, error) {
	return nil, errors.New("share: not implemented")
}

func (f *fakeMemory) ShareListSessions(ctx context.Context, token, endUserRef, filter string) ([]ShareSession, error) {
	f.sharePublicListFilter = filter
	return f.sharePublicSessions, nil
}

func (f *fakeMemory) ShareGetSession(ctx context.Context, token, endUserRef, id string) (*ShareSessionDetail, error) {
	if f.shareDetailErr != nil {
		return nil, f.shareDetailErr
	}
	return f.shareDetail, nil
}

func (f *fakeMemory) ShareCreateSession(ctx context.Context, token, endUserRef string, in ShareCreateSessionInput) (*ShareSession, error) {
	return nil, errors.New("share: not implemented")
}

func (f *fakeMemory) ShareArchiveSession(ctx context.Context, token, endUserRef, id string) error {
	return errors.New("share: not implemented")
}

func (f *fakeMemory) ShareUnarchiveSession(ctx context.Context, token, endUserRef, id string) error {
	f.shareUnarchivedIDs = append(f.shareUnarchivedIDs, id)
	return nil
}

func (f *fakeMemory) ShareDeleteSession(ctx context.Context, token, endUserRef, id string) error {
	f.shareDeletedIDs = append(f.shareDeletedIDs, id)
	return nil
}

func (f *fakeMemory) ShareCancelApproval(ctx context.Context, token, endUserRef, sessionID, questionID string) error {
	f.shareCancelledSess = append(f.shareCancelledSess, sessionID)
	f.shareCancelledIDs = append(f.shareCancelledIDs, questionID)
	return nil
}

func (f *fakeMemory) ShareApprove(ctx context.Context, token, endUserRef, sessionID, questionID string, in ShareApproveInput) error {
	return errors.New("share: not implemented")
}

func (f *fakeMemory) ShareChatStream(ctx context.Context, token, endUserRef string, in ShareStreamInput) (io.ReadCloser, error) {
	return nil, errors.New("share: not implemented")
}

func (f *fakeMemory) ListShareLinks(ctx context.Context, agentDefinitionID string) ([]ShareLink, error) {
	return nil, errors.New("share: not implemented")
}

func (f *fakeMemory) CreateShareLink(ctx context.Context, agentDefinitionID string, in ShareLinkCreateInput) (*ShareLink, error) {
	return nil, errors.New("share: not implemented")
}

func (f *fakeMemory) RevokeShareLink(ctx context.Context, linkID string) error {
	return errors.New("share: not implemented")
}

func (f *fakeMemory) RotateShareLink(ctx context.Context, linkID string) (*ShareLink, error) {
	return nil, errors.New("share: not implemented")
}

func (f *fakeMemory) RevealShareLink(ctx context.Context, linkID string) (string, error) {
	return "", errors.New("share: not implemented")
}

func (f *fakeMemory) ListShareSessionsByProject(ctx context.Context) ([]ShareOwnerSession, error) {
	if f.shareSessionsErr != nil {
		return nil, f.shareSessionsErr
	}
	return f.shareSessions, nil
}

func (f *fakeMemory) GetShareSessionTranscript(ctx context.Context, id string) ([]ShareMessage, error) {
	if f.shareTranscriptErr != nil {
		return nil, f.shareTranscriptErr
	}
	return f.shareMessages, nil
}

// --- document version history ---

func (f *fakeMemory) ListDocumentVersions(ctx context.Context, documentID string) ([]DocumentVersion, error) {
	if f.documentVersionsErr != nil {
		return nil, f.documentVersionsErr
	}
	return f.documentVersions, nil
}

// --- data-source integrations ---

func (f *fakeMemory) ListDSProviders(ctx context.Context) ([]DSProvider, error) {
	if f.dsErr != nil {
		return nil, f.dsErr
	}
	if len(f.dsProviders) == 0 {
		return []DSProvider{{Type: dsProviderTypeGoogleDrive, Name: "Google Drive", SourceType: "drive", Available: true}}, nil
	}
	return f.dsProviders, nil
}

func (f *fakeMemory) ListDSCredentials(ctx context.Context) ([]DSOAuthCredentialDescription, error) {
	if f.dsErr != nil {
		return nil, f.dsErr
	}
	return f.dsCredentials, nil
}

// --- saved, project-scoped credentials ---

func (f *fakeMemory) ListSavedDSCredentials(ctx context.Context, providerType string) ([]DSSavedCredential, error) {
	f.dsListedSavedProvider = providerType
	if f.dsSavedCredentialErr != nil {
		return nil, f.dsSavedCredentialErr
	}
	if providerType == "" {
		return f.dsSavedCredentials, nil
	}
	out := make([]DSSavedCredential, 0, len(f.dsSavedCredentials))
	for _, c := range f.dsSavedCredentials {
		if c.ProviderType == providerType {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeMemory) CreateSavedDSCredential(ctx context.Context, req *DSSavedCredentialRequest) (*DSSavedCredential, error) {
	if f.dsSavedCredentialErr != nil {
		return nil, f.dsSavedCredentialErr
	}
	f.dsCreatedSavedReq = req
	item := DSSavedCredential{
		ID:           "cred-new",
		ProviderType: req.ProviderType,
		Name:         req.Name,
		CreatedAt:    "2026-09-01T00:00:00Z",
		UpdatedAt:    "2026-09-01T00:00:00Z",
	}
	f.dsSavedCredentials = append(f.dsSavedCredentials, item)
	return &item, nil
}

func (f *fakeMemory) UpdateSavedDSCredential(ctx context.Context, id string, req *DSSavedCredentialUpdateRequest) (*DSSavedCredential, error) {
	if f.dsSavedCredentialErr != nil {
		return nil, f.dsSavedCredentialErr
	}
	f.dsUpdatedSavedID = id
	f.dsUpdatedSavedReq = req
	for i := range f.dsSavedCredentials {
		if f.dsSavedCredentials[i].ID != id {
			continue
		}
		f.dsSavedCredentials[i].Name = req.Name
		f.dsSavedCredentials[i].UpdatedAt = "2026-09-02T00:00:00Z"
		return &f.dsSavedCredentials[i], nil
	}
	return nil, &memoryHTTPError{Status: http.StatusNotFound, Message: "credential not found"}
}

func (f *fakeMemory) DeleteSavedDSCredential(ctx context.Context, id string) error {
	if f.dsSavedCredentialErr != nil {
		return f.dsSavedCredentialErr
	}
	f.dsDeletedSavedID = id
	kept := f.dsSavedCredentials[:0]
	for _, c := range f.dsSavedCredentials {
		if c.ID != id {
			kept = append(kept, c)
		}
	}
	f.dsSavedCredentials = kept
	return nil
}

func (f *fakeMemory) GetDSProviderSchema(ctx context.Context, providerType string) (*DSProviderSchema, error) {
	if f.dsErr != nil {
		return nil, f.dsErr
	}
	if f.dsSchema != nil {
		return f.dsSchema, nil
	}
	if providerType != dsProviderTypeGoogleDrive {
		return nil, &memoryHTTPError{Status: http.StatusNotFound, Message: "provider not found"}
	}
	return &DSProviderSchema{Type: providerType, Required: []string{"folder_id"}, Properties: map[string]any{
		"folder_id":       map[string]any{"type": "string"},
		"shared_drive_id": map[string]any{"type": "string"},
		"mime_types":      map[string]any{"type": "array"},
		"recursive":       map[string]any{"type": "boolean"},
	}}, nil
}

func (f *fakeMemory) ListDSSourceTypes(ctx context.Context) ([]DSSourceType, error) {
	if f.dsErr != nil {
		return nil, f.dsErr
	}
	return f.dsSourceTypes, nil
}

func (f *fakeMemory) TestDSConfig(ctx context.Context, req *DSTestConfigRequest) (*DSTestConnectionResponse, error) {
	f.dsLastTestConfig = req
	if f.dsTestConfigErr != nil {
		return nil, f.dsTestConfigErr
	}
	if f.dsTestConfigResp != nil {
		return f.dsTestConfigResp, nil
	}
	return &DSTestConnectionResponse{Success: true, Message: "connection successful"}, nil
}

func (f *fakeMemory) TestDSConnection(ctx context.Context, id string) (*DSTestConnectionResponse, error) {
	f.dsLastTestConnID = id
	if f.dsTestConnErr != nil {
		return nil, f.dsTestConnErr
	}
	if f.dsTestConnResp != nil {
		return f.dsTestConnResp, nil
	}
	return &DSTestConnectionResponse{Success: true, Message: "connection successful"}, nil
}

func (f *fakeMemory) ListDSIntegrations(ctx context.Context) ([]DSIntegration, error) {
	if f.dsErr != nil {
		return nil, f.dsErr
	}
	return f.dsIntegrations, nil
}

func (f *fakeMemory) GetDSIntegration(ctx context.Context, id string) (*DSIntegration, error) {
	if f.dsErr != nil {
		return nil, f.dsErr
	}
	for i := range f.dsIntegrations {
		if f.dsIntegrations[i].ID == id {
			return &f.dsIntegrations[i], nil
		}
	}
	return nil, &memoryHTTPError{Status: http.StatusNotFound, Message: "source not found"}
}

func (f *fakeMemory) GetDSPickerSession(ctx context.Context, id string) (*DSPickerSession, error) {
	if f.dsErr != nil {
		return nil, f.dsErr
	}
	return &DSPickerSession{AccessToken: "picker-token", DeveloperKey: "dev-key", AppID: "123"}, nil
}

func (f *fakeMemory) SetDSSyncTargets(ctx context.Context, id string, folderIDs []string) (*DSIntegration, error) {
	if f.dsErr != nil {
		return nil, f.dsErr
	}
	for i := range f.dsIntegrations {
		if f.dsIntegrations[i].ID != id {
			continue
		}
		if f.dsIntegrations[i].Config == nil {
			f.dsIntegrations[i].Config = map[string]any{}
		}
		folders := make([]any, 0, len(folderIDs))
		for _, folderID := range folderIDs {
			folders = append(folders, map[string]any{"folder_id": folderID, "name": "Folder " + folderID})
		}
		f.dsIntegrations[i].Config["folders"] = folders
		return &f.dsIntegrations[i], nil
	}
	return nil, &memoryHTTPError{Status: http.StatusNotFound, Message: "source not found"}
}

func (f *fakeMemory) CreateDSIntegration(ctx context.Context, req *DSCreateIntegrationRequest) (*DSIntegration, error) {
	if f.dsCreateErr != nil {
		return nil, f.dsCreateErr
	}
	f.dsCreatedReq = req
	item := DSIntegration{
		ID:           "ds-new",
		ProjectID:    "p1",
		Name:         req.Name,
		ProviderType: req.ProviderType,
		SourceType:   req.SourceType,
		SyncMode:     "manual",
		Status:       "active",
		CreatedAt:    "2026-09-01T00:00:00Z",
		UpdatedAt:    "2026-09-01T00:00:00Z",
	}
	if req.SyncMode != nil {
		item.SyncMode = *req.SyncMode
	}
	item.SyncIntervalMinutes = req.SyncIntervalMinutes
	f.dsIntegrations = append(f.dsIntegrations, item)
	return &item, nil
}

func (f *fakeMemory) UpdateDSIntegration(ctx context.Context, id string, req *DSUpdateIntegrationRequest) (*DSIntegration, error) {
	if f.dsUpdateErr != nil {
		return nil, f.dsUpdateErr
	}
	f.dsUpdatedID = id
	f.dsUpdatedReq = req
	for i := range f.dsIntegrations {
		if f.dsIntegrations[i].ID != id {
			continue
		}
		if req.Name != nil {
			f.dsIntegrations[i].Name = *req.Name
		}
		if req.SyncMode != nil {
			f.dsIntegrations[i].SyncMode = *req.SyncMode
		}
		if req.SyncIntervalMinutes != nil {
			f.dsIntegrations[i].SyncIntervalMinutes = req.SyncIntervalMinutes
		}
		return &f.dsIntegrations[i], nil
	}
	return nil, &memoryHTTPError{Status: http.StatusNotFound, Message: "source not found"}
}

func (f *fakeMemory) DeleteDSIntegration(ctx context.Context, id string) error {
	if f.dsDeleteErr != nil {
		return f.dsDeleteErr
	}
	f.dsDeletedID = id
	kept := f.dsIntegrations[:0]
	for _, s := range f.dsIntegrations {
		if s.ID != id {
			kept = append(kept, s)
		}
	}
	f.dsIntegrations = kept
	return nil
}

func (f *fakeMemory) TriggerDSSync(ctx context.Context, id string) (*DSTriggerSyncResponse, error) {
	f.dsLastTriggerID = id
	if f.dsTriggerErr != nil {
		return nil, f.dsTriggerErr
	}
	if f.dsTriggerResp != nil {
		return f.dsTriggerResp, nil
	}
	jobID := "job-1"
	return &DSTriggerSyncResponse{Success: true, Message: "sync started", JobID: &jobID}, nil
}

func (f *fakeMemory) ListDSSyncJobs(ctx context.Context, id, status string, limit, offset int) (*DSListSyncJobsResponse, error) {
	if f.dsSyncJobsErr != nil {
		return nil, f.dsSyncJobsErr
	}
	jobs := f.dsSyncJobs[id]
	if status != "" {
		filtered := make([]DSSyncJob, 0, len(jobs))
		for _, j := range jobs {
			if j.Status == status {
				filtered = append(filtered, j)
			}
		}
		jobs = filtered
	}
	total := len(jobs)
	if offset > 0 {
		if offset >= len(jobs) {
			jobs = nil
		} else {
			jobs = jobs[offset:]
		}
	}
	if limit > 0 && len(jobs) > limit {
		jobs = jobs[:limit]
	}
	return &DSListSyncJobsResponse{Items: jobs, Total: total}, nil
}

func (f *fakeMemory) GetLatestDSSyncJob(ctx context.Context, id string) (*DSSyncJob, error) {
	if f.dsSyncJobsErr != nil {
		return nil, f.dsSyncJobsErr
	}
	jobs := f.dsSyncJobs[id]
	if len(jobs) == 0 {
		return nil, nil
	}
	return &jobs[0], nil
}

func (f *fakeMemory) GetDSSyncJob(ctx context.Context, id, jobID string) (*DSSyncJob, error) {
	if f.dsSyncJobsErr != nil {
		return nil, f.dsSyncJobsErr
	}
	jobs := f.dsSyncJobs[id]
	for i := range jobs {
		if jobs[i].ID == jobID {
			return &jobs[i], nil
		}
	}
	return nil, &memoryHTTPError{Status: http.StatusNotFound, Message: "job not found"}
}

func (f *fakeMemory) CancelDSSyncJob(ctx context.Context, id, jobID string) (*DSCancelSyncJobResponse, error) {
	f.dsLastCancelJobID = jobID
	if f.dsCancelErr != nil {
		return nil, f.dsCancelErr
	}
	if f.dsCancelResp != nil {
		return f.dsCancelResp, nil
	}
	return &DSCancelSyncJobResponse{Success: true, Message: "cancelled"}, nil
}

func (f *fakeMemory) StartDSOAuth(ctx context.Context, providerType string, config map[string]any, scopes []string, reconnectSourceID string) (*DSOAuthAuthorizeResponse, error) {
	f.dsLastOAuthProvider = providerType
	f.dsLastOAuthConfig = config
	f.dsLastOAuthReconnectSourceID = reconnectSourceID
	if f.dsOAuthAuthorizeErr != nil {
		return nil, f.dsOAuthAuthorizeErr
	}
	if f.dsOAuthAuthorizeResp != nil {
		return f.dsOAuthAuthorizeResp, nil
	}
	return &DSOAuthAuthorizeResponse{AuthorizeURL: "https://accounts.example/authorize?state=s1"}, nil
}

func (f *fakeMemory) ListDSOAuthClients(ctx context.Context, orgID string) ([]DSOAuthClientConfig, error) {
	if f.dsOrgOAuthClientErr != nil {
		return nil, f.dsOrgOAuthClientErr
	}
	return f.dsOrgOAuthClients, nil
}

func (f *fakeMemory) GetDSOAuthClient(ctx context.Context, orgID, providerType string) (*DSOAuthClientConfig, error) {
	if f.dsOrgOAuthClientErr != nil {
		return nil, f.dsOrgOAuthClientErr
	}
	for i := range f.dsOrgOAuthClients {
		if f.dsOrgOAuthClients[i].ProviderType == providerType {
			return &f.dsOrgOAuthClients[i], nil
		}
	}
	return &DSOAuthClientConfig{ProviderType: providerType, Configured: false}, nil
}

func (f *fakeMemory) UpsertDSOAuthClient(ctx context.Context, orgID, providerType string, req *DSSetOAuthClientRequest) (*DSOAuthClientConfig, error) {
	f.dsUpsertedOAuthOrg = orgID
	f.dsUpsertedOAuthProv = providerType
	f.dsUpsertedOAuthReq = req
	if f.dsOrgOAuthClientErr != nil {
		return nil, f.dsOrgOAuthClientErr
	}
	return &DSOAuthClientConfig{ProviderType: providerType, ClientID: req.ClientID, Strategy: req.Strategy, Config: req.Config, Configured: true}, nil
}

func (f *fakeMemory) DeleteDSOAuthClient(ctx context.Context, orgID, providerType string) error {
	f.dsDeletedOAuthOrg = orgID
	f.dsDeletedOAuthProv = providerType
	return f.dsOrgOAuthClientErr
}

func (f *fakeMemory) ListProjectDSOAuthClients(ctx context.Context, projectID string) ([]DSOAuthClientConfig, error) {
	if f.dsProjectOAuthClientErr != nil {
		return nil, f.dsProjectOAuthClientErr
	}
	return f.dsProjectOAuthClients, nil
}

func (f *fakeMemory) GetProjectDSOAuthClient(ctx context.Context, projectID, providerType string) (*DSOAuthClientConfig, error) {
	if f.dsProjectOAuthClientErr != nil {
		return nil, f.dsProjectOAuthClientErr
	}
	for i := range f.dsProjectOAuthClients {
		if f.dsProjectOAuthClients[i].ProviderType == providerType {
			return &f.dsProjectOAuthClients[i], nil
		}
	}
	return &DSOAuthClientConfig{ProviderType: providerType, Configured: false}, nil
}

func (f *fakeMemory) UpsertProjectDSOAuthClient(ctx context.Context, projectID, providerType string, req *DSSetOAuthClientRequest) (*DSOAuthClientConfig, error) {
	f.dsUpsertedProjectOAuthID = projectID
	f.dsUpsertedProjectOAuthProv = providerType
	f.dsUpsertedProjectOAuthReq = req
	if f.dsProjectOAuthClientErr != nil {
		return nil, f.dsProjectOAuthClientErr
	}
	return &DSOAuthClientConfig{ProviderType: providerType, ClientID: req.ClientID, Strategy: req.Strategy, Config: req.Config, Configured: true}, nil
}

func (f *fakeMemory) DeleteProjectDSOAuthClient(ctx context.Context, projectID, providerType string) error {
	f.dsDeletedProjectOAuthID = projectID
	f.dsDeletedProjectOAuthProv = providerType
	return f.dsProjectOAuthClientErr
}

func (f *fakeMemory) ListDSResources(ctx context.Context, id, query string) ([]DSResource, bool, error) {
	f.dsResourcesCallCount++
	f.dsResourcesQuery = query
	if f.dsResourcesErr != nil {
		return nil, false, f.dsResourcesErr
	}
	return f.dsResources, f.dsResourcesTruncated, nil
}

func (f *fakeMemory) ListDSProviderResources(ctx context.Context, providerType string, config map[string]any, credentialID string) ([]DSResource, bool, error) {
	f.dsListedProviderType = providerType
	f.dsListedConfig = config
	f.dsListedCredentialID = credentialID
	if f.dsProviderResourcesErr != nil {
		return nil, false, f.dsProviderResourcesErr
	}
	return f.dsProviderResources, f.dsProviderResourcesTrunc, nil
}

func (f *fakeMemory) ListDSProviderRefs(ctx context.Context, providerType string, config map[string]any, credentialID string) ([]DSResource, bool, error) {
	f.dsListedProviderType = providerType
	f.dsListedConfig = config
	f.dsListedCredentialID = credentialID
	if f.dsProviderRefsErr != nil {
		return nil, false, f.dsProviderRefsErr
	}
	return f.dsProviderRefs, f.dsProviderRefsTrunc, nil
}

func (f *fakeMemory) SaveDSResource(ctx context.Context, id string, cfg map[string]any) (*DSIntegration, error) {
	if f.dsUpdateErr != nil {
		return nil, f.dsUpdateErr
	}
	f.dsSavedResourceID = id
	f.dsSavedResourceCfg = cfg
	for i := range f.dsIntegrations {
		if f.dsIntegrations[i].ID == id {
			return &f.dsIntegrations[i], nil
		}
	}
	return nil, &memoryHTTPError{Status: http.StatusNotFound, Message: "source not found"}
}
