package blueprints

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/emergent-company/emergent.memory/domain/scheduler"
	"github.com/emergent-company/emergent.memory/domain/schemas"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

const (
	// updateCheckBatchSize is how many pending jobs one job-runner tick claims.
	updateCheckBatchSize = 20

	// updateJobMaxAttempts caps retries before a job is terminally failed.
	updateJobMaxAttempts = 5
)

// ──────────────────────────────────────────────
// Version + content comparison
// ──────────────────────────────────────────────

// parseSemver parses a strict semver "major.minor.patch" core, tolerating a
// leading "v" and any "-prerelease" / "+metadata" suffix. ok is false when the
// string is not a semver core.
func parseSemver(v string) (major, minor, patch int, ok bool) {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return 0, 0, 0, false
	}
	var nums [3]int
	for i, p := range parts {
		if p == "" {
			return 0, 0, 0, false
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return 0, 0, 0, false
		}
		nums[i] = n
	}
	return nums[0], nums[1], nums[2], true
}

// isNewerVersion reports whether latest is strictly newer than current. When
// both versions parse as semver, the comparison is numeric (with prerelease <
// release); otherwise any difference is treated as newer (a non-semver version
// change).
func isNewerVersion(current, latest string) bool {
	if current == latest {
		return false
	}
	cmaj, cmin, cpat, cok := parseSemver(current)
	lmaj, lmin, lpat, lok := parseSemver(latest)
	if cok && lok {
		if lmaj != cmaj {
			return lmaj > cmaj
		}
		if lmin != cmin {
			return lmin > cmin
		}
		if lpat != cpat {
			return lpat > cpat
		}
		// Same numeric core: a release is newer than any prerelease; two
		// prereleases compare lexicographically; build metadata breaks ties.
		cPre := prereleaseOf(current)
		lPre := prereleaseOf(latest)
		switch {
		case cPre == lPre:
			return latest > current
		case lPre == "":
			return true // latest is a release, current is a prerelease
		case cPre == "":
			return false // current is a release, latest is a prerelease
		default:
			return lPre > cPre
		}
	}
	return true
}

// prereleaseOf returns the semver prerelease segment ("1.2.3-beta.1" → "beta.1"),
// or "" for a release with no prerelease.
func prereleaseOf(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	if i := strings.IndexByte(v, '-'); i >= 0 {
		return v[i+1:]
	}
	return ""
}

// canonicalManifestHash returns a stable content hash of a blueprint manifest,
// normalized to sorted-key compact JSON so it compares equal across jsonb
// round-trips and Go struct marshaling. Falls back to a raw sha256 when the
// bytes are not valid JSON.
func canonicalManifestHash(manifest json.RawMessage) string {
	if len(manifest) == 0 {
		manifest = json.RawMessage("{}")
	}
	var v any
	if err := json.Unmarshal(manifest, &v); err != nil {
		return rawHash(manifest)
	}
	normalized, err := json.Marshal(v)
	if err != nil {
		return rawHash(manifest)
	}
	return rawHash(normalized)
}

func rawHash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// updateAvailable decides whether the applied blueprint should surface an
// update: the upstream version is strictly newer, or (same version) the upstream
// manifest content differs from the applied one.
func updateAvailable(appliedVersion, latestVersion, appliedHash, latestHash string) bool {
	if appliedVersion != latestVersion {
		return isNewerVersion(appliedVersion, latestVersion)
	}
	return appliedHash != latestHash
}

// isRepoNotFound reports whether err is the codeload "repository or ref not
// found" failure (used to hint that a private repo needs a credential).
func isRepoNotFound(err error) bool {
	var apErr *apperror.Error
	if errors.As(err, &apErr) {
		return strings.Contains(apErr.Message, "not found")
	}
	return false
}

// isConflict reports whether err is an apperror with the conflict code (the
// "already exists" class). Used by the update path to treat a duplicate create
// as already-created and continue.
func isConflict(err error) bool {
	var apErr *apperror.Error
	return errors.As(err, &apErr) && apErr.Code == apperror.ErrConflict.Code
}

