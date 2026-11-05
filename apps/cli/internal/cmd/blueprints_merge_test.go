package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// mergeCmdServer is a fake API for the CLI merge-conflict tests. It records
// pack writes and update-apply bodies so tests can assert zero mutations and
// merge-flag propagation.
type mergeCmdServer struct {
	mu           sync.Mutex
	conflicts    []map[string]any
	previewCalls int
	packCreates  int
	assigns      []map[string]any
	applyBodies  [][]byte
}

func newMergeCmdServer(t *testing.T, conflicts bool) (*httptest.Server, *mergeCmdServer) {
	t.Helper()
	s := &mergeCmdServer{}
	if conflicts {
		s.conflicts = []map[string]any{{
			"typeName":              "Widget",
			"incomingPackName":      "incoming",
			"existingSchemaName":    "existing",
			"existingSchemaVersion": "1.0.0",
			"addedProperties":       []string{"email"},
			"conflictingProperties": []map[string]any{{
				"property":     "name",
				"existing_def": map[string]any{"type": "string"},
				"incoming_def": map[string]any{"type": "number"},
				"resolution":   "existing_wins",
			}},
		}}
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/available"):
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/installed"):
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/compiled-types"):
			_, _ = w.Write([]byte(`{"objectTypes":[],"relationshipTypes":[]}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/merge-preview"):
			s.mu.Lock()
			s.previewCalls++
			s.mu.Unlock()
			body, _ := json.Marshal(map[string]any{
				"conflicts":     s.conflicts,
				"requiresMerge": len(s.conflicts) > 0,
			})
			_, _ = w.Write(body)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/updates/preview"):
			_, _ = w.Write([]byte(`{"blueprintName":"crm","currentVersion":"1.0.0","latestVersion":"2.0.0","requiresMerge":true,"conflicts":[{"typeName":"Widget","incomingPackName":"incoming","existingSchemaName":"existing","existingSchemaVersion":"1.0.0","addedProperties":["email"],"conflictingProperties":[{"property":"name","existing_def":{},"incoming_def":{},"resolution":"existing_wins"}]}]}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/updates/apply"):
			body, _ := io.ReadAll(r.Body)
			s.mu.Lock()
			s.applyBodies = append(s.applyBodies, body)
			s.mu.Unlock()
			_, _ = w.Write([]byte(`{"id":"job-1","projectId":"proj-1","blueprintName":"crm","action":"update","status":"pending"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/schemas":
			s.mu.Lock()
			s.packCreates++
			s.mu.Unlock()
			_, _ = w.Write([]byte(`{"id":"pack-1","name":"incoming","version":"1.0.0"}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/assign"):
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			s.mu.Lock()
			s.assigns = append(s.assigns, body)
			s.mu.Unlock()
			_, _ = w.Write([]byte(`{"schema_id":"pack-1","installed_types":[]}`))
		default:
			// Model-mapping/read helpers are advisory; return an empty success.
			_, _ = w.Write([]byte(`{"success":true,"data":[],"modelOverrides":{}}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, s
}

func isolateMergeCLIState(t *testing.T, serverURL string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MEMORY_SERVER_URL", serverURL)
	t.Setenv("MEMORY_PROJECT_API_KEY", "emt_test_project_token")
	t.Setenv("CI", "1")

	prevServer := viper.GetString("server")
	viper.Set("server", serverURL)
	t.Cleanup(func() { viper.Set("server", prevServer) })

	prevProject := blueprintsProjectFlag
	prevDryRun := blueprintsDryRunFlag
	prevUpgrade := blueprintsUpgradeFlag
	prevMerge := blueprintsMergeTypesFlag
	prevWait := blueprintsUpdateWaitFlag
	t.Cleanup(func() {
		blueprintsProjectFlag = prevProject
		blueprintsDryRunFlag = prevDryRun
		blueprintsUpgradeFlag = prevUpgrade
		blueprintsMergeTypesFlag = prevMerge
		blueprintsUpdateWaitFlag = prevWait
	})
}

func runInstallMerge(t *testing.T, serverURL, source string, mergeFlag, dryRun bool) (string, error) {
	t.Helper()
	isolateMergeCLIState(t, serverURL)
	blueprintsProjectFlag = "00000000-0000-0000-0000-000000000001"
	blueprintsMergeTypesFlag = mergeFlag
	blueprintsDryRunFlag = dryRun
	blueprintsUpgradeFlag = false

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.Flags().String("config", "", "config file")
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	err := runBlueprintsInstall(cmd, []string{source})
	return out.String(), err
}

func runUpdateMerge(t *testing.T, serverURL string, mergeFlag bool) (string, error) {
	t.Helper()
	isolateMergeCLIState(t, serverURL)
	blueprintsProjectFlag = "00000000-0000-0000-0000-000000000001"
	blueprintsMergeTypesFlag = mergeFlag
	blueprintsUpdateWaitFlag = false

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.Flags().String("config", "", "config file")
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	err := runBlueprintsUpdate(cmd, []string{"crm"})
	return out.String(), err
}

func TestRunBlueprintsInstall_MergeConflictAbortsNonInteractive(t *testing.T) {
	srv, fake := newMergeCmdServer(t, true)
	dir := writePackBlueprintDir(t, "name: incoming\nversion: 1.0.0\nobjectTypes:\n  - name: Widget\n    properties:\n      name:\n        type: string\n")

	out, err := runInstallMerge(t, srv.URL, dir, false, false)
	if err == nil {
		t.Fatalf("expected non-interactive conflict abort, got nil (output: %s)", out)
	}
	if !strings.Contains(err.Error(), "--merge-types") || !strings.Contains(err.Error(), "Widget") {
		t.Fatalf("error must name the conflict and --merge-types, got: %v", err)
	}
	if !strings.Contains(out, "existing definition wins") {
		t.Fatalf("conflict details must be printed, got:\n%s", out)
	}
	if fake.packCreates != 0 || len(fake.assigns) != 0 {
		t.Fatalf("conflict abort must not mutate: creates=%d assigns=%d", fake.packCreates, len(fake.assigns))
	}
}

func TestRunBlueprintsInstall_MergeFlagAssignsWithMerge(t *testing.T) {
	srv, fake := newMergeCmdServer(t, true)
	dir := writePackBlueprintDir(t, "name: incoming\nversion: 1.0.0\nobjectTypes:\n  - name: Widget\n    properties:\n      name:\n        type: string\n")

	out, err := runInstallMerge(t, srv.URL, dir, true, false)
	if err != nil {
		t.Fatalf("install with --merge-types failed: %v\noutput:\n%s", err, out)
	}
	if fake.packCreates != 1 {
		t.Fatalf("expected one pack create, got %d", fake.packCreates)
	}
	if len(fake.assigns) != 1 {
		t.Fatalf("expected one assign, got %d", len(fake.assigns))
	}
	if fake.assigns[0]["merge"] != true {
		t.Fatalf("expected merge=true on assign, got %+v", fake.assigns[0])
	}
}

func TestRunBlueprintsInstall_MergeConflictDryRunMakesNoMutation(t *testing.T) {
	srv, fake := newMergeCmdServer(t, true)
	dir := writePackBlueprintDir(t, "name: incoming\nversion: 1.0.0\nobjectTypes:\n  - name: Widget\n")

	out, err := runInstallMerge(t, srv.URL, dir, false, true)
	if err != nil {
		t.Fatalf("dry-run should stay offline and succeed: %v\noutput:\n%s", err, out)
	}
	if fake.previewCalls != 0 {
		t.Fatalf("dry-run must not call merge-preview, got %d call(s)", fake.previewCalls)
	}
	if strings.Contains(out, "existing definition wins") {
		t.Fatalf("dry-run must not fetch/print server conflict details, got:\n%s", out)
	}
	if fake.packCreates != 0 || len(fake.assigns) != 0 {
		t.Fatalf("dry-run must not mutate: creates=%d assigns=%d", fake.packCreates, len(fake.assigns))
	}
}

func TestRunBlueprintsInstall_NoConflictDoesNotSetMerge(t *testing.T) {
	srv, fake := newMergeCmdServer(t, false)
	dir := writePackBlueprintDir(t, "name: incoming\nversion: 1.0.0\nobjectTypes:\n  - name: Widget\n")

	if _, err := runInstallMerge(t, srv.URL, dir, false, false); err != nil {
		t.Fatalf("install failed: %v", err)
	}
	if len(fake.assigns) != 1 {
		t.Fatalf("expected one assign, got %d", len(fake.assigns))
	}
	if _, ok := fake.assigns[0]["merge"]; ok {
		t.Fatalf("merge must be omitted without conflicts/flag, got %+v", fake.assigns[0])
	}
}

func TestRunBlueprintsUpdate_MergeConflictAbortsNonInteractive(t *testing.T) {
	srv, fake := newMergeCmdServer(t, true)
	out, err := runUpdateMerge(t, srv.URL, false)
	if err == nil {
		t.Fatalf("expected update conflict abort, got nil (output: %s)", out)
	}
	if !strings.Contains(err.Error(), "--merge-types") || !strings.Contains(err.Error(), "Widget") {
		t.Fatalf("error must name the conflict and --merge-types, got: %v", err)
	}
	if len(fake.applyBodies) != 0 {
		t.Fatalf("update must not be enqueued before confirmation, got %d apply calls", len(fake.applyBodies))
	}
}

func TestRunBlueprintsUpdate_MergeFlagSendsBody(t *testing.T) {
	srv, fake := newMergeCmdServer(t, true)
	if _, err := runUpdateMerge(t, srv.URL, true); err != nil {
		t.Fatalf("update with --merge-types failed: %v", err)
	}
	if len(fake.applyBodies) != 1 {
		t.Fatalf("expected one update apply call, got %d", len(fake.applyBodies))
	}
	var body map[string]any
	if err := json.Unmarshal(fake.applyBodies[0], &body); err != nil {
		t.Fatalf("decode apply body: %v", err)
	}
	if body["mergeTypeConflicts"] != true {
		t.Fatalf("expected mergeTypeConflicts=true, got %+v", body)
	}
}

func TestPromptMergeConfirmation(t *testing.T) {
	t.Run("yes", func(t *testing.T) {
		var out bytes.Buffer
		cmd := &cobra.Command{}
		cmd.SetIn(strings.NewReader("y\n"))
		cmd.SetOut(&out)
		if err := promptMergeConfirmation(cmd); err != nil {
			t.Fatalf("expected yes to confirm: %v", err)
		}
		if !strings.Contains(out.String(), "Existing object type names will be merged. Continue? [y/N]") {
			t.Fatalf("unexpected prompt: %s", out.String())
		}
	})

	t.Run("no", func(t *testing.T) {
		cmd := &cobra.Command{}
		cmd.SetIn(strings.NewReader("n\n"))
		cmd.SetOut(&bytes.Buffer{})
		if err := promptMergeConfirmation(cmd); err == nil {
			t.Fatal("expected no to abort")
		}
	})

	t.Run("EOF", func(t *testing.T) {
		cmd := &cobra.Command{}
		cmd.SetIn(strings.NewReader(""))
		cmd.SetOut(&bytes.Buffer{})
		if err := promptMergeConfirmation(cmd); err == nil {
			t.Fatal("expected EOF to abort")
		}
	})
}
