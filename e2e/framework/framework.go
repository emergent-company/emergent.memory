// Package e2eframework is a thin re-export wrapper around
// github.com/emergent-company/runlog. All types, functions, variables,
// and constants are forwarded so existing import paths continue to work
// without modification.
//
// Phase B will update all consumers to import runlog directly and remove
// this package.
package e2eframework

import (
	"net/http"
	"testing"
	"time"

	runlog "github.com/emergent-company/runlog"
)

// ─────────────────────────────────────────────────────────────────────────────
// Type aliases — these are identical to the runlog types, so methods and
// struct fields are automatically available through the alias.
// ─────────────────────────────────────────────────────────────────────────────

type AgentInfo = runlog.AgentInfo
type AgentQuestion = runlog.AgentQuestion
type AgentRunInterval = runlog.AgentRunInterval
type Analyzer = runlog.Analyzer
type AnalyzerEvent = runlog.AnalyzerEvent
type AnalyzerEventKind = runlog.AnalyzerEventKind
type AnalyzerTraceRow = runlog.AnalyzerTraceRow
type CLIResult = runlog.CLIResult
type ChildEvent = runlog.ChildEvent
type CredentialsInfo = runlog.CredentialsInfo
type EventRow = runlog.EventRow
type ExperimentSummary = runlog.ExperimentSummary
type GanttData = runlog.GanttData
type GanttRow = runlog.GanttRow
type GroupLogger = runlog.GroupLogger
type HTTPResult = runlog.HTTPResult
type LauncherRow = runlog.LauncherRow
type RunDescription = runlog.RunDescription
type RunLog = runlog.RunLog
type RunOutcome = runlog.RunOutcome
type RunRow = runlog.RunRow
type RunTokenSummary = runlog.RunTokenSummary
type Step = runlog.Step
type SuggestionRow = runlog.SuggestionRow
type TestContext = runlog.TestContext
type TestOpts = runlog.TestOpts
type TracePoller = runlog.TracePoller

// ─────────────────────────────────────────────────────────────────────────────
// Constants
// ─────────────────────────────────────────────────────────────────────────────

const CLITimeout = runlog.CLITimeout

// RunOutcome values.
const (
	OutcomeFail = runlog.OutcomeFail
	OutcomePass = runlog.OutcomePass
	OutcomeSkip = runlog.OutcomeSkip
)

// AnalyzerEventKind values.
const (
	AEThought      = runlog.AEThought
	AEText         = runlog.AEText
	AEToolCall     = runlog.AEToolCall
	AEToolResult   = runlog.AEToolResult
	AETokenUsage   = runlog.AETokenUsage
	AEError        = runlog.AEError
	AETurnComplete = runlog.AETurnComplete
	AESystemPrompt = runlog.AESystemPrompt
	AEUserMessage  = runlog.AEUserMessage
)

// ─────────────────────────────────────────────────────────────────────────────
// Variables
// ─────────────────────────────────────────────────────────────────────────────

// ActiveRunLogs points to the canonical sync.Map in the runlog package.
// sync.Map must not be copied, so we export a pointer. Go auto-dereferences
// for method calls, so consumer code like framework.ActiveRunLogs.Load(key)
// continues to work unchanged.
var ActiveRunLogs = &runlog.ActiveRunLogs

// ─────────────────────────────────────────────────────────────────────────────
// Functions — agents.go
// ─────────────────────────────────────────────────────────────────────────────

var BuildAgentRunIntervals = runlog.BuildAgentRunIntervals
var DumpAgentRunDetails = runlog.DumpAgentRunDetails
var FetchRunTokenUsage = runlog.FetchRunTokenUsage
var ListAllQuestions = runlog.ListAllQuestions
var ListPendingQuestions = runlog.ListPendingQuestions
var PollUntilSuccess = runlog.PollUntilSuccess
var PrintGantt = runlog.PrintGantt
var PrintTokenSummary = runlog.PrintTokenSummary

// RespondToQuestion cannot be re-exported via var because its return type
// includes error. We use a thin wrapper.
func RespondToQuestion(t *testing.T, rl *RunLog, home, projectID, questionID, response string) (string, error) {
	return runlog.RespondToQuestion(t, rl, home, projectID, questionID, response)
}

var TriggerAgent = runlog.TriggerAgent

// ─────────────────────────────────────────────────────────────────────────────
// Functions — analyzer.go
// ─────────────────────────────────────────────────────────────────────────────

var NewAnalyzer = runlog.NewAnalyzer

// ─────────────────────────────────────────────────────────────────────────────
// Functions — cli.go
// ─────────────────────────────────────────────────────────────────────────────

var LogSession = runlog.LogSession
var LogStatusPreamble = runlog.LogStatusPreamble
var MustRunBinaryInDirWithHome = runlog.MustRunBinaryInDirWithHome
var MustRunCLI = runlog.MustRunCLI
var MustRunCLIInDir = runlog.MustRunCLIInDir
var MustRunCLIInDirWithHome = runlog.MustRunCLIInDirWithHome

func RunBinaryInDirWithHome(t *testing.T, binary, dir, home string, args ...string) (string, error) {
	return runlog.RunBinaryInDirWithHome(t, binary, dir, home, args...)
}

func RunCLIInDirWithHome(t *testing.T, dir, home string, args ...string) (string, error) {
	return runlog.RunCLIInDirWithHome(t, dir, home, args...)
}

var SetupCLIAuth = runlog.SetupCLIAuth