// ──────────────────────────────────────────────
// Update-check service (scheduler-registered)
// ──────────────────────────────────────────────

// NewUpdateCheckScheduler registers the always-on blueprint update-check tasks
// on the shared scheduler. It is wired as an fx.Invoke so it runs once after the
// scheduler is constructed (before the scheduler lifecycle starts it).
func NewUpdateCheckScheduler(svc *Service, sched *scheduler.Scheduler, cfg *config.Config, log *slog.Logger) error {
	schedule := cfg.BlueprintUpdateCheckSchedule
	if schedule == "" {
		schedule = "0 0 2 * * *"
	}
	if err := sched.AddCronTask("blueprint_update_check", schedule, func(ctx context.Context) error {
		return svc.EnqueueUpdateCheckSweep(ctx)
	}); err != nil {
		log.Warn("failed to register blueprint update check sweep", logger.Error(err))
	}

	interval := cfg.BlueprintUpdateJobInterval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if err := sched.AddIntervalTask("blueprint_update_jobs", interval, func(ctx context.Context) error {
		return svc.RunUpdateJobs(ctx)
	}); err != nil {
		log.Warn("failed to register blueprint update job runner", logger.Error(err))
	}
	return nil
}

// EnqueueUpdateCheckSweep enqueues one `check` job per distinct applied,
// GitHub-imported blueprint. Called by the always-on cron. Individual enqueue
// failures are logged and skipped — a single failure never fails the sweep.
func (s *Service) EnqueueUpdateCheckSweep(ctx context.Context) error {
	refs, err := s.repo.ListAppliedGithubBlueprintNames(ctx)
	if err != nil {
		return err
	}
	for _, ref := range refs {
		if _, err := s.EnqueueUpdateCheck(ctx, ref.ProjectID, ref.BlueprintName); err != nil {
			s.log.Warn("failed to enqueue blueprint update check",
				logger.Error(err),
				slog.String("projectID", ref.ProjectID),
				slog.String("blueprintName", ref.BlueprintName),
			)
		}
	}
	return nil
}

// EnqueueUpdateCheck enqueues a `check` job for a project's applied blueprint
// (idempotent: returns the existing pending/running job when present).
func (s *Service) EnqueueUpdateCheck(ctx context.Context, projectID, blueprintName string) (*BlueprintUpdateJob, error) {
	return s.repo.EnqueueUpdateJob(ctx, projectID, blueprintName, UpdateActionCheck)
}

// GetUpdateCheck returns the latest check result for a project's blueprint.
func (s *Service) GetUpdateCheck(ctx context.Context, projectID, blueprintName string) (*BlueprintUpdateCheck, error) {
	return s.repo.GetUpdateCheck(ctx, projectID, blueprintName)
}

// EnqueueBlueprintUpdate enqueues an `update` (apply) job for a project's
// applied blueprint. Refused with a conflict when an update job is already
// pending/running for the same (project, name), so the caller cannot stack
// duplicate applies.
func (s *Service) EnqueueBlueprintUpdate(ctx context.Context, projectID, blueprintName string) (*BlueprintUpdateJob, error) {
	return s.EnqueueBlueprintUpdateWithOptions(ctx, projectID, blueprintName, UpdateJobOptions{})
}

// EnqueueBlueprintUpdateWithOptions is EnqueueBlueprintUpdate with explicit job
// policy (e.g. merge confirmation for object-type collisions).
func (s *Service) EnqueueBlueprintUpdateWithOptions(ctx context.Context, projectID, blueprintName string, opts UpdateJobOptions) (*BlueprintUpdateJob, error) {
	existing, err := s.repo.findPendingUpdateJob(ctx, projectID, blueprintName, UpdateActionUpdate)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, apperror.NewConflict("an update for this blueprint is already pending or running")
	}
	return s.repo.EnqueueUpdateJobWithOptions(ctx, projectID, blueprintName, UpdateActionUpdate, opts)
}

