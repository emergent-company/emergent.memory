package sources

import (
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/internal/outbound"
)

// TestDefaultSyncMode pins the policy a new source gets. An explicit mode always
// wins: a Manual source created through the connect form must never inherit a
// provider default, because that is exactly what made "manual" sources sync on
// the scheduler sweep.
func TestDefaultSyncMode(t *testing.T) {
	web := newWebProvider(outbound.DefaultPolicy())
	gmail := newGmailProvider("", "")

	manual, scheduled, blank, recurring, bogus := SyncPolicyManual, SyncPolicyScheduled, "", "recurring", "hourly"

	cases := []struct {
		name      string
		provider  SourceProvider
		requested *string
		want      string
		wantErr   bool
	}{
		{name: "explicit manual beats the web default", provider: web, requested: &manual, want: SyncPolicyManual},
		{name: "explicit manual beats the gmail default", provider: gmail, requested: &manual, want: SyncPolicyManual},
		{name: "explicit scheduled is honoured", provider: gmail, requested: &scheduled, want: SyncPolicyScheduled},
		{name: "documented recurring alias is scheduled", provider: web, requested: &recurring, want: SyncPolicyScheduled},
		{name: "unknown mode is rejected", provider: web, requested: &bogus, wantErr: true},
		{name: "explicit scheduled on an unbounded provider", provider: newGoogleDriveProvider(), requested: &scheduled, want: SyncPolicyScheduled},
		{name: "empty mode falls back to the provider default", provider: web, requested: &blank, want: SyncPolicyScheduled},
		{name: "no mode on a bounded provider is scheduled", provider: web, requested: nil, want: SyncPolicyScheduled},
		{name: "no mode on gmail is scheduled", provider: gmail, requested: nil, want: SyncPolicyScheduled},
		{name: "no mode on drive is manual", provider: newGoogleDriveProvider(), requested: nil, want: SyncPolicyManual},
		{name: "no mode on github is manual", provider: newGitHubProvider(""), requested: nil, want: SyncPolicyManual},
		{name: "no mode on trale is manual", provider: newTraleProvider(), requested: nil, want: SyncPolicyManual},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := defaultSyncMode(tc.provider, tc.requested)
			if tc.wantErr {
				if err == nil {
					t.Fatal("defaultSyncMode() = nil error, want a validation error")
				}
				return
			}
			if err != nil {
				t.Fatalf("defaultSyncMode() error = %v", err)
			}
			if got != tc.want {
				t.Errorf("defaultSyncMode() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestNormalizeSyncMode pins the accepted wire values: the canonical two, the
// documented `recurring` alias, and an empty value (unset). Anything else is a
// validation error rather than a stored policy the sweep would never admit.
func TestNormalizeSyncMode(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "", want: ""},
		{in: "manual", want: SyncPolicyManual},
		{in: "scheduled", want: SyncPolicyScheduled},
		{in: "recurring", want: SyncPolicyScheduled},
		{in: " scheduled ", want: SyncPolicyScheduled},
		{in: "hourly", wantErr: true},
		{in: "MANUAL", wantErr: true},
	}
	for _, tc := range cases {
		got, err := NormalizeSyncMode(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("NormalizeSyncMode(%q) = %q, want error", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("NormalizeSyncMode(%q) error = %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("NormalizeSyncMode(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestApplyScheduledInterval pins that the mode owns the interval: manual stores
// nothing, scheduled always stores a cadence.
func TestApplyScheduledInterval(t *testing.T) {
	web := newWebProvider(outbound.DefaultPolicy())
	drive := newGoogleDriveProvider()

	cases := []struct {
		name       string
		provider   SourceProvider
		mode       string
		interval   *int
		wantVal    int
		wantNil    bool
		wantErr    bool
		wantErrSub string
	}{
		{name: "manual drops a submitted interval", provider: web, mode: SyncPolicyManual, interval: intPtr(30), wantNil: true},
		{name: "manual without an interval stays nil", provider: web, mode: SyncPolicyManual, interval: nil, wantNil: true},
		{name: "scheduled inherits the provider default", provider: web, mode: SyncPolicyScheduled, interval: nil, wantVal: 60},
		{name: "scheduled keeps a submitted interval", provider: web, mode: SyncPolicyScheduled, interval: intPtr(30), wantVal: 30},
		{name: "scheduled below the provider minimum is rejected", provider: web, mode: SyncPolicyScheduled, interval: intPtr(5), wantErr: true, wantErrSub: "at least 15 minutes"},
		{name: "scheduled on an unbounded provider gets the package default", provider: drive, mode: SyncPolicyScheduled, interval: nil, wantVal: defaultSyncIntervalMinutes},
		{name: "scheduled on an unbounded provider keeps a submitted interval", provider: drive, mode: SyncPolicyScheduled, interval: intPtr(5), wantVal: 5},
		{name: "manual on an unbounded provider drops a submitted interval", provider: drive, mode: SyncPolicyManual, interval: intPtr(60), wantNil: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := applyScheduledInterval(tc.provider, tc.mode, tc.interval)
			if tc.wantErr {
				if err == nil {
					t.Fatal("applyScheduledInterval() = nil, want error")
				}
				if tc.wantErrSub != "" && !strings.Contains(err.Error(), tc.wantErrSub) {
					t.Errorf("applyScheduledInterval() error = %q, want it to contain %q", err, tc.wantErrSub)
				}
				return
			}
			if err != nil {
				t.Fatalf("applyScheduledInterval() error = %v", err)
			}
			if tc.wantNil {
				if got != nil {
					t.Errorf("applyScheduledInterval() = %v, want nil", *got)
				}
				return
			}
			if got == nil || *got != tc.wantVal {
				t.Errorf("applyScheduledInterval() = %v, want %d", got, tc.wantVal)
			}
		})
	}
}
