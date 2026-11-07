package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"

	"github.com/emergent-company/emergent.memory/pkg/adk"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// ProjectSkillCaptureReader resolves the project-level capture policy. It is
// implemented by domain/projects (which may import skills) and installed via
// SetProjectConfigReader, keeping skills free of a projects import cycle.
type ProjectSkillCaptureReader interface {
	SkillCaptureConfigForProject(ctx context.Context, projectID string) (*ProjectSkillCaptureConfig, error)
	ListSkillCaptureProjects(ctx context.Context) ([]string, error)
}

// BudgetChecker reports whether a project has exhausted its monthly budget.
// *provider.UsageService satisfies it.
type BudgetChecker interface {
	CheckBudgetExceeded(ctx context.Context, projectID string) (bool, error)
}

// TranscriptEntry is a normalised run-log line used to build the review digest.
// It is deliberately decoupled from the agents package so skills does not
// import agents.
type TranscriptEntry struct {
	Kind     string
	Role     string
	ToolName string
	Status   string
	Text     string
	Input    map[string]any
	Output   map[string]any
	Step     int
}

// RunTranscriptReader loads the persisted transcript of an agent run.
// Implemented by an adapter in domain/agents.
type RunTranscriptReader interface {
	FindSkillReviewTranscript(ctx context.Context, runID string) ([]TranscriptEntry, error)
}

// ProposalNotifier is an optional hook for delivering an inbox notification
// when a proposal is staged. Wiring is deliberately optional so the core loop
// works without the notifications domain.
type ProposalNotifier interface {
	NotifySkillProposal(ctx context.Context, projectID, agentName, proposalID, kind, name string)
}

// AfterRunInput carries the run facts the executor already has. The capture
// service resolves the project policy, applies the nudge, and inserts a bounded
// review job; it never calls an LLM on the executor's goroutine.
type AfterRunInput struct {
	ProjectID        string
	OrgID            string
	RunID            string
	AgentID          string
	AgentName        string
	AgentDescription string
	ModelName        string
	TriggerMessage   string
	Steps            int
	Status           string
	Summary          map[string]any
	AutoLoadSkills   bool
	DeclaredSkills   []string
	AgentCapture     *AgentSkillCapture
}

// CaptureService owns the staged skill-capture loop: AfterRun queues a review,
// and Start processes the queue one row at a time.
type CaptureService struct {
	repo         *Repository
	modelFactory *adk.ModelFactory
	budget       BudgetChecker
	log          *slog.Logger

	projectReader ProjectSkillCaptureReader
	transcripts   RunTranscriptReader
	notifier      ProposalNotifier
	proposals     *ProposalService

	startOnce sync.Once
	stopOnce  sync.Once
	stopCh    chan struct{}
	doneCh    chan struct{}
}

// NewCaptureService creates the capture service.
func NewCaptureService(repo *Repository, modelFactory *adk.ModelFactory, log *slog.Logger) *CaptureService {
	return &CaptureService{
		repo:         repo,
		modelFactory: modelFactory,
		log:          log.With(logger.Scope("skills.capture")),
		stopCh:       make(chan struct{}),
		doneCh:       make(chan struct{}),
	}
}

// SetBudgetChecker installs the monthly-budget gate used before auto-apply.
// *provider.UsageService satisfies BudgetChecker; the adapter is wired by the
// agents module to keep skills out of the provider/scheduler import graph.
func (s *CaptureService) SetBudgetChecker(b BudgetChecker) { s.budget = b }

// SetProjectConfigReader installs the project policy reader.
func (s *CaptureService) SetProjectConfigReader(r ProjectSkillCaptureReader) { s.projectReader = r }

// SetTranscriptReader installs the run transcript reader.
func (s *CaptureService) SetTranscriptReader(r RunTranscriptReader) { s.transcripts = r }

// SetNotifier installs the optional proposal notifier.
func (s *CaptureService) SetNotifier(n ProposalNotifier) { s.notifier = n }

// SetProposalService installs the applier used by auto-apply.
func (s *CaptureService) SetProposalService(p *ProposalService) { s.proposals = p }