// GetUpdateStatus returns the latest check result plus the latest update job
// (check or update) for a project's blueprint. ErrNotFound when neither a check
// nor a job has been recorded yet.
func (s *Service) GetUpdateStatus(ctx context.Context, projectID, blueprintName string) (*BlueprintUpdateStatus, error) {
	var check *BlueprintUpdateCheck
	c, err := s.repo.GetUpdateCheck(ctx, projectID, blueprintName)
	switch {
	case err == nil:
		check = c
	case isNotFound(err):
		// no check yet — still report the latest job below
	default:
		return nil, err
	}

	var job *BlueprintUpdateJob
	j, err := s.repo.GetLatestUpdateJob(ctx, projectID, blueprintName)
	switch {
	case err == nil:
		job = j
	case isNotFound(err):
		// no job yet
	default:
		return nil, err
	}

	if check == nil && job == nil {
		return nil, apperror.NewNotFound("update status", blueprintName)
	}
	return &BlueprintUpdateStatus{BlueprintUpdateCheck: check, LatestJob: job}, nil
}

// SaveGithubCredential stores (or replaces) a project's encrypted GitHub PAT.
// Refused when no encryption key is configured (never store unencrypted).
func (s *Service) SaveGithubCredential(ctx context.Context, projectID, token string) error {
	if s.encryptor == nil {
		return apperror.NewConflict("GitHub token encryption key is not configured")
	}
	if strings.TrimSpace(token) == "" {
		return apperror.NewBadRequest("token is required")
	}
	ciphertext, nonce, err := s.encryptor.Encrypt([]byte(token))
	if err != nil {
		return apperror.NewInternal("failed to encrypt GitHub token", err)
	}
	now := time.Now()
	return s.repo.UpsertGithubCredential(ctx, &GithubCredential{
		ProjectID:      projectID,
		EncryptedToken: ciphertext,
		Nonce:          nonce,
		CreatedAt:      now,
		UpdatedAt:      now,
	})
}

// decryptGithubToken returns the project's decrypted GitHub PAT, or "" when no
// credential is stored or encryption is not configured.
func (s *Service) decryptGithubToken(ctx context.Context, projectID string) (string, error) {
	if s.encryptor == nil {
		return "", nil
	}
	cred, err := s.repo.GetGithubCredential(ctx, projectID)
	if err != nil {
		if isNotFound(err) {
			return "", nil
		}
		return "", err
	}
	plaintext, err := s.encryptor.Decrypt(cred.EncryptedToken, cred.Nonce)
	if err != nil {
		return "", fmt.Errorf("decrypt github credential: %w", err)
	}
	return string(plaintext), nil
}

// RunUpdateJobs claims and executes a batch of pending update jobs. Called by
// the interval task. A single job failure never fails the whole tick.
func (s *Service) RunUpdateJobs(ctx context.Context) error {
	jobs, err := s.repo.ClaimUpdateJobs(ctx, updateCheckBatchSize)
	if err != nil {
		return err
	}
	for i := range jobs {
		s.executeJob(ctx, &jobs[i])
	}
	return nil
}

// executeJob dispatches one claimed job to its action handler and records its
// terminal state. Both `check` and `update` share the single claim loop.
func (s *Service) executeJob(ctx context.Context, job *BlueprintUpdateJob) {
	switch job.Action {
	case UpdateActionCheck:
		s.executeCheckJob(ctx, job)
	case UpdateActionUpdate:
		s.executeUpdateJob(ctx, job)
	default:
		_ = s.repo.FailUpdateJob(ctx, job.ID, fmt.Sprintf("unsupported update action %q", job.Action))
	}
}

// executeCheckJob runs one claimed `check` job and records its terminal state.
func (s *Service) executeCheckJob(ctx context.Context, job *BlueprintUpdateJob) {
	check, err := s.checkBlueprint(ctx, job.ProjectID, job.BlueprintName)
	if err != nil {
		if isNotFound(err) {
			// No longer applied / not a GitHub blueprint: nothing to check.
			_ = s.repo.CompleteUpdateJob(ctx, job.ID, json.RawMessage(`{"skipped":"no applied github blueprint"}`))
			return
		}
		if job.Attempts >= updateJobMaxAttempts {
			_ = s.repo.FailUpdateJob(ctx, job.ID, err.Error())
		} else {
			_ = s.repo.RequeueUpdateJob(ctx, job.ID, err.Error())
		}
		return
	}

	result, _ := json.Marshal(map[string]any{
		"updateAvailable": check.UpdateAvailable,
		"latestVersion":   check.LatestVersion,
	})
	_ = s.repo.CompleteUpdateJob(ctx, job.ID, result)
}

