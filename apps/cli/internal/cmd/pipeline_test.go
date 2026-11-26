package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/projects"
	"github.com/spf13/cobra"
)

func TestPrintPipelineViewJSON(t *testing.T) {
	orig := output
	output = "json"
	defer func() { output = orig }()

	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)

	printPipelineView(cmd, &projects.PipelineView{
		Mode:             "auto",
		Policy:           json.RawMessage(`{"mode":"auto"}`),
		SchemaGeneration: 3,
	})

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}
	if got["mode"] != "auto" || got["schema_generation"] != float64(3) {
		t.Fatalf("unexpected view: %v", got)
	}
}

func TestPrintPipelineViewText(t *testing.T) {
	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)

	printPipelineView(cmd, &projects.PipelineView{
		Mode:       "suggest",
		Customized: true,
		Policy:     json.RawMessage(`{"mode":"suggest"}`),
	})

	out := buf.String()
	if !strings.Contains(out, "Mode: suggest (customized)") {
		t.Fatalf("expected customized mode line, got:\n%s", out)
	}
	if !strings.Contains(out, "Policy:") {
		t.Fatalf("expected policy section, got:\n%s", out)
	}
}

func TestPipelineSetModeValidation(t *testing.T) {
	valid := []string{"off", "suggest", "auto"}
	for _, m := range valid {
		if !isPipelineMode(m) {
			t.Fatalf("isPipelineMode(%q) = false", m)
		}
	}
	if isPipelineMode("bogus") {
		t.Fatal("isPipelineMode(\"bogus\") = true")
	}
}
