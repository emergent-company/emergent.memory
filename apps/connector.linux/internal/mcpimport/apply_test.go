package mcpimport

import (
	"testing"

	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/mcphost"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/mcpconfig"
)

func parsed(servers ...mcpconfig.Server) mcpconfig.Result {
	return mcpconfig.Result{Servers: servers}
}

func TestApplyCreates(t *testing.T) {
	next, result := Apply(nil, parsed(
		mcpconfig.Server{Name: "a", Transport: mcpconfig.TransportStdio, Command: "run"},
		mcpconfig.Server{Name: "b", Transport: mcpconfig.TransportHTTP, URL: "https://x"},
	), false, nil)

	if len(next) != 2 || len(result.Created) != 2 {
		t.Fatalf("next=%d created=%v", len(next), result.Created)
	}
	if result.Changed() != true {
		t.Fatal("changed should be true")
	}
}

func TestApplySkipsExistingUnlessOverwrite(t *testing.T) {
	existing := []mcphost.ServerConfig{
		{Name: "a", Transport: mcphost.TransportStdio, Command: "old"},
	}

	next, result := Apply(existing, parsed(
		mcpconfig.Server{Name: "a", Transport: mcpconfig.TransportStdio, Command: "new"},
	), false, nil)
	if len(result.Skipped) != 1 || next[0].Command != "old" {
		t.Fatalf("skip result=%+v command=%q", result, next[0].Command)
	}

	next, result = Apply(existing, parsed(
		mcpconfig.Server{Name: "a", Transport: mcpconfig.TransportStdio, Command: "new"},
	), true, nil)
	if len(result.Updated) != 1 || next[0].Command != "new" {
		t.Fatalf("overwrite result=%+v command=%q", result, next[0].Command)
	}
}

func TestApplyOverwritePreservesLocalState(t *testing.T) {
	disabled := false
	existing := []mcphost.ServerConfig{{
		Name: "a", Transport: mcphost.TransportStdio, Command: "old",
		Enabled: &disabled, DisabledTools: []string{"danger"},
	}}

	next, _ := Apply(existing, parsed(
		mcpconfig.Server{Name: "a", Transport: mcpconfig.TransportStdio, Command: "new"},
	), true, nil)

	if next[0].IsEnabled() {
		t.Error("enabled state must be preserved (disabled)")
	}
	if len(next[0].DisabledTools) != 1 || next[0].DisabledTools[0] != "danger" {
		t.Errorf("disabled tools = %v", next[0].DisabledTools)
	}
}

func TestApplySkipsManaged(t *testing.T) {
	_, result := Apply(nil, parsed(
		mcpconfig.Server{Name: "managed", Transport: mcpconfig.TransportStdio, Command: "run"},
	), true, func(name string) bool { return name == "managed" })

	if len(result.Skipped) != 1 || result.Skipped[0].Reason != "managed by Memory" {
		t.Fatalf("result = %+v", result)
	}
}

func TestApplyReportsInvalidEntries(t *testing.T) {
	_, result := Apply(nil, parsed(
		mcpconfig.Server{Name: "good", Transport: mcpconfig.TransportStdio, Command: "run"},
		mcpconfig.Server{Name: "bad", Transport: mcpconfig.Transport("carrier-pigeon")},
	), false, nil)

	if len(result.Created) != 1 || len(result.Failed) != 1 {
		t.Fatalf("result = %+v", result)
	}
}

func TestApplyCarriesParserErrorsAndWarnings(t *testing.T) {
	input := mcpconfig.Result{
		Servers:  nil,
		Warnings: []mcpconfig.Warning{{Name: "x", Message: "ignored"}},
		Errors:   []mcpconfig.EntryError{{Name: "y", Err: errTest("boom")}},
	}
	_, result := Apply(nil, input, false, nil)
	if len(result.Warnings) != 1 || len(result.Failed) != 1 {
		t.Fatalf("result = %+v", result)
	}
}

type errTest string

func (e errTest) Error() string { return string(e) }