// executeUpdateJob runs one claimed `update` (apply) job and records its
// terminal state. A failure marks the job failed or requeues it with the same
// attempts/backoff budget as check; an applied-or-noop outcome records its
// result verbatim.
func (s *Service) executeUpdateJob(ctx context.Context, job *BlueprintUpdateJob) {
	outcome, err := s.applyBlueprintUpdate(ctx, job.ProjectID, job.BlueprintName, job.MergeTypeConflicts)
	if err != nil {
		if isNotFound(err) {
			// No longer applied / not a GitHub blueprint: nothing to update.
			_ = s.repo.CompleteUpdateJob(ctx, job.ID, json.RawMessage(`{"skipped":"no applied github blueprint"}`))
			return
		}
		// An object-type collision is a user decision, not a transient failure:
		// fail immediately (no retry budget) and persist the conflict details so
		// the caller can preview/merge explicitly.
		if isConflict(err) {
			_ = s.repo.FailUpdateJobWithResult(ctx, job.ID, err.Error(), updateConflictResult(err))
			return
		}
		if job.Attempts >= updateJobMaxAttempts {
			_ = s.repo.FailUpdateJob(ctx, job.ID, err.Error())
		} else {
			_ = s.repo.RequeueUpdateJob(ctx, job.ID, err.Error())
		}
		return
	}

	result, _ := json.Marshal(outcome)
	_ = s.repo.CompleteUpdateJob(ctx, job.ID, result)
}

// applyBlueprintUpdate fetches the upstream manifest for one applied GitHub
// blueprint and, when a strictly newer version is available, creates a new
// version carrying the same source provenance, publishes it, and applies
// (merges) it into the project. It returns a descriptive outcome map recorded
// verbatim in the job result. ErrNotFound signals the blueprint is no longer an
// applied GitHub blueprint.
func (s *Service) applyBlueprintUpdate(ctx context.Context, projectID, blueprintName string, mergeTypeConflicts bool) (map[string]any, error) {
	src, err := s.repo.GetAppliedBlueprintSource(ctx, projectID, blueprintName)
	if err != nil {
		return nil, err
	}

	desc, manifest, err := s.fetchUpstreamManifest(ctx, projectID, src)
	if err != nil {
		return nil, err
	}

	newer := updateAvailable(
		src.AppliedVersion, desc.Version,
		canonicalManifestHash(src.AppliedManifest), canonicalManifestHash(manifest),
	)
	if !newer {
		return map[string]any{
			"noop":           true,
			"appliedVersion": src.AppliedVersion,
			"latestVersion":  desc.Version,
		}, nil
	}
	if desc.Version == src.AppliedVersion {
		// Same version, different content: published blueprints are immutable and
		// (name, version) is unique, so there is no new row to mint. Report a
		// no-op rather than colliding on the existing version; the next upstream
		// version bump applies normally.
		return map[string]any{
			"noop":           true,
			"reason":         "content drift at same version (requires an upstream version bump)",
			"appliedVersion": src.AppliedVersion,
			"latestVersion":  desc.Version,
		}, nil
	}

	// Object-type collision gate BEFORE creating/publishing the new version. A
	// blocked update must be a true no-op: without this pre-check the new
	// blueprint row would be created and published, then Apply would refuse —
	// leaving version state behind despite a conflict failure. Apply keeps its
	// own gate as defense in depth for every other apply entrypoint.
	if !mergeTypeConflicts {
		conflicts, err := s.PreviewManifestConflicts(ctx, projectID, manifest, OriginImport)
		if err != nil {
			return nil, err
		}
		if len(conflicts) > 0 {
			return nil, apperror.NewConflict("object type name conflicts require merge confirmation").
				WithDetails(map[string]any{"conflicts": conflicts})
		}
	}

	var scope *string
	if projectID != "" {
		scope = &projectID
	}
	bp, createErr := s.CreateBlueprint(ctx, &CreateBlueprintRequest{
		Name:        blueprintName,
		Version:     desc.Version,
		Description: desc.Description,
		Author:      desc.Author,
		ProjectID:   scope,
		Manifest:    manifest,
		SourceKind:  "github",
		SourceURL:   src.SourceURL,
		SourceRef:   src.SourceRef,
	})
	if createErr != nil {
		if !isConflict(createErr) {
			return nil, createErr
		}
		// Already created by a previous attempt (same name+version): continue
		// with the existing row so a retry converges.
		bp, err = s.repo.GetByNameVersion(ctx, projectID, blueprintName, desc.Version)
		if err != nil {
			return nil, err
		}
	}

	if bp.Status == StatusDraft {
		if _, err := s.PublishBlueprint(ctx, projectID, bp.ID); err != nil {
			return nil, err
		}
	}

	if _, err := s.Apply(ctx, bp.ID, projectID, "", ApplyOptions{MergeTypeConflicts: mergeTypeConflicts}); err != nil {
		return nil, err
	}

	return map[string]any{
		"noop":            false,
		"applied":         true,
		"blueprintId":     bp.ID,
		"version":         desc.Version,
		"previousVersion": src.AppliedVersion,
	}, nil
}

