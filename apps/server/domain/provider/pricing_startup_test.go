package provider

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"go.uber.org/fx"
)

// recordingLifecycle captures the hooks registered on the fx lifecycle so a
// startup hook can be exercised directly in a test.
type recordingLifecycle struct {
	hooks []fx.Hook
}

func (l *recordingLifecycle) Append(h fx.Hook) { l.hooks = append(l.hooks, h) }

// ctxObservingPricingRepo records the Context error observed on each upsert, so
// a test can prove the startup sync does not run with a cancelled context.
type ctxObservingPricingRepo struct {
	mu       sync.Mutex
	ctxErrs  []error
	upserts  int
	pruneErr error
}

func (r *ctxObservingPricingRepo) UpsertPricing(ctx context.Context, _ []ProviderPricing) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.upserts++
	r.ctxErrs = append(r.ctxErrs, ctx.Err())
	return nil
}

func (r *ctxObservingPricingRepo) PruneStalePricing(context.Context, []ProviderType, []ProviderPricing) (int64, error) {
	return 0, r.pruneErr
}

// gatedPricingFetcher signals when the fetch started and blocks until released,
// simulating the up-to-30s models.dev HTTP call.
type gatedPricingFetcher struct {
	started chan struct{}
	release chan struct{}
}

func (f *gatedPricingFetcher) FetchRates(context.Context) ([]fetchedPricing, error) {
	close(f.started)
	<-f.release
	return nil, errors.New("feed unavailable")
}

// TestRunStartupPricingSync_OutlivesStartupContext is the regression guard for
// #1452: the fx OnStart context is cancelled as soon as start-up completes, so
// handing it to the background sync aborts the fetch and the baseline upsert,
// leaving kb.provider_pricing unseeded until the daily cron. The startup sync
// must use its own bounded context.
func TestRunStartupPricingSync_OutlivesStartupContext(t *testing.T) {
	repo := &ctxObservingPricingRepo{}
	fetcher := &gatedPricingFetcher{started: make(chan struct{}), release: make(chan struct{})}
	svc := &PricingSyncService{repo: repo, fetcher: fetcher, log: slog.Default()}

	lc := &recordingLifecycle{}
	runStartupPricingSync(lc, svc, slog.Default())
	if len(lc.hooks) != 1 {
		t.Fatalf("registered %d lifecycle hooks, want 1", len(lc.hooks))
	}
	if lc.hooks[0].OnStart == nil {
		t.Fatal("startup pricing hook has no OnStart")
	}

	startCtx, cancelStart := context.WithCancel(context.Background())
	if err := lc.hooks[0].OnStart(startCtx); err != nil {
		t.Fatalf("OnStart() error = %v", err)
	}

	// Wait until the fetch is in flight, then cancel the fx start context (as
	// production does once start-up returns) and release the fetch.
	select {
	case <-fetcher.started:
	case <-time.After(5 * time.Second):
		t.Fatal("fetch never started")
	}
	cancelStart()
	close(fetcher.release)

	deadline := time.After(5 * time.Second)
	for {
		repo.mu.Lock()
		n := repo.upserts
		repo.mu.Unlock()
		if n > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("baseline upsert never ran after the startup context was cancelled")
		case <-time.After(10 * time.Millisecond):
		}
	}

	repo.mu.Lock()
	defer repo.mu.Unlock()
	for i, err := range repo.ctxErrs {
		if err != nil {
			t.Fatalf("upsert %d ran with cancelled context (%v); startup sync must own its context", i, err)
		}
	}
}
