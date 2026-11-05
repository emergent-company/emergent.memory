package cmd

import (
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/apps/cli/internal/blueprints"
)

func TestValidationFailure(t *testing.T) {
	warnReport := &blueprints.ValidationReport{
		Issues: []blueprints.ValidationIssue{
			{Severity: blueprints.ValidationWarning, Message: "style"},
		},
	}
	errReport := &blueprints.ValidationReport{
		Issues: []blueprints.ValidationIssue{
			{Severity: blueprints.ValidationError, Message: "structure"},
			{Severity: blueprints.ValidationWarning, Message: "style"},
		},
	}

	if err := validationFailure(warnReport, false); err != nil {
		t.Fatalf("warnings-only must exit 0 by default, got: %v", err)
	}
	if err := validationFailure(warnReport, true); err == nil {
		t.Fatal("expected --strict to fail on warnings")
	} else if !strings.Contains(err.Error(), "warning") || !strings.Contains(err.Error(), "strict") {
		t.Fatalf("expected a strict warning error, got: %v", err)
	}
	if err := validationFailure(errReport, false); err == nil {
		t.Fatal("expected errors to fail even without --strict")
	}
	if err := validationFailure(&blueprints.ValidationReport{}, true); err != nil {
		t.Fatalf("strict must pass a clean report, got: %v", err)
	}
	if err := validationFailure(&blueprints.ValidationReport{}, false); err != nil {
		t.Fatalf("default must pass a clean report, got: %v", err)
	}
}

func TestValidateCommandHasStrictFlag(t *testing.T) {
	flag := blueprintsValidateCmd.Flags().Lookup("strict")
	if flag == nil {
		t.Fatal("validate command is missing --strict")
	}
	if flag.DefValue != "false" {
		t.Fatalf("--strict default = %q, want false", flag.DefValue)
	}
	if blueprintsInstallCmd.Flags().Lookup("strict") != nil {
		t.Fatal("--strict must be scoped to validate, not install")
	}
}