// AfterRun is called once per completed successful run. It is cheap and never
// blocks on a model call: it only performs the nudge check and inserts a review
// row.
func (s *CaptureService) AfterRun(ctx context.Context, in AfterRunInput) {
	if s == nil || s.repo == nil || SkillCaptureDisabled() {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			s.log.Error("skill capture: AfterRun panicked", slog.Any("panic", r))
		}
	}()

	projectID, err := uuid.Parse(in.ProjectID)
	if err != nil {
		return
	}
	agentID, err := uuid.Parse(in.AgentID)
	if err != nil {
		return // capture is per agent definition; a runtime agent without an id is skipped
	}
	runID, err := uuid.Parse(in.RunID)
	if err != nil {
		return
	}

	cfg := s.projectConfig(ctx, in.ProjectID)
	if cfg == nil {
		return
	}
	mode := ResolveCaptureMode(cfg, in.AgentCapture)
	if mode == SkillCaptureOff {
		return
	}
	if !agentLoadsPrefixSkills(in.AutoLoadSkills, in.DeclaredSkills) {
		s.log.Debug("skill capture: skipping agent that does not auto-load prefix skills",
			slog.String("agent_id", in.AgentID))
		return
	}

	// Nudge: at most one review per N completed runs, and never while a review
	// for this agent is still queued or running.
	active, err := s.repo.HasActiveReview(ctx, agentID)
	if err != nil || active {
		return
	}
	last, err := s.repo.LastReviewCreatedAt(ctx, agentID)
	if err != nil {
		return
	}
	if !last.IsZero() && last.Year() > 1 {
		since, err := s.repo.CountCompletedRunsSince(ctx, agentID, last)
		if err != nil || since < cfg.Normalized().NudgeInterval {
			return
		}
	}

	review := &SkillReview{
		ProjectID:  projectID,
		AgentID:    &agentID,
		AgentRunID: &runID,
		AgentName:  in.AgentName,
		Status:     ReviewStatusPending,
		Input: map[string]any{
			"trigger_message":   in.TriggerMessage,
			"summary":           in.Summary,
			"steps":             in.Steps,
			"status":            in.Status,
			"declared_skills":   in.DeclaredSkills,
			"auto_load_skills":  in.AutoLoadSkills,
			"agent_name":        in.AgentName,
			"agent_description": in.AgentDescription,
			"run_model":         in.ModelName,
			"capture_mode":      mode,
			"agent_capture":     in.AgentCapture,
		},
	}
	if in.OrgID != "" {
		if orgID, err := uuid.Parse(in.OrgID); err == nil {
			review.OrgID = &orgID
		}
	}
	if err := s.repo.CreateReview(ctx, review); err != nil {
		s.log.Warn("skill capture: failed to queue review", logger.Error(err))
	}
}

// agentLoadsPrefixSkills mirrors the executor's AutoLoadSkills gate: learned
// skills are only useful if the next run will load them.
func agentLoadsPrefixSkills(autoLoad bool, declared []string) bool {
	if autoLoad {
		return true
	}
	for _, n := range declared {
		if n == "*" {
			return true
		}
	}
	return false
}

func (s *CaptureService) projectConfig(ctx context.Context, projectID string) *ProjectSkillCaptureConfig {
	if s.projectReader == nil {
		return nil
	}
	cfg, err := s.projectReader.SkillCaptureConfigForProject(ctx, projectID)
	if err != nil {
		s.log.Warn("skill capture: failed to read project capture config",
			slog.String("project_id", projectID), logger.Error(err))
		return nil
	}
	return cfg
}

// Start begins the single background poller.
func (s *CaptureService) Start(ctx context.Context) {
	s.startOnce.Do(func() {
		go s.loop(ctx)
	})
}

// Stop terminates the poller and waits for the in-flight review to finish.
func (s *CaptureService) Stop() {
	s.stopOnce.Do(func() {
		close(s.stopCh)
		<-s.doneCh
	})
}

func (s *CaptureService) loop(ctx context.Context) {
	defer close(s.doneCh)
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.drain(ctx)
		}
	}
}

