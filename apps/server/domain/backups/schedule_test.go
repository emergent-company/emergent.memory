package backups

import (
	"strings"
	"testing"
	"time"
)

func TestValidateBackupCron(t *testing.T) {
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	valid := []string{
		"0 2 * * *",    // daily 02:00
		"0 */6 * * *",  // every 6 hours
		"*/15 * * * *", // exactly the minimum interval
		"0 2 * * 0",    // weekly Sunday
		"30 3 1 * *",   // monthly
		"0,30 * * * *", // twice an hour
	}
	for _, expr := range valid {
		if err := ValidateBackupCron(expr, from); err != nil {
			t.Errorf("ValidateBackupCron(%q) = %v, want nil", expr, err)
		}
	}

	invalid := map[string]string{
		"":            "required",
		"   ":         "required",
		"0 2 * *":     "5 fields",
		"0 0 2 * * *": "5 fields",
		"*/5 * * * *": "minimum interval",
		"* * * * *":   "minimum interval",
		// A short gap hidden behind a long one: 00:05, 00:55, 01:05, 01:55 is a
		// 50-minute then a 10-minute gap, so checking only the first interval
		// would accept it.
		"5,55 0,1 * * *": "minimum interval",
		"not a schedule": "5 fields",
		"99 2 * * *":     "invalid cron",
	}
	for expr, want := range invalid {
		err := ValidateBackupCron(expr, from)
		if err == nil {
			t.Fatalf("ValidateBackupCron(%q) = nil, want error containing %q", expr, want)
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("ValidateBackupCron(%q) = %q, want it to contain %q", expr, err.Error(), want)
		}
	}
}

func TestNextBackupRun_IsUTC(t *testing.T) {
	from := time.Date(2026, 1, 1, 3, 0, 0, 0, time.UTC)

	next, err := NextBackupRun("0 2 * * *", from)
	if err != nil {
		t.Fatalf("NextBackupRun: %v", err)
	}
	want := time.Date(2026, 1, 2, 2, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("next = %s, want %s", next, want)
	}
	if next.Location() != time.UTC {
		t.Fatalf("next location = %s, want UTC", next.Location())
	}

	// A fire time later the same day is picked over tomorrow's.
	later, err := NextBackupRun("30 5 * * *", from)
	if err != nil {
		t.Fatalf("NextBackupRun: %v", err)
	}
	if want := time.Date(2026, 1, 1, 5, 30, 0, 0, time.UTC); !later.Equal(want) {
		t.Fatalf("next = %s, want %s", later, want)
	}
}

func TestNextBackupRun_StrictlyAfter(t *testing.T) {
	// Every 15 minutes: from exactly on a slot the next fire is the following
	// slot, so a claim taken at the slot time cannot re-fire the same instant.
	from := time.Date(2026, 1, 1, 0, 15, 0, 0, time.UTC)
	next, err := NextBackupRun("*/15 * * * *", from)
	if err != nil {
		t.Fatalf("NextBackupRun: %v", err)
	}
	if want := time.Date(2026, 1, 1, 0, 30, 0, 0, time.UTC); !next.Equal(want) {
		t.Fatalf("next = %s, want %s", next, want)
	}
}
