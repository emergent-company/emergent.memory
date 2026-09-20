package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	framework "github.com/emergent-company/runlog"
)

func TestReExtractionFriends5Episodes(t *testing.T) {
	rl := framework.NewRunLog(t)
	defer rl.Close()

	token := framework.E2ETestToken()
	projectID := ""
	var snaps []extractionSnapshot

	rl.Describe("5-episode progressive extraction with ground-truth verification",
		"Extracts entities from 5 Friends episodes via remember API",
		"Verifies progressive object growth and canonical ID stability",
		"Checks no dangling relationships or relationship-as-object types")

	// Setup — create project (org pre-exists in standalone mode)
	rl.Section("Setup: create project + configure provider")
	orgID := framework.OrgID()
	if orgID == "" {
		orgID = "2c0c2fbd-d81a-4c69-9143-c64590d66665"
	}
	projectID = createProject(t, rl, orgID, "friends-rext-5ep")
	if projectID == "" {
		t.Fatal("failed to create project")
	}
	rl.Printf("  project_id=%s", projectID)
	configureProjectModel(t, rl, projectID)

	episodes := []struct {
		Label string
		Doc   string
	}{
		{"S01E01 — Pilot", friendsPilotDoc},
		{"S01E02 — Sonogram", friendsE02Doc},
		{"S01E03 — Thumb", friendsE03Doc},
		{"S01E04 — Stephanopoulos", friendsE04Doc},
		{"S01E05 — Laundry", friendsE05Doc},
	}

	for i, ep := range episodes {
		rl.Section(fmt.Sprintf("Episode %d/5: %s", i+1, ep.Label))

		rl.Printf("Uploading document...")
		docID := uploadDoc(t, rl, token, projectID, "friends_"+ep.Label+".txt", ep.Doc)
		rl.Printf("  doc_id=%s", docID)
		url := fmt.Sprintf("%s/api/projects/%s/remember", framework.ServerURL(), projectID)
		body := fmt.Sprintf(`{"message":%s,"schema_policy":"auto","mode":"sync"}`, jsonString(ep.Doc))
		// Use a longer timeout for the remember call (LLM can take 60s+)
		resp := doRequestLong(t, "POST", url, token, projectID, []byte(body), 120*time.Second)
		if resp != nil {
			var result map[string]any
			json.NewDecoder(resp.Body).Decode(&result)
			resp.Body.Close()
			rl.Printf("  remember done: status=%v", result["status"])
		}

		rl.Printf("Waiting for extraction to complete...")
		waitForExtraction(t, rl, projectID, 180*time.Second)

		snap := snapshotExtraction(t, rl, projectID)
		snaps = append(snaps, snap)
		printExtractionSummary(t, rl, ep.Label, snap)
		verifyExtraction(t, rl, projectID, ep.Label, epEntities[i])
	}

	// Progressive diffs
	for i := 1; i < len(snaps); i++ {
		rl.Printf("")
		rl.Printf("──────────────────────────────────────────────────────────────────────")
		progressDiff(t, rl, snaps[i-1], snaps[i], episodes[i-1].Label, episodes[i].Label)
	}

	// Integrity checks
	rl.Section("Post-extraction integrity checks")

	rl.Printf("Checking for relationship-as-object types...")
	typeCounts := getTypeCounts(t, rl, projectID)
	if n := typeCounts["Relationship"] + typeCounts["CharacterRelationship"]; n > 0 {
		rl.Printf("  ❌ found %d relationship-as-object objects", n)
	} else {
		rl.Printf("  ✅ no relationship-as-object types found")
	}

	rl.Printf("Checking for dangling relationships...")
	assertNoDanglingRelationships(t, rl, projectID)

	// Core cast
	rl.Section(fmt.Sprintf("Core cast verification (%d main characters)", len(coreCastNames)))
	allFound := true
	for _, name := range coreCastNames {
		e := groundTruthEntity{Name: name, TypeHints: []string{"Character", "Person"}}
		if assertEntityExists(t, rl, projectID, e, 10*time.Second) == nil {
			allFound = false
		}
	}
	if !allFound {
		t.Error("core cast extraction failed — all 6 main characters must exist")
	}

	if len(snaps) >= 5 {
		rl.Printf("")
		rl.Printf("╔══════════════════════════════════════════════════════════════════════╗")
		rl.Printf("║   CUMULATIVE: %-3d → %-3d → %-3d → %-3d → %-3d objects across 5 episodes        ║",
			snaps[0].TotalObjects(), snaps[1].TotalObjects(), snaps[2].TotalObjects(),
			snaps[3].TotalObjects(), snaps[4].TotalObjects())
		rl.Printf("╚══════════════════════════════════════════════════════════════════════╝")
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func createProject(t *testing.T, rl *framework.RunLog, orgID, name string) string {
	t.Helper()
	url := fmt.Sprintf("%s/api/projects", framework.ServerURL())
	body := fmt.Sprintf(`{"name":%q,"orgId":%q}`, name+"-"+fmt.Sprint(time.Now().UnixMilli()), orgID)
	req, err := http.NewRequest("POST", url, bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatalf("createProject: build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	framework.SetAuthHeader(req, framework.E2ETestToken())
	req.Header.Set("X-Org-ID", orgID)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("createProject: do request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := json.Marshal(map[string]any{"status": resp.StatusCode})
		rl.Printf("  createProject response: %s", string(bodyBytes))
		t.Fatalf("createProject: unexpected status %d", resp.StatusCode)
	}
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err == nil {
		if id, ok := result["id"].(string); ok {
			rl.Printf("  project_id=%s", id)
			return id
		}
	}
	t.Fatalf("createProject: failed to parse response: %s", name)
	return ""
}

func uploadDoc(t *testing.T, rl *framework.RunLog, token, projectID, filename, content string) string {
	t.Helper()
	url := fmt.Sprintf("%s/api/documents", framework.ServerURL())
	body := fmt.Sprintf(`{"filename":%q,"content":%s}`, filename, jsonString(content))
	resp := doRequest(t, "POST", url, token, projectID, []byte(body))
	if resp != nil {
		defer resp.Body.Close()
		var result map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&result); err == nil {
			if id, ok := result["id"].(string); ok {
				return id
			}
			rl.Printf("  upload response: %v", result)
		}
	}
	return ""
}

func configureProjectModel(t *testing.T, rl *framework.RunLog, projectID string) {
	// Set up OpenAI provider via LiteLLM proxy
	rl.Printf("  configuring openai provider with model openai/deepseek-v4-flash")
	home := t.TempDir()
	framework.SetupCLIAuth(t, home)

	_, err := framework.RunCLIInDirWithHome(t, ".", home,
		"--server", framework.ServerURL(),
		"--project-token", framework.E2ETestToken(),
		"provider", "configure-project", "openai",
		"--api-key", "sk--hdGtQNlFMQDO3aW2d-bPw",
		"--generative-model", "openai/deepseek-v4-flash",
		"--base-url", "http://litellm:4000/v1",
		"--project", projectID)
	if err != nil {
		rl.Printf("  ⚠ provider config failed: %v (tests will fail)", err)
		return
	}
	rl.Printf("  provider configured")

	// Also set the model config so the model resolver can find it
	rl.Printf("  setting model config")
	err = setModelConfig(t, projectID, "openai/deepseek-v4-flash")
	if err != nil {
		rl.Printf("  ⚠ model config failed: %v (tests will fail)", err)
		return
	}
	rl.Printf("  model config set")
}

func setModelConfig(t *testing.T, projectID, modelName string) error {
	url := fmt.Sprintf("%s/api/v1/projects/%s/model-config", framework.ServerURL(), projectID)
	body := fmt.Sprintf(`{"generativeModel":%q}`, modelName)
	resp := doRequest(t, "PUT", url, framework.E2ETestToken(), projectID, []byte(body))
	if resp != nil {
		resp.Body.Close()
		if resp.StatusCode == 200 {
			return nil
		}
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return fmt.Errorf("no response")
}

func waitForExtraction(t *testing.T, rl *framework.RunLog, projectID string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		out, err := framework.RunCLIInDirWithHome(t, ".", "", "graph", "objects", "list", "--project", projectID, "--json")
		if err == nil && out != "" {
			var resp struct {
				Items []any `json:"items"`
			}
			if json.Unmarshal([]byte(out), &resp) == nil && len(resp.Items) > 0 {
				rl.Printf("  extraction done: %d objects", len(resp.Items))
				return
			}
		}
		rl.Printf("  waiting for extraction...")
		time.Sleep(3 * time.Second)
	}
	rl.Printf("  ⚠ timeout after %v", timeout)
}

func getTypeCounts(t *testing.T, rl *framework.RunLog, projectID string) map[string]int {
	counts := make(map[string]int)
	token := framework.E2ETestToken()
	for _, typeName := range []string{"Character", "Person", "Location", "Event", "Episode", "Relationship", "CharacterRelationship"} {
		url := fmt.Sprintf("%s/api/graph/objects?project_id=%s&type=%s&limit=1", framework.ServerURL(), projectID, typeName)
		resp := doRequest(t, "GET", url, token, projectID, nil)
		if resp != nil && resp.StatusCode == 200 {
			var data struct {
				Total int `json:"total"`
			}
			json.NewDecoder(resp.Body).Decode(&data)
			resp.Body.Close()
			counts[typeName] = data.Total
		} else if resp != nil {
			resp.Body.Close()
		}
	}
	return counts
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func doRequest(t *testing.T, method, url, token, projectID string, body []byte) *http.Response {
	return framework.DoJSON(t, method, url, token, projectID, body)
}

func doRequestLong(t *testing.T, method, url, token, projectID string, body []byte, timeout time.Duration) *http.Response {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request %s %s: %v", method, url, err)
	}
	req.Header.Set("Content-Type", "application/json")
	framework.SetAuthHeader(req, token)
	if projectID != "" {
		req.Header.Set("X-Project-ID", projectID)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request %s %s: %v", method, url, err)
	}
	return resp
}
