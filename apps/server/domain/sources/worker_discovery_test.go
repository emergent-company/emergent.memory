package sources

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
)

// fakeDiscoveryStarter records the calls the sync worker's gate makes. It is a
// pure unit double: the gate is the toggle's only enforcement point, so the
// three spec scenarios (enabled → delegate, disabled → skip, lookup error →
// skip) must be pinned without a database.
type fakeDiscoveryStarter struct {
	enabled     bool
	lookupErr   error
	startErr    error
	lookupCalls int
	startCalls  int
	gotProject  uuid.UUID
	gotIDs      []uuid.UUID
}

func (f *fakeDiscoveryStarter) AutoDiscoveryEnabled(ctx context.Context, projectID uuid.UUID) (bool, error) {
	f.lookupCalls++
	return f.enabled, f.lookupErr
}

func (f *fakeDiscoveryStarter) StartDiscovery(ctx context.Context, projectID uuid.UUID, documentIDs []uuid.UUID) error {
	f.startCalls++
	f.gotProject = projectID
	f.gotIDs = documentIDs
	return f.startErr
}

// TestDelegateDiscovery pins the sync worker's discovery gate: the project
// setting decides whether a sync spends model calls, and an unreadable setting
// fails closed instead of guessing.
func TestDelegateDiscovery(t *testing.T) {
	projectID := uuid.New()
	docA := uuid.New()
	docB := uuid.New()

	cases := []struct {
		name string
		// fake configuration
		starter   *fakeDiscoveryStarter
		noStarter bool
		// sync outcome
		created []string
		// expectations
		wantLookup string // "yes" | "no"
		wantStart  bool
		wantIDs    []uuid.UUID
	}{
		{
			name:       "setting on delegates the ingested documents",
			starter:    &fakeDiscoveryStarter{enabled: true},
			created:    []string{docA.String(), docB.String()},
			wantLookup: "yes",
			wantStart:  true,
			wantIDs:    []uuid.UUID{docA, docB},
		},
		{
			name:       "setting off skips discovery",
			starter:    &fakeDiscoveryStarter{enabled: false},
			created:    []string{docA.String()},
			wantLookup: "yes",
			wantStart:  false,
		},
		{
			name:       "unreadable setting fails closed",
			starter:    &fakeDiscoveryStarter{enabled: true, lookupErr: errors.New("db is down")},
			created:    []string{docA.String()},
			wantLookup: "yes",
			wantStart:  false,
		},
		{
			name:       "a start failure does not retry or fail the sync",
			starter:    &fakeDiscoveryStarter{enabled: true, startErr: errors.New("409 discovery already running")},
			created:    []string{docA.String()},
			wantLookup: "yes",
			wantStart:  true,
			wantIDs:    []uuid.UUID{docA},
		},
		{
			name:       "no new documents makes no call at all",
			starter:    &fakeDiscoveryStarter{enabled: true},
			created:    nil,
			wantLookup: "no",
			wantStart:  false,
		},
		{
			name:       "unparsable document ids are dropped",
			starter:    &fakeDiscoveryStarter{enabled: true},
			created:    []string{"not-a-uuid", docA.String()},
			wantLookup: "yes",
			wantStart:  true,
			wantIDs:    []uuid.UUID{docA},
		},
		{
			name:       "only unparsable ids start nothing",
			starter:    &fakeDiscoveryStarter{enabled: true},
			created:    []string{"nope"},
			wantLookup: "yes",
			wantStart:  false,
		},
		{
			name:       "a container without the discovery module skips silently",
			noStarter:  true,
			created:    []string{docA.String()},
			wantLookup: "no",
			wantStart:  false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := &SyncWorker{
				log: slog.New(slog.NewTextHandler(io.Discard, nil)),
			}
			if !tc.noStarter {
				w.discovery = tc.starter
			}

			w.delegateDiscovery(context.Background(), projectID, tc.created, w.log)

			if tc.starter != nil {
				wantLookups := 0
				if tc.wantLookup == "yes" {
					wantLookups = 1
				}
				if tc.starter.lookupCalls != wantLookups {
					t.Fatalf("setting lookups = %d, want %d", tc.starter.lookupCalls, wantLookups)
				}
				if tc.wantStart != (tc.starter.startCalls > 0) {
					t.Fatalf("StartDiscovery called %d times, want called=%v", tc.starter.startCalls, tc.wantStart)
				}
				if tc.starter.startCalls > 1 {
					t.Fatalf("StartDiscovery called %d times; a failed delegation must not be retried", tc.starter.startCalls)
				}
				if tc.wantStart {
					if tc.starter.gotProject != projectID {
						t.Fatalf("project = %s, want %s", tc.starter.gotProject, projectID)
					}
					if len(tc.starter.gotIDs) != len(tc.wantIDs) {
						t.Fatalf("document ids = %v, want %v", tc.starter.gotIDs, tc.wantIDs)
					}
					for i := range tc.wantIDs {
						if tc.starter.gotIDs[i] != tc.wantIDs[i] {
							t.Fatalf("document ids = %v, want %v", tc.starter.gotIDs, tc.wantIDs)
						}
					}
				}
			}
		})
	}
}