func (s *CaptureService) drain(ctx context.Context) {
	// The kill switch stops queued work too, not only queueing: during an
	// incident an operator expects the loop to be inert, including auto-apply.
	if SkillCaptureDisabled() {
		return
	}
	// Do not consume queued rows until the app has installed the project reader
	// and the transcript adapter; claiming without them would mark reviews
	// skipped for a wiring bug.
	if s.projectReader == nil || s.transcripts == nil {
		return
	}
	for i := 0; i < 4; i++ { // bounded batch; the next tick picks up the rest
		review, err := s.repo.ClaimPendingReview(ctx)
		if err != nil {
			s.log.Warn("skill capture: claim failed", logger.Error(err))
			return
		}
		if review == nil {
			return
		}
		// Bound one review: a hung provider must not wedge the poller forever.
		reviewCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		s.processReviewSafely(reviewCtx, review)
		cancel()
	}
}

// processReviewSafely keeps a panic in one review from taking down the process.
// The row stays `running` and is reclaimable after 30 minutes.
func (s *CaptureService) processReviewSafely(ctx context.Context, review *SkillReview) {
	defer func() {
		if r := recover(); r != nil {
			s.log.Error("skill capture: review panicked",
				slog.Any("panic", r), slog.String("review_id", review.ID.String()))
		}
	}()
	s.processReview(ctx, review)
}

// --- review prompt ---

const reviewSystemPrompt = `You curate the skill library of an AI agent named %q.

Read the run digest and decide whether this run taught a durable, reusable lesson worth writing into the agent's skills. Most runs teach nothing: returning {"candidates":[]} is the normal, expected answer.

Rules:
- Lessons, not logs. Never include dates, PR/issue/ticket numbers, quoted chat, user names, or one-off facts.
- A skill is a short Markdown procedure: a title, when to use it, and concrete steps. Write it for a future agent with no memory of this run.
- Prefer patching an existing skill over creating a new one when the lesson belongs there. Use action "patch" and provide patch_old (an exact, unique substring of the current content) and patch_new.
- Use action "create" only for a genuinely new, generalisable procedure. The system prefixes new names with %q.
- Use action "delete" only when an existing learned skill is actively harmful. It is always human-reviewed.
- Confidence is 0..1. Use >= 0.8 only when the lesson is clear, general, and supported by the evidence.
- Return at most %d candidates.
- Output JSON only, no prose and no code fences:
{"candidates":[{"action":"create|patch|delete","name":"topic-slug","description":"...","content":"...","target_skill_name":"...","patch_old":"...","patch_new":"...","evidence":"...","confidence":0.0}]}

Existing project skills (name: description):
%s`

type reviewCandidate struct {
	Action          string  `json:"action"`
	Name            string  `json:"name"`
	Description     string  `json:"description"`
	Content         string  `json:"content"`
	TargetSkillID   string  `json:"target_skill_id"`
	TargetSkillName string  `json:"target_skill_name"`
	PatchOld        string  `json:"patch_old"`
	PatchNew        string  `json:"patch_new"`
	Evidence        string  `json:"evidence"`
	Confidence      float64 `json:"confidence"`
}

type reviewOutput struct {
	Candidates []reviewCandidate `json:"candidates"`
}

