package sources

import (
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/internal/outbound"
)

// TestApplySyncIntervalBounds covers the web provider's sync-cadence bounds:
// the default applied when a source omits the interval, the minimum rejection,
// and the pass-through behaviour for providers without the interface.
func TestApplySyncIntervalBounds(t *testing.T) {
	web := newWebProvider(outbound.DefaultPolicy())

	cases := []struct {
		name       string
		provider   SourceProvider
		interval   *int
		wantVal    int
		wantNil    bool
		wantErr    bool
		wantErrSub string
	}{
		{
			name:     "web default applied when omitted",
			provider: web,
			interval: nil,
			wantVal:  60,
		},
		{
			name:       "web below minimum rejected",
			provider:   web,
			interval:   intPtr(5),
			wantErr:    true,
			wantErrSub: "at least 15 minutes",
		},
		{
			name:     "web above minimum passes through",
			provider: web,
			interval: intPtr(30),
			wantVal:  30,
		},
		{
			name:     "non-bound provider nil stays nil",
			provider: newGoogleDriveProvider(),
			interval: nil,
			wantNil:  true,
		},
		{
			name:     "non-bound provider value passes through",
			provider: newGoogleDriveProvider(),
			interval: intPtr(5),
			wantVal:  5,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := applySyncIntervalBounds(tc.provider, tc.interval)
			if tc.wantErr {
				if err == nil {
					t.Fatal("applySyncIntervalBounds() = nil, want error")
				}
				if tc.wantErrSub != "" && !strings.Contains(err.Error(), tc.wantErrSub) {
					t.Errorf("applySyncIntervalBounds() error = %q, want it to contain %q", err, tc.wantErrSub)
				}
				return
			}
			if err != nil {
				t.Fatalf("applySyncIntervalBounds() error = %v", err)
			}
			if tc.wantNil {
				if got != nil {
					t.Errorf("applySyncIntervalBounds() = %v, want nil", got)
				}
				return
			}
			if got == nil || *got != tc.wantVal {
				t.Errorf("applySyncIntervalBounds() = %v, want %d", got, tc.wantVal)
			}
		})
	}
}

// TestWebProviderImplementsSyncIntervalBounds asserts the web provider opts in
// while the other shipped providers leave the default (unbounded) behaviour.
func TestWebProviderImplementsSyncIntervalBounds(t *testing.T) {
	if _, ok := any(newWebProvider(outbound.DefaultPolicy())).(SyncIntervalBoundsProvider); !ok {
		t.Error("web provider must implement SyncIntervalBoundsProvider")
	}
	for name, p := range map[string]SourceProvider{
		"google_drive": newGoogleDriveProvider(),
		"github":       newGitHubProvider(""),
		"trale":        newTraleProvider(),
	} {
		if _, ok := p.(SyncIntervalBoundsProvider); ok {
			t.Errorf("%s provider must not implement SyncIntervalBoundsProvider", name)
		}
	}
}

func intPtr(v int) *int { return &v }
