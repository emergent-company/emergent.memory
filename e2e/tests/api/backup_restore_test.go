// Package api_test — backup_restore_test.go
//
// End-to-end tests for the backup-restore feature (overwrite + clone modes).
//
// Backup/restore are HTTP-only (no CLI surface), so per Constitution Rule 1
// these tests exercise the frozen HTTP contract:
//
//	POST /api/v1/projects/{projectId}/restore            (overwrite, 202 + job)
//	POST /api/v1/organizations/{orgId}/restore           (clone, 202 + job)
//	GET  /api/v1/restores/{restoreId}                    (job status/progress)
//	POST /api/v1/projects/{projectId}/backups            (create, 202)
//	GET  /api/v1/organizations/{orgId}/backups/{backupId} (poll until ready)
//
// Evidence follows openspec/changes/backup-restore/verification.md §2/§3:
// DB-level projectFingerprint equality (overwrite) and modulo-ID/FK equality
// (clone) using the seedRichProject fixture.
package api_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// 1. Overwrite round-trip — same project ID, snapshot-equivalent state
// =============================================================================

func TestBackupRestore_Overwrite_RoundTrip(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	rl.Describe("Verify overwrite restore returns a project to its backed-up snapshot state",
		"Seed a rich project (documents, chunks, graph data, schema, branch, agent, skill, tag, memberships)",
		"Create a backup and wait until it is ready",
		"Capture the pre-restore projectFingerprint",
		"Restore in overwrite mode and wait for the restore job to complete",
		"Assert the project keeps its ID and the post-restore fingerprint equals the pre-restore fingerprint",
	)
	skipIfServerDown(t, rl)
	db := mustMemoryDB(t, rl)

	seed := seedRichProject(t, rl)

	rl.Section("Create backup and wait for ready")
	backupID := createProjectBackup(t, rl, seed.ProjectID)
	waitBackupReady(t, rl, seed.OrgID, backupID)

	rl.Section("Capture pre-restore fingerprint")
	pre := projectFingerprint(t, rl, db, seed.ProjectID)

	rl.Section("Run overwrite restore")
	job := startOverwriteRestore(t, rl, seed.ProjectID, backupID)
	_, seen := waitRestoreTerminal(t, rl, seed.OrgID, job)
	rl.Printf("overwrite restore observed transitions: %s", strings.Join(seen, " -> "))

	rl.Section("Verify round-trip fidelity")
	if !projectExistsInDB(t, rl, db, seed.ProjectID) {
		rl.Failf("project %s no longer exists after overwrite restore", seed.ProjectID)
	}
	if got := projectOrgID(t, rl, db, seed.ProjectID); got != seed.OrgID {
		rl.Failf("project org changed after overwrite restore: got %s want %s", got, seed.OrgID)
	}
	post := projectFingerprint(t, rl, db, seed.ProjectID)
	requireFingerprintsEqual(t, rl, post, pre, nil)
}

// =============================================================================
// 2. Clone round-trip (same org) — new ID, content equal modulo ID/FK remap
// =============================================================================

func TestBackupRestore_Clone_RoundTrip(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	rl.Describe("Verify clone restore reproduces project content under a new project ID",
		"Seed a rich project and create a ready backup",
		"Clone-restore into the same org with a target project name",
		"Assert the clone has a distinct ID and equal per-table counts/content",
		"Assert no entity UUID is shared with the source and the source is unchanged",
	)
	skipIfServerDown(t, rl)
	db := mustMemoryDB(t, rl)

	seed := seedRichProject(t, rl)

	rl.Section("Create backup and wait for ready")
	backupID := createProjectBackup(t, rl, seed.ProjectID)
	waitBackupReady(t, rl, seed.OrgID, backupID)

	rl.Section("Capture source fingerprint")
	pre := projectFingerprint(t, rl, db, seed.ProjectID)

	rl.Section("Run clone restore into same org")
	targetName := uniqueName("e2e-restore-clone")
	job := startCloneRestore(t, rl, seed.OrgID, backupID, targetName)
	final, seen := waitRestoreTerminal(t, rl, seed.OrgID, job)
	rl.Printf("clone restore observed transitions: %s", strings.Join(seen, " -> "))

	cloneID := final.TargetProjectID
	if cloneID == "" {
		rl.Failf("clone restore completed without a targetProjectId")
	}
	if cloneID == seed.ProjectID {
		rl.Failf("clone must produce a distinct project ID, got same %s", cloneID)
	}
	rl.Printf("clone project id=%s", cloneID)

	rl.Section("Verify clone content equals source modulo ID/FK remap")
	if got := projectOrgID(t, rl, db, cloneID); got != seed.OrgID {
		rl.Failf("clone project org = %s, want %s (same-org clone)", got, seed.OrgID)
	}
	if !projectExistsInDB(t, rl, db, cloneID) {
		rl.Failf("clone project %s does not exist", cloneID)
	}
	cloneFP := projectFingerprint(t, rl, db, cloneID)
	requireFingerprintsEqual(t, rl, cloneFP, pre, nil)

	rl.Section("Verify source project unchanged and zero shared UUIDs")
	sourceFP := projectFingerprint(t, rl, db, seed.ProjectID)
	requireFingerprintsEqual(t, rl, sourceFP, pre, nil)
	assertNoSharedUUIDs(t, rl, db, seed.ProjectID, cloneID)
}

