package sources

import (
	"context"
	"errors"
	"time"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// Probe policy (#23). These are variables rather than constants so the tests can
// shrink them and drive the cadence with a fixed clock; production values are
// the ones below.
var (
	// probeCadence is how often one eligible source's grant may be revalidated
	// without a sync. An hourly cadence means a grant that dies between syncs is
	// surfaced within an hour, instead of at the next daily sync — the whole
	// point of the probe.
	probeCadence = time.Hour

	// probeIntervalThreshold is the sync interval at or above which a source is
	// "long-interval" enough to warrant probing. A source syncing at least every
	// six hours already phones home four times a day; probing it hourly would add
	// 20 more provider calls for no earlier answer. Below the threshold the sync
	// itself is the probe.
	probeIntervalThreshold = 6 * time.Hour

	// probeSweepInterval is how often the scheduler looks for sources whose probe
	// is due. It is shorter than probeCadence so an hourly cadence does not lose
	// an hour to sweep jitter.
	probeSweepInterval = 15 * time.Minute

	// probeTimeout bounds one provider call. A hung provider must not hold the
	// sweep, and the call is a metadata read, so 30 seconds is generous.
	probeTimeout = 30 * time.Second
)

// shouldProbe reports whether src's grant is due for a proactive probe at now.
// It is the cadence policy as a pure function: eligibility (a scheduled, active,
// long-interval source) plus the per-source clock (last_probed_at). Keeping it
// separate from the provider call is what makes the cadence testable without a
// database or a network.
func shouldProbe(now time.Time, src *Source, cadence, threshold time.Duration) bool {
	if src == nil {
		return false
	}
	if src.Status != SourceStatusActive || src.SyncPolicy != SyncPolicyScheduled {
		return false
	}
	if src.SyncIntervalMinutes == nil {
		return false
	}
	if time.Duration(*src.SyncIntervalMinutes)*time.Minute < threshold {
		return false
	}
	if src.LastProbedAt != nil && now.Sub(*src.LastProbedAt) < cadence {
		return false
	}
	return true
}

// ProbeDueSources revalidates the grants of long-interval sources whose probe is
// due, parking a source whose provider rejects its stored credential. It is the
// scheduler entry point and is best-effort: one provider failure is logged and
// does not stop the sweep, and the only returned error is a failure to list the
// candidates.
//
// A probe is not a sync. It reads no content, moves no cursor, and never records
// a sync failure. Its only durable effects are last_probed_at and, when the grant
// is dead, the same park + notification a failed sync would have produced.
func (s *Service) ProbeDueSources(ctx context.Context) error {
	if s.repo == nil {
		return nil
	}
	now := s.now()
	candidates, err := s.repo.ListProbeCandidates(ctx, int(probeIntervalThreshold/time.Minute))
	if err != nil {
		return err
	}

	probed := 0
	for _, src := range candidates {
		if !shouldProbe(now, src, probeCadence, probeIntervalThreshold) {
			continue
		}
		// A source with a job in flight is about to answer the same question the
		// probe would ask, and parking it under the running job would race the
		// epoch guard. Leave it to the sync.
		active, aErr := s.repo.GetActiveSyncJob(ctx, src.ID)
		if aErr != nil {
			s.log.Warn("failed to check active sync job before probing",
				"source_id", src.ID.String(), "error", aErr)
			continue
		}
		if active != nil {
			continue
		}
		if s.probeSource(ctx, src) {
			probed++
		}
	}
	if probed > 0 {
		s.log.Info("probed source grants", "count", probed)
	}
	return nil
}

// probeSource asks one provider whether the source's stored grant is still
// accepted. It reports whether a probe actually ran (the provider may have no
// cheap probe; the row may have been deleted). On a reconnect-required verdict
// the source is parked through the same epoch-guarded statement the worker uses,
// and the committed park publishes the actionable notification.
func (s *Service) probeSource(ctx context.Context, listed *Source) bool {
	p, ok := s.providers.Get(listed.Provider)
	if !ok {
		s.log.Warn("cannot probe source: unknown provider",
			"source_id", listed.ID.String(), "provider", listed.Provider)
		return false
	}
	prober, ok := p.(GrantProber)
	if !ok {
		// The source stays eligible; the next sweep simply skips it again. This is
		// deliberate: marking it probed would hide a provider capability gap.
		return false
	}

	// Re-read with credentials: the candidate listing excludes the blob so a
	// sweep does not decrypt what it will not use.
	src, err := s.repo.GetByIDWithCredentials(ctx, listed.ID, listed.ProjectID)
	if err != nil {
		if !errors.Is(err, apperror.ErrNotFound) {
			s.log.Warn("failed to load source for probe",
				"source_id", listed.ID.String(), "error", err)
		}
		return false
	}

	creds, err := s.credentialsForSource(ctx, src)
	if err != nil {
		s.log.Warn("failed to resolve source credentials for probe",
			"source_id", src.ID.String(), "error", err)
		s.markSourceProbed(ctx, src)
		return true
	}

	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	err = prober.ProbeGrant(probeCtx, creds, s.credentialDeps(src.ProjectID, src.ID))
	// Record the attempt whatever the outcome: a failing provider must not be
	// probed on every sweep.
	s.markSourceProbed(ctx, src)

	switch {
	case err == nil:
		s.log.Debug("source grant probe succeeded", "source_id", src.ID.String())
	case IsReconnectRequired(err):
		applied, pErr := s.repo.MarkSourceNeedsReconnect(ctx, src.ProjectID, src.ID, err.Error(), credentialEpoch(src))
		if pErr != nil {
			s.log.Error("failed to park source after a rejected grant probe",
				"source_id", src.ID.String(), "error", pErr)
		} else if applied {
			s.log.Info("source grant probe parked the source", "source_id", src.ID.String())
			// The park committed, so the member has to be told. Best-effort, exactly
			// like the sync worker's path: delivery never changes the park.
			s.publishSourceNeedsReconnect(ctx, src, err.Error())
		}
	default:
		// A transient provider failure (5xx, timeout, a deleted sync target) says
		// nothing about the grant: do not record it as a source error and do not
		// park. The next sync is still the authority on the sync outcome.
		s.log.Warn("source grant probe failed without a reconnect verdict",
			"source_id", src.ID.String(),
			"provider", src.Provider,
			"error", err)
	}
	return true
}

// markSourceProbed writes the per-source probe clock. It is best-effort: losing
// the write costs one extra probe next sweep, never a lifecycle decision.
func (s *Service) markSourceProbed(ctx context.Context, src *Source) {
	if err := s.repo.MarkSourceProbed(ctx, src.ProjectID, src.ID, s.now()); err != nil {
		s.log.Warn("failed to record source probe", "source_id", src.ID.String(), "error", err)
	}
}