func (s *CaptureService) processReview(ctx context.Context, review *SkillReview) {
	start := time.Now()
	cfg := s.projectConfig(ctx, review.ProjectID.String())
	if cfg == nil {
		s.finish(review, ReviewStatusSkipped, "", 0, 0, 0, "capture disabled")
		return
	}
	norm := cfg.Normalized()
	if s.transcripts == nil || s.modelFactory == nil {
		s.finish(review, ReviewStatusSkipped, "", 0, 0, 0, "capture not wired")
		return
	}

	runCtx := captureContext(ctx, review)
	entries, err := s.transcripts.FindSkillReviewTranscript(runCtx, uuidOrEmpty(review.AgentRunID))
	if err != nil {
		s.finish(review, ReviewStatusFailed, "", 0, 0, 0, "transcript: "+err.Error())
		return
	}
	if len(entries) == 0 {
		s.finish(review, ReviewStatusSkipped, "", 0, 0, 0, "empty transcript")
		return
	}

	orgID := uuidOrEmpty(review.OrgID)
	var orgPtr *string
	if orgID != "" {
		orgPtr = &orgID
	}
	existing, err := s.repo.FindAll(runCtx, strPtrString(review.ProjectID.String()), orgPtr)
	if err != nil {
		s.finish(review, ReviewStatusFailed, "", 0, 0, 0, "skills: "+err.Error())
		return
	}
	existingIndex := make(map[string]*Skill, len(existing))
	for _, sk := range existing {
		existingIndex[sk.Name] = sk
	}

	agentName, _ := review.Input["agent_name"].(string)
	if agentName == "" {
		agentName = review.AgentName
	}
	prefix := AgentSkillPrefix(agentName)
	digest := buildReviewDigest(review, entries, norm.MaxInputTokens)
	prompt := fmt.Sprintf(reviewSystemPrompt, agentName, prefix, norm.MaxCandidates, renderExistingSkills(existing))
	prompt += "\n\nRun digest:\n" + digest

	runModel, _ := review.Input["run_model"].(string)
	llm, modelName, err := s.reviewModel(runCtx, norm, runModel)
	if err != nil {
		s.finish(review, ReviewStatusFailed, modelName, 0, 0, 0, "model: "+err.Error())
		return
	}

	req := &model.LLMRequest{
		Contents: []*genai.Content{genai.NewContentFromText(prompt, genai.RoleUser)},
		Config: &genai.GenerateContentConfig{
			MaxOutputTokens: 4096,
			Temperature:     genai.Ptr[float32](0.2),
		},
	}
	var text strings.Builder
	var inTokens, outTokens int32
	for resp, respErr := range llm.GenerateContent(runCtx, req, false) {
		if respErr != nil {
			s.finish(review, ReviewStatusFailed, modelName, int(inTokens), int(outTokens), 0, "llm: "+respErr.Error())
			return
		}
		if resp == nil {
			continue
		}
		if resp.UsageMetadata != nil {
			inTokens = resp.UsageMetadata.PromptTokenCount
			outTokens += resp.UsageMetadata.CandidatesTokenCount
		}
		if resp.Content != nil {
			for _, part := range resp.Content.Parts {
				if part != nil && part.Text != "" {
					text.WriteString(part.Text)
				}
			}
		}
	}

	out, err := parseReviewOutput(text.String())
	if err != nil {
		s.finish(review, ReviewStatusFailed, modelName, int(inTokens), int(outTokens), 0, "parse: "+err.Error())
		return
	}

	agentID := review.AgentID
	staged := 0
	for _, c := range out.Candidates {
		if staged >= norm.MaxCandidates {
			break
		}
		if s.stageCandidate(runCtx, review, cfg, c, existingIndex, agentName) {
			staged++
		}
	}

	s.log.Info("skill review complete",
		slog.String("review_id", review.ID.String()),
		slog.String("agent", agentName),
		slog.Int("candidates", staged),
		slog.Int("input_tokens", int(inTokens)),
		slog.Int("output_tokens", int(outTokens)),
		slog.Duration("duration", time.Since(start)),
		slog.String("agent_id", uuidOrEmpty(agentID)),
	)
	s.finish(review, ReviewStatusDone, modelName, int(inTokens), int(outTokens), staged, "")
}

func (s *CaptureService) reviewModel(ctx context.Context, cfg ProjectSkillCaptureConfig, runModel string) (model.LLM, string, error) {
	name := cfg.ReviewModel
	if name == "" {
		name = runModel
	}
	if name != "" {
		llm, err := s.modelFactory.CreateModelWithName(ctx, name)
		if err != nil {
			return nil, name, err
		}
		return llm, name, nil
	}
	llm, err := s.modelFactory.CreateModel(ctx)
	if err != nil {
		return nil, "", err
	}
	return llm, "", nil
}

func (s *CaptureService) finish(review *SkillReview, status, modelName string, inTok, outTok, candidates int, errMsg string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.repo.FinishReview(ctx, review.ID, status, modelName, inTok, outTok, candidates, errMsg); err != nil {
		s.log.Warn("skill capture: failed to finish review", logger.Error(err))
	}
}

