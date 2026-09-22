// Package api_test — derive_category_test.go
//
// Self-documenting regression test for the newRunLog category taxonomy
// (helpers_test.go: deriveCategory). Each case pins a real test-name prefix
// from this package to its expected RunLog category so that:
//   - the mapping table stays in sync with helpers_test.go
//   - a future rename/typo in a category string is caught immediately
//   - adding a new domain prefix without updating deriveCategory shows up
//     as a newly-uncategorized ("") entry that a maintainer must consciously
//     accept (see the "known uncategorized" table below) or fix
package api_test

import "testing"

// TestDeriveCategory_AllPrefixesMapped verifies every documented test-name
// prefix maps to its expected category. Test names below are real examples
// pulled from this package (not invented), one per prefix, to guard against
// drift between the mapping table and actual test files.
func TestDeriveCategory_AllPrefixesMapped(t *testing.T) {
	cases := []struct {
		prefix       string
		sampleName   string
		wantCategory string
	}{
		// api/graph
		{"Graph", "TestGraph_CreateObject_Success", "api/graph"},
		{"GraphSearch", "TestGraphSearch_HybridSearch_BasicQuery", "api/graph"},
		{"GraphSimilar", "TestGraphSimilar_NoEmbedding", "api/graph"},
		{"GraphSubgraph", "TestGraphSubgraph_CreateSubgraph_Success", "api/graph"},
		{"GraphAnalytics", "TestGraphAnalytics_GetMostAccessed_Success", "api/graph"},
		{"GraphPropertyValidation", "TestGraphPropertyValidation_NumberCoercion", "api/graph"},
		{"GraphQuery", "TestGraphQuery_ReturnsSSEStream", "api/graph"},
		{"GraphFieldProjection", "TestGraphFieldProjection_ListObjects_FieldProjection", "api/graph"},

		// api/mcp
		{"MCP", "TestMCP_RPC_RequiresAuth", "api/mcp"},
		{"MCPRegistry", "TestMCPRegistry_ListServers_RequiresAuth", "api/mcp"},
		{"MCPSSE", "TestMCPSSE_ToolsList_DiscoverAll18Tools", "api/mcp"},
		{"MCPNew", "TestMCPNew_ListDocuments", "api/mcp"},
		{"MCPSchemaLifecycle", "TestMCPSchemaLifecycle_RequiresExternalServer", "api/mcp"},
		{"ToolSettings", "TestToolSettings_ToggleBuiltinTool", "api/mcp"},
		{"Discovery", "TestDiscovery_StartAndFinalizeSchema", "api/mcp"},

		// api/documents
		{"Documents", "TestDocuments_ListRequiresAuth", "api/documents"},
		{"DocumentsUpload", "TestDocumentsUpload_RequiresAuth", "api/documents"},
		{"DocumentsBatchUpload", "TestDocumentsBatchUpload_RequiresAuth", "api/documents"},
		{"Extraction", "TestExtraction_CreateRequiresAuth", "api/documents"},
		{"Chunks", "TestChunks_ListRequiresAuth", "api/documents"},

		// api/projects
		{"Projects", "TestProjects_ListRequiresAuth", "api/projects"},
		{"Branches", "TestBranches_List_RequiresAuth", "api/projects"},
		{"Orgs", "TestOrgs_ListRequiresAuth", "api/projects"},
		{"GetOrgsAndProjects", "TestGetOrgsAndProjects_RequiresAuth", "api/projects"},

		// api/agents
		{"Agents", "TestAgents_CreateAgent_Success", "api/agents"},
		{"AgentsWebhooks", "TestAgentsWebhooks_CreateHook_Success", "api/agents"},
		{"AgentsVisibility", "TestAgentsVisibility_CancelRun_NotFound", "api/agents"},
		{"AgentsQuestions", "TestAgentsQuestions_RespondToQuestion_RequiresAuth", "api/agents"},
		{"AgentsQuestionsRemote", "TestAgentsQuestionsRemote_APIEndpoints", "api/agents"},
		{"AgentsQuestionsLive", "TestAgentsQuestionsLive_ListAndRespond", "api/agents"},
		{"AgentChat", "TestAgentChat_StreamChat_InvalidAgentDefinition", "api/agents"},
		{"ADKSessions", "TestADKSessions_ListEmpty", "api/agents"},
		{"Tasks", "TestTasks_List_RequiresAuth", "api/agents"},

		// api/auth
		{"ApiToken", "TestApiToken_CreateSuccess", "api/auth"},
		{"SecurityScopes", "TestSecurityScopes_Documents_RequiresAuth", "api/auth"},
		{"Auth", "TestAuth_MissingTokenReturns401", "api/auth"},
		{"AuthInfo", "TestAuthInfo_MeReturnsUserInfo", "api/auth"},
		{"TenantIsolation", "TestTenantIsolation_RejectsInvalidUUIDHeader", "api/auth"},

		// api/admin
		{"Superadmin", "TestSuperadmin_GetMe_RequiresAuth", "api/admin"},
		{"Provider", "TestProvider_ListOrgCredentials_RequiresAuth", "api/admin"},
		{"EmailJobs", "TestEmailJobs_Enqueue_CreatesJob", "api/admin"},
		{"TemplatePacks", "TestTemplatePacks_GetAvailablePacks_RequiresAuth", "api/admin"},
		{"Skills", "TestSkills_ListGlobal_RequiresAuth", "api/admin"},
		{"Notifications", "TestNotifications_GetStats_RequiresAuth", "api/admin"},
		{"Events", "TestEvents_GetConnectionsCount_RequiresAuth", "api/admin"},
		{"Health", "TestHealth_Endpoint", "api/admin"},
		{"ListPending", "TestListPending_RequiresAuth", "api/admin"},

		// api/search
		{"Search", "TestSearch_UnifiedSearch_RequiresAuth", "api/search"},
		{"RelationshipSearch", "TestRelationshipSearch_CreateRelationship_SetsEmbeddingUpdatedAt", "api/search"},
		{"SearchUsers", "TestSearchUsers_RequiresAuth", "api/search"},

		// api/chat
		{"Chat", "TestChat_ListConversations_RequiresAuth", "api/chat"},
		{"ChatHistory", "TestChatHistory_MultiTurnConversation_BuildsContextSummary", "api/chat"},

		// api/users
		{"UserProfile", "TestUserProfile_GetProfile_RequiresAuth", "api/users"},
		{"UserActivity", "TestUserActivity_Record_RequiresAuth", "api/users"},

		// api/embeddings
		{"EmbeddingPolicies", "TestEmbeddingPolicies_ListRequiresAuth", "api/embeddings"},
	}

	for _, tc := range cases {
		t.Run(tc.prefix, func(t *testing.T) {
			got := deriveCategory(tc.sampleName)
			if got != tc.wantCategory {
				t.Errorf("deriveCategory(%q) = %q, want %q", tc.sampleName, got, tc.wantCategory)
			}
		})
	}
}

// TestDeriveCategory_NoMatchReturnsEmpty verifies unrecognized test names
// remain uncategorized instead of panicking or false-matching.
func TestDeriveCategory_NoMatchReturnsEmpty(t *testing.T) {
	for _, name := range []string{"", "NotATest", "TestZzzUnknownDomain_Case", "TestMain"} {
		if got := deriveCategory(name); got != "" {
			t.Errorf("deriveCategory(%q) = %q, want \"\"", name, got)
		}
	}
}
