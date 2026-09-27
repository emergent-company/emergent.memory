package schemadrift

import (
	"strings"
	"testing"
)

// TestCheckRegistryClean proves the deterministic, DB-less registry check that
// the preflight guard (cmd/schemadrift-guard) runs returns no violations on the
// current tree, so the guard stays under `go test` even though its primary
// wiring is the DB-less preflight pipeline. A violation here is the same real
// drift the guard exists to catch: a bun model missing from models.go (or a
// registry entry the census cannot resolve).
func TestCheckRegistryClean(t *testing.T) {
	for _, v := range CheckRegistry() {
		t.Errorf("CheckRegistry: %s", v)
	}
}

// TestCheckRegistryMissesModelSharingExistingTable is the regression test for
// the duplicate-table coverage gap: the registry check must key on model
// identity, not table name, so a model that maps to a table already covered by
// another model is still flagged when it is not registered. It reproduces the
// adversarial probe — a second model on kb.notifications alongside the
// registered notifications.Notification — against the pure registryViolations
// core, so it is deterministic and needs no DB or source-tree mutation.
func TestCheckRegistryMissesModelSharingExistingTable(t *testing.T) {
	const notifTable = "kb.notifications"
	census := []censusModel{
		{
			Table:      notifTable,
			Pkg:        "notifications",
			ImportPath: modulePath + "/domain/notifications",
			Name:       "Notification",
		},
		{
			// Unregistered model sharing the same table as Notification.
			Table:      notifTable,
			Pkg:        "provider",
			ImportPath: modulePath + "/domain/provider",
			Name:       "budgetNotification",
		},
	}
	registered := []modelTable{
		{Schema: "kb", Name: "notifications", Identity: modulePath + "/domain/notifications.Notification"},
	}

	violations := registryViolations(census, registered, nil)

	var found bool
	for _, v := range violations {
		if strings.Contains(v, "provider.budgetNotification") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a violation naming provider.budgetNotification, got %v", violations)
	}
	if len(violations) != 1 {
		t.Fatalf("expected exactly one violation (the unregistered duplicate-table model), got %v", violations)
	}
}

// TestCheckRegistryDuplicateTableBothRegistered proves two models sharing a
// table are both accepted when both are registered — the legitimate
// duplicate-table case (e.g. email.EmailJob and superadmin.EmailJob) must not
// be flagged.
func TestCheckRegistryDuplicateTableBothRegistered(t *testing.T) {
	const emailTable = "kb.email_jobs"
	census := []censusModel{
		{Table: emailTable, Pkg: "email", ImportPath: modulePath + "/domain/email", Name: "EmailJob"},
		{Table: emailTable, Pkg: "superadmin", ImportPath: modulePath + "/domain/superadmin", Name: "EmailJob"},
	}
	registered := []modelTable{
		{Schema: "kb", Name: "email_jobs", Identity: modulePath + "/domain/email.EmailJob"},
		{Schema: "kb", Name: "email_jobs", Identity: modulePath + "/domain/superadmin.EmailJob"},
	}

	violations := registryViolations(census, registered, nil)
	if len(violations) != 0 {
		t.Fatalf("expected no violations when both duplicate-table models are registered, got %v", violations)
	}
}