// ─────────────────────────────────────────────────────────────────────────────
// Functions — client.go
// ─────────────────────────────────────────────────────────────────────────────

var DoJSON = runlog.DoJSON
var DoMCPJSON = runlog.DoMCPJSON
var ReadBody = runlog.ReadBody
var SetAuthHeader = runlog.SetAuthHeader
var Truncate = runlog.Truncate

// ─────────────────────────────────────────────────────────────────────────────
// Functions — credentials.go
// ─────────────────────────────────────────────────────────────────────────────

var VerifyCredentialsWritten = runlog.VerifyCredentialsWritten

var FormatInt = runlog.FormatInt

// ─────────────────────────────────────────────────────────────────────────────
// Functions — env.go
// ─────────────────────────────────────────────────────────────────────────────

var BlueprintEnvVar = runlog.BlueprintEnvVar
var LoadDotEnv = runlog.LoadDotEnv
var LoadDotEnvFrom = runlog.LoadDotEnvFrom
var ParseBlueprintEnvFiles = runlog.ParseBlueprintEnvFiles

// ─────────────────────────────────────────────────────────────────────────────
// Functions — graph.go
// ─────────────────────────────────────────────────────────────────────────────

var ListByLabel = runlog.ListByLabel
var ListByType = runlog.ListByType
var ListRelationships = runlog.ListRelationships
var ListRelationshipsFromCLI = runlog.ListRelationshipsFromCLI
var PropString = runlog.PropString

// ─────────────────────────────────────────────────────────────────────────────
// Functions — parse.go
// ─────────────────────────────────────────────────────────────────────────────

var AllRunsTerminal = runlog.AllRunsTerminal
var CompactRunsOutput = runlog.CompactRunsOutput
var ParseAgentDefsModel = runlog.ParseAgentDefsModel
var ParseAgentID = runlog.ParseAgentID
var ParseFrontmatterFields = runlog.ParseFrontmatterFields
var ParseJSONField = runlog.ParseJSONField
var ParseLineField = runlog.ParseLineField
var ParseProjectID = runlog.ParseProjectID
var PrettyJSONOutput = runlog.PrettyJSONOutput

// ─────────────────────────────────────────────────────────────────────────────
// Functions — precheck.go
// ─────────────────────────────────────────────────────────────────────────────

var RequireServerReady = runlog.RequireServerReady
var SkipIfEndpointMissing = runlog.SkipIfEndpointMissing
var SkipIfServerDown = runlog.SkipIfServerDown

// ─────────────────────────────────────────────────────────────────────────────
// Functions — project.go
// ─────────────────────────────────────────────────────────────────────────────

var ConfigureGoogleProvider = runlog.ConfigureGoogleProvider
var ConfigureProvider = runlog.ConfigureProvider
var ProviderFromEnv = runlog.ProviderFromEnv
var ProviderFromEnvBaseURL = runlog.ProviderFromEnvBaseURL
var CreateProject = runlog.CreateProject
var DeleteProjectOnCleanup = runlog.DeleteProjectOnCleanup
var InstallBlueprint = runlog.InstallBlueprint
var OrgID = runlog.OrgID
var OrgIDArgs = runlog.OrgIDArgs
var ProjectCreateOrgArgs = runlog.ProjectCreateOrgArgs
var RevokeTokenOnCleanup = runlog.RevokeTokenOnCleanup
var UniqueProjectName = runlog.UniqueProjectName

// ─────────────────────────────────────────────────────────────────────────────
// Functions — runlog.go
// ─────────────────────────────────────────────────────────────────────────────

var DoSkipf = runlog.DoSkipf
var NewRunLog = runlog.NewRunLog
var SetupTestProvider = runlog.SetupTestProvider

// ─────────────────────────────────────────────────────────────────────────────
// Functions — server.go
// ─────────────────────────────────────────────────────────────────────────────

var AuthMode = runlog.AuthMode
var E2ETestToken = runlog.E2ETestToken
var DiscoverOrgID = runlog.DiscoverOrgID
var FilteredEnv = runlog.FilteredEnv
var Runner = runlog.Runner
var ServerURL = runlog.ServerURL
var SetToken = runlog.SetToken

// ─────────────────────────────────────────────────────────────────────────────
// Functions — skills.go
// ─────────────────────────────────────────────────────────────────────────────

var VerifySkillInstalled = runlog.VerifySkillInstalled

// ─────────────────────────────────────────────────────────────────────────────
// Functions — step.go, step_result.go, test_context.go, trace_poller.go
// ─────────────────────────────────────────────────────────────────────────────

var NewTest = runlog.NewTest
var NewTracePoller = runlog.NewTracePoller

// ─────────────────────────────────────────────────────────────────────────────
// Types — fixture.go (declarative test entry point)
// ─────────────────────────────────────────────────────────────────────────────

type Fixture = runlog.Fixture
type Option = runlog.Option

// ─────────────────────────────────────────────────────────────────────────────
// Functions — fixture.go
// ─────────────────────────────────────────────────────────────────────────────

var Use = runlog.Use
var WithProject = runlog.WithProject
var WithSchema = runlog.WithSchema
var WithDocument = runlog.WithDocument
var WithBinary = runlog.WithBinary

// ─────────────────────────────────────────────────────────────────────────────
// Ensure imports are used.
// ─────────────────────────────────────────────────────────────────────────────

// These blank-identifier assignments prevent "imported and not used" errors
// for packages that are only referenced in type signatures above.
var _ *http.Response
var _ time.Duration
