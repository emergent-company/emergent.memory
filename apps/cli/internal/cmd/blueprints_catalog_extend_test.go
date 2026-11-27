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

// catalogInstallRequest is what the fake server saw, so a test can prove the
// flag reaches the body rather than being dropped on the way.
type catalogInstallServer struct {
	mu   sync.Mutex
	body map[string]any

	response string
	status   int
}

func newCatalogInstallServer(t *testing.T) (*httptest.Server, *catalogInstallServer) {
	t.Helper()
	s := &catalogInstallServer{status: http.StatusOK}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/api/blueprints/catalog/install") {
			raw, _ := io.ReadAll(r.Body)
			s.mu.Lock()
			s.body = map[string]any{}
			_ = json.Unmarshal(raw, &s.body)
			status, body := s.status, s.response
			s.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	return srv, s
}

// isolateCatalogCLIState pins the globals runBlueprintsCatalogInstall reads and
// points the client at the fake server.
func isolateCatalogCLIState(t *testing.T, serverURL string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MEMORY_SERVER_URL", serverURL)
	t.Setenv("MEMORY_PROJECT_API_KEY", "emt_test_project_token")
	t.Setenv("CI", "1")

	prevServer := viper.GetString("server")
	viper.Set("server", serverURL)
	t.Cleanup(func() { viper.Set("server", prevServer) })

	prevProject := blueprintsProjectFlag
	prevTypes := blueprintsCatalogTypesFlag
	prevExtend := blueprintsCatalogExtendFlag
	prevName := blueprintsCatalogNameFlag
	prevVersion := blueprintsCatalogVersionFlag
	prevReplace := blueprintsCatalogReplaceFlag
	t.Cleanup(func() {
		blueprintsProjectFlag = prevProject
		blueprintsCatalogTypesFlag = prevTypes
		blueprintsCatalogExtendFlag = prevExtend
		blueprintsCatalogNameFlag = prevName
		blueprintsCatalogVersionFlag = prevVersion
		blueprintsCatalogReplaceFlag = prevReplace
	})

	blueprintsProjectFlag = "00000000-0000-0000-0000-000000000001"
	blueprintsCatalogTypesFlag = nil
	blueprintsCatalogExtendFlag = nil
	blueprintsCatalogNameFlag = ""
	blueprintsCatalogVersionFlag = ""
	blueprintsCatalogReplaceFlag = false
}

func runCatalogInstall(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.Flags().String("config", "", "config file")
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	err := runBlueprintsCatalogInstall(cmd, args)
	return out.String(), err
}

// TestCatalogInstallSendsExtendAndReportsMerged pins the CLI half of the
// cross-pack merge contract: the server refuses a type another pack provides
// unless the caller names it in `extend`, so the command must be able to send
// that field and to report what merged.
func TestCatalogInstallSendsExtendAndReportsMerged(t *testing.T) {
	srv, fake := newCatalogInstallServer(t)
	fake.response = `{"packId":"p1","created":false,"types":["Organization"],"merged":["Organization"]}`
	isolateCatalogCLIState(t, srv.URL)
	blueprintsCatalogExtendFlag = []string{"Organization"}

	out, err := runCatalogInstall(t, "Organization")
	if err != nil {
		t.Fatalf("catalog-install: %v", err)
	}
	if got := fake.body["extend"]; got == nil {
		t.Fatalf("request body has no extend: %#v", fake.body)
	} else if names, ok := got.([]any); !ok || len(names) != 1 || names[0] != "Organization" {
		t.Errorf("extend = %#v, want [Organization]", got)
	}
	if !strings.Contains(out, "Merged into existing types") || !strings.Contains(out, "Organization") {
		t.Errorf("output does not report the merge: %q", out)
	}
}

// TestCatalogInstallOmitsExtendWhenUnset keeps the read-only default: a plain
// install sends no extend, so a cross-pack collision is a server-side refusal
// rather than a merge the user did not ask for.
func TestCatalogInstallOmitsExtendWhenUnset(t *testing.T) {
	srv, fake := newCatalogInstallServer(t)
	fake.response = `{"packId":"p1","created":true,"types":["Movie"]}`
	isolateCatalogCLIState(t, srv.URL)

	if _, err := runCatalogInstall(t, "Movie"); err != nil {
		t.Fatalf("catalog-install: %v", err)
	}
	if _, ok := fake.body["extend"]; ok {
		t.Errorf("body carries extend without the flag: %#v", fake.body)
	}
}