// BlueprintUpdatePreview is the read-only result of previewing an upstream
// blueprint update: the versions involved and the object-type collisions an
// apply would hit. It creates/publishes nothing.
type BlueprintUpdatePreview struct {
	BlueprintName  string                         `json:"blueprintName"`
	CurrentVersion string                         `json:"currentVersion"`
	LatestVersion  string                         `json:"latestVersion"`
	RequiresMerge  bool                           `json:"requiresMerge"`
	Conflicts      []schemas.MergePreviewConflict `json:"conflicts"`
}

// PreviewBlueprintUpdate fetches the upstream manifest for one applied GitHub
// blueprint and reports its object-type conflicts against the project WITHOUT
// creating, publishing, or applying a new version.
func (s *Service) PreviewBlueprintUpdate(ctx context.Context, projectID, blueprintName string) (*BlueprintUpdatePreview, error) {
	src, err := s.repo.GetAppliedBlueprintSource(ctx, projectID, blueprintName)
	if err != nil {
		return nil, err
	}

	desc, manifest, err := s.fetchUpstreamManifest(ctx, projectID, src)
	if err != nil {
		return nil, err
	}

	conflicts, err := s.PreviewManifestConflicts(ctx, projectID, manifest, OriginImport)
	if err != nil {
		return nil, err
	}
	return &BlueprintUpdatePreview{
		BlueprintName:  blueprintName,
		CurrentVersion: src.AppliedVersion,
		LatestVersion:  desc.Version,
		RequiresMerge:  len(conflicts) > 0,
		Conflicts:      conflicts,
	}, nil
}

// fetchUpstreamManifest downloads and parses the upstream blueprint for one
// applied GitHub blueprint, returning its descriptor and manifest. Shared by
// applyBlueprintUpdate and PreviewBlueprintUpdate so the fetch/load path cannot
// drift. The token is used only for the outbound fetch and never logged.
func (s *Service) fetchUpstreamManifest(ctx context.Context, projectID string, src *appliedBlueprintSource) (*blueprintDescriptor, json.RawMessage, error) {
	org, repo, ref, err := parseGitHubURL(src.SourceURL, src.SourceRef)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid source URL: %v", err)
	}

	token, err := s.decryptGithubToken(ctx, projectID)
	if err != nil {
		return nil, nil, err
	}

	fetcher := s.fetcher
	if fetcher == nil {
		fetcher = newGitHubFetcher()
	}
	root, cleanup, err := fetcher.Fetch(ctx, org, repo, ref, token)
	if err != nil {
		if token == "" && isRepoNotFound(err) {
			err = fmt.Errorf("no GitHub credential configured (repository may be private): %w", err)
		}
		return nil, nil, err
	}
	defer cleanup()

	desc, err := loadBlueprintDir(os.DirFS(root), ".", repo)
	if err != nil {
		return nil, nil, err
	}
	manifest, err := json.Marshal(desc.BlueprintManifest)
	if err != nil {
		return nil, nil, fmt.Errorf("manifest not serializable: %v", err)
	}
	return desc, manifest, nil
}

