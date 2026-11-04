// Package cli_test — graph_write_attribution_test.go
//
// Runlog test for the write-stamp filters on the CLI (#2468): create a project
// token, write an object with it, then ask the CLI "what did this credential
// write" and "what did this run write". The run never exists outside an agent
// run, so the run filter is exercised as an unmatched-but-valid page.
package cli_test

import (
	"encoding/json"
	"strings"
	"testing"
)

type stampListResponse struct {
	Items []struct {
		ID             string  `json:"id"`
		EntityID       string  `json:"entity_id"`
		CredentialKind string  `json:"credential_kind"`
		CredentialID   *string `json:"credential_id"`
		DataClass      string  `json:"data_class"`
	} `json:"items"`
	Total int `json:"total"`
}

// TestCLIInstalled_GraphWriteAttributionFilters exercises the stamp filters end
// to end through the CLI: a write made with a known project token must be
// findable by that credential, and an unmatched run must be an empty page.
func TestCLIInstalled_GraphWriteAttributionFilters(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify graph write-stamp filters from the CLI",
		"Create a project and a project API token",
		"Write a graph object with that token so the row records credential_kind=api_key",
		"List by --credential-id and find exactly that object",
		"List by an unmatched --run-id and get an empty page, not an error",
		"Show the stamp in graph objects get",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project")
	srv := serverURL()
	name := uniqueProjectName("e2e-graph-stamp")
	projectID := createProject(t, home, srv, name)
	deleteProjectOnCleanup(t, home, projectID)
	rl.Printf("project: %s (%s)", name, projectID)

	rl.Section("Create a project token")
	tokenName := "e2e-stamp-token"
	createTokenOut := mustRunCLIInDirWithHome(t, "", home,
		"projects", "create-token", projectID,
		"--name", tokenName,
	)
	rl.CLI("memory projects create-token "+projectID+" --name "+tokenName, createTokenOut)

	var token string
	for _, line := range strings.Split(createTokenOut, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "Token created: "); ok {
			token = strings.TrimSpace(rest)
		}
	}
	if token == "" {
		t.Fatalf("could not parse the project token from: %s", truncate(createTokenOut, 300))
	}

	// The credential id is what the write stamp records; read it off the token
	// listing (compact mode prints "<name>  <id>").
	tokensOut := mustRunCLIInDirWithHome(t, "", home,
		"tokens", "list", "--project", projectID, "--compact",
	)
	rl.CLI("memory tokens list --project "+projectID+" --compact", tokensOut)

	var credentialID string
	for _, line := range strings.Split(tokensOut, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == tokenName {
			credentialID = fields[1]
			break
		}
	}
	if credentialID == "" {
		t.Fatalf("could not find token %q in the token listing: %s", tokenName, truncate(tokensOut, 300))
	}
	rl.Printf("credential id: %s", credentialID)

	rl.Section("Write an object with that token")
	createOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "create",
		"--type", "StampCliObject",
		"--name", "WrittenByProjectToken",
		"--project", projectID,
		"--project-token", token,
		"--output", "json",
	)
	rl.CLI("memory graph objects create --type StampCliObject --project "+projectID+" --project-token <token>", createOut)

	objectID := parseJSONField(createOut, "id")
	if objectID == "" {
		t.Fatalf("could not parse object ID from create output: %s", truncate(createOut, 300))
	}
	if kind := parseJSONField(createOut, "credential_kind"); kind != "api_key" {
		t.Errorf("expected the created object to disclose credential_kind=api_key, got %q", kind)
	}

	rl.Section("List by credential id")
	listOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "list",
		"--type", "StampCliObject",
		"--credential-id", credentialID,
		"--project", projectID,
		"--output", "json",
	)
	rl.CLI("memory graph objects list --credential-id "+credentialID+" --project "+projectID+" --output json", listOut)

	var listed stampListResponse
	if err := json.Unmarshal([]byte(listOut), &listed); err != nil {
		t.Fatalf("list output is not JSON: %v\n%s", err, truncate(listOut, 300))
	}
	if len(listed.Items) != 1 {
		t.Fatalf("expected exactly 1 object for --credential-id %s, got %d", credentialID, len(listed.Items))
	}
	if listed.Items[0].EntityID != objectID && listed.Items[0].ID != objectID {
		t.Errorf("expected the listed object to be %s, got %+v", objectID, listed.Items[0])
	}
	if listed.Items[0].CredentialKind != "api_key" {
		t.Errorf("expected the listed row to disclose credential_kind=api_key, got %q", listed.Items[0].CredentialKind)
	}
	if listed.Items[0].DataClass != "synthesized" {
		t.Errorf("expected data_class=synthesized, got %q", listed.Items[0].DataClass)
	}

	rl.Section("List by credential kind")
	kindOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "list",
		"--type", "StampCliObject",
		"--credential-kind", "api_key",
		"--project", projectID,
		"--output", "json",
	)
	rl.CLI("memory graph objects list --credential-kind api_key --project "+projectID, kindOut)
	var byKind stampListResponse
	if err := json.Unmarshal([]byte(kindOut), &byKind); err != nil {
		t.Fatalf("credential-kind list output is not JSON: %v", err)
	}
	if len(byKind.Items) != 1 {
		t.Errorf("expected 1 object for --credential-kind api_key, got %d", len(byKind.Items))
	}

	rl.Section("List by an unmatched run id")
	noSuchRun := "00000000-0000-0000-0000-000000000000"
	runOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "list",
		"--type", "StampCliObject",
		"--run-id", noSuchRun,
		"--project", projectID,
		"--output", "json",
	)
	rl.CLI("memory graph objects list --run-id "+noSuchRun+" --project "+projectID+" --output json", runOut)

	var byRun stampListResponse
	if err := json.Unmarshal([]byte(runOut), &byRun); err != nil {
		t.Fatalf("run-id list output is not JSON (a filter that matches nothing must not error): %v\n%s", err, truncate(runOut, 300))
	}
	if len(byRun.Items) != 0 {
		t.Errorf("expected an empty page for --run-id %s, got %d items", noSuchRun, len(byRun.Items))
	}
	if byRun.Total != 0 {
		t.Errorf("expected total=0 for an unmatched run, got %d", byRun.Total)
	}

	rl.Section("Show the stamp in graph objects get")
	getOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "get", objectID,
		"--project", projectID,
	)
	rl.CLI("memory graph objects get "+objectID+" --project "+projectID, getOut)

	if !strings.Contains(getOut, "Credential:  api_key "+credentialID) {
		t.Errorf("expected get output to show the write-stamp credential, got:\n%s", truncate(getOut, 500))
	}
	if !strings.Contains(getOut, "Data class:  synthesized") {
		t.Errorf("expected get output to show the data class, got:\n%s", truncate(getOut, 500))
	}

	rl.Section("Reject an unknown credential kind locally")
	_, err := runCLIInDirWithHome(t, "", home,
		"graph", "objects", "list",
		"--credential-kind", "oauth",
		"--project", projectID,
	)
	if err == nil {
		t.Error("expected --credential-kind oauth to be rejected")
	} else if !strings.Contains(err.Error(), "invalid --credential-kind") {
		t.Errorf("expected a local validation error naming the flag, got: %v", err)
	}
}