// stageCandidate validates one model candidate and either stages a proposal or
// auto-applies it. Returns true when a proposal row was created.
func (s *CaptureService) stageCandidate(ctx context.Context, review *SkillReview, cfg *ProjectSkillCaptureConfig,
	c reviewCandidate, existing map[string]*Skill, agentName string) bool {
	norm := cfg.Normalized()
	kind := strings.TrimSpace(strings.ToLower(c.Action))
	if kind == "" {
		kind = ProposalKindCreate
	}
	if kind != ProposalKindCreate && kind != ProposalKindPatch && kind != ProposalKindDelete {
		return false
	}
	if c.Confidence < 0 || c.Confidence > 1 {
		c.Confidence = 0
	}

	// Resolve the target for patch/delete.
	var target *Skill
	if kind == ProposalKindPatch || kind == ProposalKindDelete {
		if id, err := uuid.Parse(c.TargetSkillID); err == nil {
			if sk, err := s.repo.FindByID(ctx, id); err == nil && sk.ProjectID != nil && *sk.ProjectID == review.ProjectID.String() {
				target = sk
			}
		}
		if target == nil && c.TargetSkillName != "" {
			target = existing[c.TargetSkillName]
		}
		if target == nil {
			return false
		}
		if target.IsLocked() || target.IsPinned() {
			return false
		}
		if kind == ProposalKindDelete {
			if !target.IsAgentCreated() {
				return false
			}
		}
	}

	// Normalize the candidate name.
	c.Name = strings.TrimSpace(strings.ToLower(c.Name))
	if kind == ProposalKindCreate {
		if !strings.HasPrefix(c.Name, AgentSkillPrefix(agentName)) {
			c.Name = LearnedSkillName(agentName, c.Name)
		}
		if _, exists := existing[c.Name]; exists {
			return false
		}
	} else if target != nil {
		c.Name = target.Name
	}

	// Build the content that the linter/scan evaluate.
	// Compute the bytes that will actually be written. A patch candidate may
	// carry both `content` and `patch_old`/`patch_new`; the patch fields win
	// because they are what applyPatch writes, so scanning the model's
	// `content` would judge different bytes than land in the library.
	content := c.Content
	if kind == ProposalKindPatch {
		if target == nil {
			return false
		}
		if c.PatchOld != "" || c.PatchNew != "" {
			merged, err := applyPatchContent(target.Content, c.PatchOld, c.PatchNew)
			if err != nil {
				s.log.Debug("skill capture: bad patch candidate",
					logger.Error(err), slog.String("name", c.Name))
				return false
			}
			content = merged
		}
	}
	if kind == ProposalKindDelete {
		content = ""
	}

	// Security scan is an unconditional hard gate: a leaking or injectable
	// candidate is never staged, even for human review.
	if kind != ProposalKindDelete {
		scan := ScanSkill(c.Name, content)
		if !scan.Passed() {
			s.log.Warn("skill capture: candidate rejected by security scan",
				slog.String("agent", agentName), slog.String("name", c.Name))
			return false
		}
	}
	var findings []LintFinding
	if norm.LinterEnabled() && kind != ProposalKindDelete {
		desc := c.Description
		if desc == "" && target != nil {
			desc = target.Description
		}
		lint := LintSkill(c.Name, desc, content, norm.MaxSkillBytes)
		findings = lint.Findings
		if lint.Errors > 0 {
			return false
		}
	}

	// Dedup against the visible library. A failed check drops the candidate
	// rather than letting an unverified duplicate through.
	if kind == ProposalKindCreate && content != "" {
		id, sim, err := s.repo.NearestSkillSimilarity(ctx, review.ProjectID.String(), uuidOrEmpty(review.OrgID), c.Name+" "+c.Description)
		if err != nil {
			s.log.Warn("skill capture: dedup check failed; dropping candidate", logger.Error(err))
			return false
		}
		if id != uuid.Nil && sim >= norm.DedupThreshold {
			s.log.Debug("skill capture: candidate is a near-duplicate",
				slog.String("skill_id", id.String()), slog.Float64("similarity", sim))
			return false
		}
	}

	p := &SkillProposal{
		ProjectID:   review.ProjectID,
		AgentID:     review.AgentID,
		AgentRunID:  review.AgentRunID,
		AgentName:   agentName,
		ReviewID:    &review.ID,
		Kind:        kind,
		Name:        c.Name,
		Description: c.Description,
		Content:     content,
		PatchOld:    c.PatchOld,
		PatchNew:    c.PatchNew,
		Evidence:    c.Evidence,
		Confidence:  c.Confidence,
		Source:      ProposalSourceBackground,
		Status:      ProposalStatusPending,
		Findings:    findings,
	}
	if target != nil {
		p.BaseSkillID = &target.ID
		p.BaseContentHash = target.ContentHashOf()
		if p.Description == "" {
			p.Description = target.Description
		}
	}

	// Insert first: auto-apply is recorded on the same audit row, and a crash
	// between insert and apply leaves a visible pending proposal, never a lost
	// lesson.
	if err := s.repo.CreateProposal(ctx, p); err != nil {
		s.log.Warn("skill capture: failed to stage proposal", logger.Error(err))
		return false
	}

	if s.proposals != nil && s.autoApplyAllowed(ctx, review, *cfg, p, target, norm) {
		if _, err := s.proposals.Apply(ctx, p, nil, true); err != nil {
			s.log.Warn("skill capture: auto-apply failed, leaving pending",
				slog.String("name", p.Name), logger.Error(err))
		}
		return true
	}

	if s.notifier != nil {
		s.notifier.NotifySkillProposal(ctx, p.ProjectID.String(), agentName, p.ID.String(), p.Kind, p.Name)
	}
	return true
}