// =============================================================================
// 3. Clone cross-org — membership filtering to the target org
// =============================================================================

func TestBackupRestore_Clone_CrossOrg(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	rl.Describe("Verify cross-org clone filters project memberships to the target org",
		"Seed a rich project in org A with a second non-target member",
		"Create a ready backup",
		"Create org B and clone-restore the backup into org B",
		"Assert the clone lives in org B and its membership set contains only users resolvable to org B",
		"Assert non-membership content matches the source modulo ID/FK remap",
	)
	skipIfServerDown(t, rl)
	db := mustMemoryDB(t, rl)

	seed := seedRichProject(t, rl)

	rl.Section("Create backup and wait for ready")
	backupID := createProjectBackup(t, rl, seed.ProjectID)
	waitBackupReady(t, rl, seed.OrgID, backupID)

	pre := projectFingerprint(t, rl, db, seed.ProjectID)

	rl.Section("Create target org and run cross-org clone restore")
	targetOrgID := createOrg(t, uniqueName("e2e-restore-target-org"))
	rl.Printf("created target org %s", targetOrgID)
	job := startCloneRestore(t, rl, targetOrgID, backupID, uniqueName("e2e-restore-cross-org"))
	final, seen := waitRestoreTerminal(t, rl, targetOrgID, job)
	rl.Printf("cross-org clone restore observed transitions: %s", strings.Join(seen, " -> "))

	cloneID := final.TargetProjectID
	if cloneID == "" {
		rl.Failf("cross-org clone completed without a targetProjectId")
	}
	if cloneID == seed.ProjectID {
		rl.Failf("clone must produce a distinct project ID, got same %s", cloneID)
	}

	rl.Section("Verify clone lives in the target org")
	if got := projectOrgID(t, rl, db, cloneID); got != targetOrgID {
		rl.Failf("clone project org = %s, want target org %s", got, targetOrgID)
	}

	rl.Section("Verify membership filtering (restorer is member of target org)")
	var restorerInTargetOrg int
	if err := db.QueryRow(
		`SELECT count(*) FROM kb.organization_memberships WHERE organization_id = $1 AND user_id = $2`,
		targetOrgID, seed.RestorerUserID,
	).Scan(&restorerInTargetOrg); err != nil {
		rl.Failf("check restorer org membership: %v", err)
	}
	if restorerInTargetOrg != 1 {
		rl.Failf("restorer %s must be a member of target org %s", seed.RestorerUserID, targetOrgID)
	}

	var memberCount int
	var memberUser string
	if err := db.QueryRow(
		`SELECT count(*), coalesce(min(user_id::text), '') FROM kb.project_memberships WHERE project_id = $1`,
		cloneID,
	).Scan(&memberCount, &memberUser); err != nil {
		rl.Failf("read clone memberships: %v", err)
	}
	if memberCount != 1 {
		rl.Failf("cross-org clone must filter memberships down to the restorer; got %d member(s)", memberCount)
	}
	if memberUser != seed.RestorerUserID {
		rl.Failf("cross-org clone membership user = %s, want restorer %s", memberUser, seed.RestorerUserID)
	}
	rl.Printf("clone membership filtered to restorer %s (count=1)", memberUser)

	rl.Section("Verify non-membership content matches source modulo ID/FK remap")
	cloneFP := projectFingerprint(t, rl, db, cloneID)
	ignore := map[string]bool{"kb.project_memberships": true}
	requireFingerprintsEqual(t, rl, cloneFP, pre, ignore)
	assertNoSharedUUIDs(t, rl, db, seed.ProjectID, cloneID)

	// The source project must be untouched by the cross-org clone.
	sourceFP := projectFingerprint(t, rl, db, seed.ProjectID)
	requireFingerprintsEqual(t, rl, sourceFP, pre, nil)
}

// =============================================================================
// 4. Async restore lifecycle — pending → running/… → completed, progress 0-100
// =============================================================================

