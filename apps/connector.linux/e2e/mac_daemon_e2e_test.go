package e2e

import (
	"runtime"
	"testing"
	"time"
)

// requireDarwin skips the test under -short or on non-darwin hosts. This test
// asserts the Apple tool set the engine registers when built GOOS=darwin, so it
// is only meaningful on macOS. It mirrors requireRunnable's style and does not
// weaken the existing Linux test.
func requireDarwin(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping connector e2e in -short mode")
	}
	if runtime.GOOS != "darwin" {
		t.Skipf("darwin engine e2e asserts the Apple tool set (host is %s)", runtime.GOOS)
	}
}

// darwinAppleTools is the set of Apple tool ids the engine registers on darwin.
// Sourced from internal/appletools.Provider (appletools/provider.go), which
// returns exactly this list when osascript is available, and cross-checked
// against the app's ToolCatalog.swift (whose ids "must match the connector").
// Registration does not execute the tool, so asserting these is permission-free
// even though the tools themselves need TCC/EventKit/AppleScript grants to run.
var darwinAppleTools = []string{
	"notes_search",
	"notes_create",
	"reminders_list",
	"reminders_lists",
	"reminders_add",
	"reminders_update",
	"reminders_delete",
}

// TestDarwinDaemonRegisterAndStatus builds the real darwin memory-connector
// binary (reusing TestMain/buildConnectorBinary) and runs it against the
// in-process fake relay hub. It asserts the wire handshake and registration:
// a register frame carrying our instance id whose tool list contains the Apple
// tool ids, and `status --json` reporting connected.
//
// The tool-*call* round-trip stays Linux-only: the permission-free probe tool
// (linux-host-info) has no darwin analogue, and invoking the Apple tools would
// require live TCC/EventKit/AppleScript grants that a hermetic test must not
// assume.
func TestDarwinDaemonRegisterAndStatus(t *testing.T) {
	requireDarwin(t)

	hub := newFakeHub(t)
	instanceID := uniqueInstanceID("e2e-darwin")
	cfgPath := writeConfig(t, hub.url(), instanceID, "e2e-token")

	startDaemon(t, cfgPath)

	// 1. Register: first frame carries our instance id and the Apple tool list.
	regs := hub.waitRegisters(1, 15*time.Second)
	reg := regs[0]
	if reg.InstanceID != instanceID {
		t.Fatalf("register instance_id = %q, want %q", reg.InstanceID, instanceID)
	}
	if reg.Version != "0.1.0" {
		t.Errorf("register version = %q, want 0.1.0", reg.Version)
	}
	names := toolNames(reg)
	for _, id := range darwinAppleTools {
		if !containsString(names, id) {
			t.Errorf("register tools = %v, want it to contain %q", names, id)
		}
	}
	if got := hub.authHeader(); got != "Bearer e2e-token" {
		t.Errorf("handshake Authorization = %q, want Bearer e2e-token", got)
	}

	// 2. Status: while the daemon runs, `status --json` reports connected and
	//    the same Apple tool set.
	doc := runStatusJSON(t, cfgPath)
	if doc.SchemaVersion != 1 {
		t.Errorf("status schema_version = %d, want 1", doc.SchemaVersion)
	}
	if doc.InstanceID != instanceID {
		t.Errorf("status instance_id = %q, want %q", doc.InstanceID, instanceID)
	}
	for _, id := range darwinAppleTools {
		if !containsString(doc.Tools, id) {
			t.Errorf("status tools = %v, want it to contain %q", doc.Tools, id)
		}
	}
	if doc.HubState != "connected" {
		t.Errorf("status hub_state = %q, want connected", doc.HubState)
	}
}