func (s *CaptureService) autoApplyAllowed(ctx context.Context, review *SkillReview, cfg ProjectSkillCaptureConfig,
	p *SkillProposal, target *Skill, norm ProjectSkillCaptureConfig) bool {
	if p.Kind == ProposalKindDelete {
		return false
	}
	var agentCapture *AgentSkillCapture
	if v, ok := review.Input["agent_capture"].(map[string]any); ok && len(v) > 0 {
		// The typed override round-tripped through JSONB; re-read it.
		var mode *string
		if m, ok := v["mode"].(string); ok && m != "" {
			mode = &m
		}
		var autoApply *bool
		if b, ok := v["autoApply"].(bool); ok {
			autoApply = &b
		}
		agentCapture = &AgentSkillCapture{Mode: mode, AutoApply: autoApply}
	}
	if !ResolveAutoApply(&cfg, agentCapture) {
		return false
	}
	if p.Confidence < norm.AutoApplyConfidence {
		return false
	}
	if target != nil {
		if !target.IsAgentCreated() || target.IsLocked() || target.IsPinned() {
			return false
		}
		if target.LifecycleStateOrDefault() == SkillLifecycleArchived {
			return false
		}
	}
	if review.AgentID == nil {
		return false
	}

	// Earned trust: at least one human-approved proposal before autonomy.
	human, err := s.repo.CountAppliedProposals(ctx, *review.AgentID, "", nil, true)
	if err != nil || human < norm.MinApprovedBeforeAuto {
		return false
	}

	// Rate caps apply per candidate kind: a day of creates must not block a
	// legitimate patch, and vice versa.
	dayStart := time.Now().UTC().Truncate(24 * time.Hour)
	var dailyCap int
	switch p.Kind {
	case ProposalKindCreate:
		dailyCap = norm.AutoCreatesPerDay
	case ProposalKindPatch:
		dailyCap = norm.AutoPatchesPerDay
	default:
		return false
	}
	appliedToday, err := s.repo.CountAppliedProposals(ctx, *review.AgentID, p.Kind, &dayStart, false)
	if err != nil || appliedToday >= dailyCap {
		return false
	}
	if p.Kind == ProposalKindCreate {
		total, err := s.repo.CountAppliedProposals(ctx, *review.AgentID, ProposalKindCreate, nil, false)
		if err != nil || total >= norm.MaxAgentSkills {
			return false
		}
	}

	// Budget: never spend on self-improvement when the project is over budget.
	// A check that cannot complete fails closed, like every other guardrail.
	if s.budget != nil {
		exceeded, err := s.budget.CheckBudgetExceeded(ctx, review.ProjectID.String())
		if err != nil {
			s.log.Warn("skill capture: budget check failed; refusing auto-apply", logger.Error(err))
			return false
		}
		if exceeded {
			return false
		}
	}

	// Patch base must still match.
	if target != nil && p.Kind == ProposalKindPatch {
		if p.BaseContentHash == "" || p.BaseContentHash != target.ContentHashOf() {
			return false
		}
	}
	return true
}

// --- digest & parsing ---