// updateConflictResult builds the job Result JSON for a terminal object-type
// conflict, preserving the structured conflict details returned by Apply.
func updateConflictResult(err error) json.RawMessage {
	payload := map[string]any{
		"conflict":      true,
		"requiresMerge": true,
		"message":       err.Error(),
	}
	var apErr *apperror.Error
	if errors.As(err, &apErr) {
		payload["message"] = apErr.Message
		if apErr.Details != nil {
			if conflicts, ok := apErr.Details["conflicts"]; ok {
				payload["conflicts"] = conflicts
			}
		}
	}
	out, _ := json.Marshal(payload)
	return out
}

// checkBlueprint fetches the upstream manifest for one applied GitHub blueprint,
// compares it against the applied snapshot, and persists the result. It returns
// ErrNotFound when the blueprint is no longer an applied GitHub blueprint.
func (s *Service) checkBlueprint(ctx context.Context, projectID, blueprintName string) (*BlueprintUpdateCheck, error) {
	src, err := s.repo.GetAppliedBlueprintSource(ctx, projectID, blueprintName)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	check := &BlueprintUpdateCheck{
		ProjectID:     projectID,
		BlueprintName: blueprintName,
		SourceURL:     src.SourceURL,
		SourceRef:     src.SourceRef,
		LastCheckedAt: &now,
		UpdatedAt:     now,
	}

	org, repo, ref, err := parseGitHubURL(src.SourceURL, src.SourceRef)
	if err != nil {
		check.LastCheckError = fmt.Sprintf("invalid source URL: %v", err)
		_ = s.repo.MarkUpdateCheckError(ctx, check)
		return check, err
	}

	token, err := s.decryptGithubToken(ctx, projectID)
	if err != nil {
		check.LastCheckError = err.Error()
		_ = s.repo.MarkUpdateCheckError(ctx, check)
		return check, err
	}

	fetcher := s.fetcher
	if fetcher == nil {
		fetcher = newGitHubFetcher()
	}
	root, cleanup, err := fetcher.Fetch(ctx, org, repo, ref, token)
	if err != nil {
		if token == "" && isRepoNotFound(err) {
			err = fmt.Errorf("no GitHub credential configured (repository may be private): %w", err)
		}
		check.LastCheckError = err.Error()
		_ = s.repo.MarkUpdateCheckError(ctx, check)
		return check, err
	}
	defer cleanup()

	desc, err := loadBlueprintDir(os.DirFS(root), ".", repo)
	if err != nil {
		check.LastCheckError = err.Error()
		_ = s.repo.MarkUpdateCheckError(ctx, check)
		return check, err
	}

	manifest, err := json.Marshal(desc.BlueprintManifest)
	if err != nil {
		check.LastCheckError = fmt.Sprintf("manifest not serializable: %v", err)
		_ = s.repo.MarkUpdateCheckError(ctx, check)
		return check, err
	}

	check.LatestVersion = desc.Version
	check.LatestChecksum = manifestChecksum(manifest)
	// Content baseline: prefer the raw upstream hash recorded at import (so a
	// locally-overridden manifest still compares equal to its upstream); fall
	// back to the applied manifest for imports predating that provenance.
	appliedHash := src.AppliedSourceHash
	if appliedHash == "" {
		appliedHash = canonicalManifestHash(src.AppliedManifest)
	}
	check.UpdateAvailable = updateAvailable(
		src.AppliedVersion, check.LatestVersion,
		appliedHash, canonicalManifestHash(manifest),
	)
	if err := s.repo.UpsertUpdateCheck(ctx, check); err != nil {
		return check, err
	}
	return check, nil
}