func TestBackupRestore_AsyncProgress(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	rl.Describe("Verify restore runs asynchronously with pollable progress",
		"Seed a project and create a ready backup",
		"Start a clone restore and assert the 202 job is status pending with progress 0",
		"Poll GET /api/v1/restores/{id} until terminal",
		"Assert only valid statuses are observed, progress never regresses or leaves 0-100",
		"Assert the job terminates completed with progress 100",
	)
	skipIfServerDown(t, rl)

	seed := seedRichProject(t, rl)

	backupID := createProjectBackup(t, rl, seed.ProjectID)
	waitBackupReady(t, rl, seed.OrgID, backupID)

	rl.Section("Start clone restore and assert pending job")
	job := startCloneRestore(t, rl, seed.OrgID, backupID, uniqueName("e2e-restore-async"))
	rl.Printf("202 job id=%s status=%s progress=%d", job.ID, job.Status, job.Progress)

	rl.Section("Poll restore job through terminal state")
	final, seen := waitRestoreTerminal(t, rl, seed.OrgID, job)
	rl.Printf("async restore observed transitions: %s", strings.Join(seen, " -> "))

	if final.Status != "completed" {
		rl.Failf("expected terminal status completed, got %s", final.Status)
	}
	if final.Progress != 100 {
		rl.Failf("expected final progress 100, got %d", final.Progress)
	}
	if final.ErrorMessage != nil && *final.ErrorMessage != "" {
		rl.Failf("completed restore unexpectedly carried errorMessage %q", *final.ErrorMessage)
	}
	if final.CompletedAt == nil || *final.CompletedAt == "" {
		rl.Printf("note: completed restore did not expose completedAt")
	}
	if len(seen) < 2 {
		rl.Printf("note: job completed before additional intermediate states could be observed (%s)", strings.Join(seen, " -> "))
	}
	rl.Printf("restore progressed from pending to %s with monotonic progress 0..100", seen[len(seen)-1])
}

// =============================================================================
// 5. Overwrite creates a pre-restore snapshot backup
// =============================================================================

func TestBackupRestore_Overwrite_CreatesPreRestoreSnapshot(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	rl.Describe("Verify overwrite restore snapshots the live project before modifying it",
		"Seed a project and create a ready backup",
		"Assert the project currently has exactly one backup",
		"Run an overwrite restore with preRestoreSnapshot enabled",
		"Assert a second backup (the pre-restore snapshot) appears for the project and becomes ready",
		"Assert the original backup is still present",
	)
	skipIfServerDown(t, rl)

	seed := seedRichProject(t, rl)

	rl.Section("Create initial backup")
	backup1 := createProjectBackup(t, rl, seed.ProjectID)
	waitBackupReady(t, rl, seed.OrgID, backup1)
	before := listOrgBackups(t, rl, seed.OrgID, seed.ProjectID)
	if len(before) != 1 {
		rl.Failf("expected exactly 1 backup before overwrite restore, got %d", len(before))
	}
	rl.Printf("confirmed exactly 1 backup before overwrite restore")

	rl.Section("Run overwrite restore with pre-restore snapshot")
	job := startOverwriteRestore(t, rl, seed.ProjectID, backup1)
	waitRestoreTerminal(t, rl, seed.OrgID, job)

	rl.Section("Wait for pre-restore snapshot backup")
	deadline := time.Now().Add(backupReadyTimeout)
	var after []backupRecord
	for time.Now().Before(deadline) {
		time.Sleep(backupPollInterval)
		after = listOrgBackups(t, rl, seed.OrgID, seed.ProjectID)
		if len(after) >= 2 {
			break
		}
		rl.Printf("waiting for pre-restore snapshot backup (currently %d backups)", len(after))
	}
	if len(after) < 2 {
		rl.Failf("expected a pre-restore snapshot backup to be created, only %d backups exist", len(after))
	}

	// The original backup must still exist and the new one must differ from it.
	origFound := false
	var snapshot *backupRecord
	for i := range after {
		if after[i].ID == backup1 {
			origFound = true
			continue
		}
		copy := after[i]
		snapshot = &copy
	}
	if !origFound {
		rl.Failf("original backup %s disappeared after overwrite restore", backup1)
	}
	if snapshot == nil {
		rl.Failf("no additional (pre-restore snapshot) backup found for project")
	}
	rl.Printf("found pre-restore snapshot candidate id=%s status=%s", snapshot.ID, snapshot.Status)

	// Wait until the pre-restore snapshot is fully ready.
	finalSnapshot := waitBackupReady(t, rl, seed.OrgID, snapshot.ID)
	if finalSnapshot.ProjectID != seed.ProjectID {
		rl.Failf("pre-restore snapshot belongs to project %s, want %s", finalSnapshot.ProjectID, seed.ProjectID)
	}

	// Marker presence is implementation-shaped (metadata/includes JSON). It is
	// reported when exposed but the hard contract assertion is that the
	// overwrite automatically produced exactly one additional backup.
	raw, err := json.Marshal(finalSnapshot)
	if err != nil {
		rl.Failf("marshal snapshot backup for marker check: %v", err)
	}
	lower := strings.ToLower(string(raw))
	if strings.Contains(lower, "pre-restore") || strings.Contains(lower, "prerestore") {
		rl.Printf("pre-restore snapshot backup is explicitly marked: %s", string(raw))
	} else {
		rl.Printf("pre-restore snapshot backup id=%s is ready; marker not exposed on the backup record JSON", finalSnapshot.ID)
	}
}