func buildReviewDigest(review *SkillReview, entries []TranscriptEntry, maxTokens int) string {
	if maxTokens <= 0 {
		maxTokens = defaultMaxInputTokens
	}
	budget := maxTokens * 4 // rough chars-per-token
	var b strings.Builder
	if msg, _ := review.Input["trigger_message"].(string); msg != "" {
		fmt.Fprintf(&b, "[trigger] %s\n\n", truncate(msg, 2000))
	}
	if summary, ok := review.Input["summary"]; ok {
		if raw, err := json.Marshal(summary); err == nil && string(raw) != "null" && string(raw) != "{}" {
			fmt.Fprintf(&b, "[run summary] %s\n\n", truncate(string(raw), 2000))
		}
	}
	for _, e := range entries {
		switch e.Kind {
		case "message", "assistant", "user":
			role := e.Role
			if role == "" {
				role = e.Kind
			}
			fmt.Fprintf(&b, "[step %d][%s] %s\n", e.Step, role, truncate(e.Text, 3000))
		case "tool_call":
			fmt.Fprintf(&b, "[step %d][tool %s %s] %s -> %s\n", e.Step, e.ToolName, e.Status,
				truncate(marshalMap(e.Input), 1200), truncate(marshalMap(e.Output), 1200))
		}
	}
	out := b.String()
	if len(out) <= budget {
		return out
	}
	head := out[:budget/5]
	tail := out[len(out)-budget*4/5:]
	return head + "\n...[digest truncated]...\n" + tail
}

func renderExistingSkills(sks []*Skill) string {
	if len(sks) == 0 {
		return "(none)"
	}
	var b strings.Builder
	limit := 60
	for i, sk := range sks {
		if i >= limit {
			fmt.Fprintf(&b, "- … %d more\n", len(sks)-limit)
			break
		}
		fmt.Fprintf(&b, "- %s: %s\n", sk.Name, truncate(sk.Description, 200))
	}
	return b.String()
}

func parseReviewOutput(text string) (*reviewOutput, error) {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("no JSON object in model output")
	}
	var out reviewOutput
	if err := json.Unmarshal([]byte(text[start:end+1]), &out); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	return &out, nil
}

// ApplyPatch applies an exact, unique old->new replacement. An empty old means
// the caller supplied a full replacement (new is the whole content). It is the
// single patch primitive shared by the MCP skill-patch tool, the proposal
// applier, and the review worker.
func ApplyPatch(content, old, new string) (string, error) {
	return applyPatchContent(content, old, new)
}

// applyPatchContent applies an exact, unique old->new replacement. An empty old
// means the candidate supplied a full replacement (new is the whole content).
func applyPatchContent(content, old, new string) (string, error) {
	if old == "" {
		if strings.TrimSpace(new) == "" {
			return "", fmt.Errorf("patch has no content")
		}
		return new, nil
	}
	if strings.TrimSpace(new) == "" {
		return "", fmt.Errorf("patch replacement is empty")
	}
	n := strings.Count(content, old)
	if n == 0 {
		return "", fmt.Errorf("patch base text not found")
	}
	if n > 1 {
		return "", fmt.Errorf("patch base text is not unique (%d matches)", n)
	}
	return strings.Replace(content, old, new, 1), nil
}

func captureContext(ctx context.Context, review *SkillReview) context.Context {
	p := review.ProjectID.String()
	ctx = auth.ContextWithProjectID(ctx, p)
	ctx = adk.WithProjectID(ctx, p)
	if review.OrgID != nil {
		ctx = auth.ContextWithOrgID(ctx, review.OrgID.String())
	}
	if review.AgentRunID != nil {
		ctx = auth.ContextWithRunIDs(ctx, review.AgentRunID, nil)
	}
	return ctx
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func marshalMap(m map[string]any) string {
	if len(m) == 0 {
		return ""
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return string(raw)
}

func uuidOrEmpty(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

func strPtrString(s string) *string { return &s }

// ErrSkillCaptureNotConfigured is returned by the proposal service when a
// project has not opted in. It exists so the API can answer 409 instead of 500.
var ErrSkillCaptureNotConfigured = apperror.ErrConflict.WithMessage("skill capture is not enabled for this project")

// SkillCaptureEnabled is the nil-safe helper the API and the executor use.
func SkillCaptureEnabled(cfg *ProjectSkillCaptureConfig) bool {
	return cfg != nil && cfg.Enabled()
}
