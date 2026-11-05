// Package cli_test — blueprint_type_conflicts_test.go
//
// CLI coverage for blueprint object-type collision preflight: install blocks
// without --merge-types in non-interactive mode, prints property-level details,
// merges with the flag, respects dry-run, and ignores same-pack upgrades.
package cli_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeBlueprintPackYAML writes <root>/schemas/<file> with one pack/type.
func writeBlueprintPackYAML(t *testing.T, root, file, packName, version string, propertiesYAML string) {
	t.Helper()
	schemasDir := filepath.Join(root, "schemas")
	if err := os.MkdirAll(schemasDir, 0o755); err != nil {
		t.Fatalf("mkdir schemas: %v", err)
	}
	content := fmt.Sprintf(`name: %s
version: "%s"
objectTypes:
  - name: Thing
    label: Thing
    properties:
%s
`, packName, version, propertiesYAML)
	if err := os.WriteFile(filepath.Join(schemasDir, file), []byte(content), 0o644); err != nil {
		t.Fatalf("write pack yaml: %v", err)
	}
}

const baseThingPropertiesYAML = `      title:
        type: string
      status:
        type: string
        enum: [open, closed]`

const conflictThingPropertiesYAML = `      title:
        type: string
        description: incoming title
      description:
        type: string
      owner:
        type: string`

type cliCompiledTypes struct {
	ObjectTypes []struct {
		Name       string         `json:"name"`
		Shadowed   bool           `json:"shadowed"`
		Properties map[string]any `json:"properties"`
	} `json:"objectTypes"`
}

func cliCompiledTypeProperties(t *testing.T, home, projectID, typeName string) map[string]any {
	t.Helper()
	out := mustRunCLIInDirWithHome(t, "", home,
		"schemas", "compiled-types",
		"--project", projectID,
		"--output", "json",
	)
	var compiled cliCompiledTypes
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &compiled); err != nil {
		t.Fatalf("decode compiled-types output: %v\noutput:\n%s", err, truncate(out, 1000))
	}
	var found map[string]any
	for _, ot := range compiled.ObjectTypes {
		if ot.Name == typeName && !ot.Shadowed {
			found = ot.Properties
		}
	}
	if found == nil {
		t.Fatalf("compiled type %q not found in output:\n%s", typeName, truncate(out, 1000))
	}
	return found
}

func setupCLIConflictProject(t *testing.T, prefix string) (home, projectID string) {
	t.Helper()
	home = t.TempDir()
	requireServerReady(t, home)
	projectID = createProject(t, home, serverURL(), uniqueProjectName(prefix))
	deleteProjectOnCleanup(t, home, projectID)
	// The CLI subprocess has no TTY; force the explicit non-interactive path.
	t.Setenv("CI", "1")
	return home, projectID
}

func TestCLIInstalled_BlueprintTypeConflicts_BaseInstallSucceeds(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	rl.Describe("Install a base blueprint with no existing types")

	home, projectID := setupCLIConflictProject(t, "e2e-bp-base")
	baseDir := filepath.Join(home, "base-bp")
	writeBlueprintPackYAML(t, baseDir, "base.yaml", "base-pack", "1.0.0", baseThingPropertiesYAML)

	out := mustRunCLIInDirWithHome(t, "", home, "blueprints", "install", baseDir, "--project", projectID)
	rl.CLI("memory blueprints install "+baseDir+" --project "+projectID, out)

	props := cliCompiledTypeProperties(t, home, projectID, "Thing")
	if props["title"] == nil || props["status"] == nil {
		t.Fatalf("expected base Thing properties title/status, got %v", props)
	}
}

func TestCLIInstalled_BlueprintTypeConflicts_NonInteractiveBlocks(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	rl.Describe("A conflicting install without --merge-types must abort with zero mutations")

	home, projectID := setupCLIConflictProject(t, "e2e-bp-block")
	baseDir := filepath.Join(home, "base-bp")
	conflictDir := filepath.Join(home, "conflict-bp")
	writeBlueprintPackYAML(t, baseDir, "base.yaml", "base-pack", "1.0.0", baseThingPropertiesYAML)
	writeBlueprintPackYAML(t, conflictDir, "conflict.yaml", "conflict-pack", "1.0.0", conflictThingPropertiesYAML)

	mustRunCLIInDirWithHome(t, "", home, "blueprints", "install", baseDir, "--project", projectID)

	out, err := runCLIInDirWithHome(t, "", home, "blueprints", "install", conflictDir, "--project", projectID)
	rl.CLIErr("memory blueprints install "+conflictDir+" --project "+projectID, out, err, 0)
	if err == nil {
		t.Fatalf("expected conflicting install to fail non-interactively, output:\n%s", out)
	}
	for _, want := range []string{"Thing", "base-pack", "conflict-pack", "description", "owner", "existing definition wins", "--merge-types"} {
		if !strings.Contains(out, want) {
			t.Errorf("conflicting install output missing %q:\n%s", want, truncate(out, 1500))
		}
	}

	props := cliCompiledTypeProperties(t, home, projectID, "Thing")
	if props["description"] != nil || props["owner"] != nil {
		t.Fatalf("blocked install mutated compiled Thing properties: %v", props)
	}
	installed := mustRunCLIInDirWithHome(t, "", home, "schemas", "installed", "--project", projectID)
	if strings.Contains(installed, "conflict-pack") {
		t.Fatalf("blocked install assigned conflict-pack:\n%s", truncate(installed, 1000))
	}
}

func TestCLIInstalled_BlueprintTypeConflicts_MergeFlagApplies(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	rl.Describe("A conflicting install with --merge-types applies and merges properties")

	home, projectID := setupCLIConflictProject(t, "e2e-bp-merge")
	baseDir := filepath.Join(home, "base-bp")
	conflictDir := filepath.Join(home, "conflict-bp")
	writeBlueprintPackYAML(t, baseDir, "base.yaml", "base-pack", "1.0.0", baseThingPropertiesYAML)
	writeBlueprintPackYAML(t, conflictDir, "conflict.yaml", "conflict-pack", "1.0.0", conflictThingPropertiesYAML)

	mustRunCLIInDirWithHome(t, "", home, "blueprints", "install", baseDir, "--project", projectID)
	out := mustRunCLIInDirWithHome(t, "", home,
		"blueprints", "install", conflictDir, "--project", projectID, "--merge-types")
	rl.CLI("memory blueprints install "+conflictDir+" --project "+projectID+" --merge-types", out)

	props := cliCompiledTypeProperties(t, home, projectID, "Thing")
	if props["description"] == nil || props["owner"] == nil {
		t.Fatalf("expected merged Thing properties description/owner, got %v", props)
	}
}

func TestCLIInstalled_BlueprintTypeConflicts_DryRunStaysOfflineAndDoesNotMutate(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	rl.Describe("Dry-run makes no merge-preview API call and applies nothing")

	home, projectID := setupCLIConflictProject(t, "e2e-bp-dry")
	baseDir := filepath.Join(home, "base-bp")
	conflictDir := filepath.Join(home, "conflict-bp")
	writeBlueprintPackYAML(t, baseDir, "base.yaml", "base-pack", "1.0.0", baseThingPropertiesYAML)
	writeBlueprintPackYAML(t, conflictDir, "conflict.yaml", "conflict-pack", "1.0.0", conflictThingPropertiesYAML)

	mustRunCLIInDirWithHome(t, "", home, "blueprints", "install", baseDir, "--project", projectID)
	out := mustRunCLIInDirWithHome(t, "", home,
		"blueprints", "install", conflictDir, "--project", projectID, "--dry-run")
	rl.CLI("memory blueprints install "+conflictDir+" --project "+projectID+" --dry-run", out)
	if strings.Contains(out, "existing definition wins") {
		t.Fatalf("dry-run must not fetch server conflict details, output:\n%s", truncate(out, 1500))
	}

	props := cliCompiledTypeProperties(t, home, projectID, "Thing")
	if props["description"] != nil || props["owner"] != nil {
		t.Fatalf("dry-run mutated compiled Thing properties: %v", props)
	}
	installed := mustRunCLIInDirWithHome(t, "", home, "schemas", "installed", "--project", projectID)
	if strings.Contains(installed, "conflict-pack") {
		t.Fatalf("dry-run assigned conflict-pack:\n%s", truncate(installed, 1000))
	}
}

func TestCLIInstalled_BlueprintTypeConflicts_SamePackUpgradeNoMergeNeeded(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	rl.Describe("Upgrading the same pack name with a new property needs no merge flag")

	home, projectID := setupCLIConflictProject(t, "e2e-bp-upgrade")
	baseDir := filepath.Join(home, "base-bp")
	writeBlueprintPackYAML(t, baseDir, "base.yaml", "base-pack", "1.0.0", baseThingPropertiesYAML)
	mustRunCLIInDirWithHome(t, "", home, "blueprints", "install", baseDir, "--project", projectID)

	upgradedProperties := baseThingPropertiesYAML + `
      extra:
        type: string`
	writeBlueprintPackYAML(t, baseDir, "base.yaml", "base-pack", "2.0.0", upgradedProperties)
	out := mustRunCLIInDirWithHome(t, "", home,
		"blueprints", "install", baseDir, "--project", projectID, "--upgrade")
	rl.CLI("memory blueprints install "+baseDir+" --project "+projectID+" --upgrade", out)

	props := cliCompiledTypeProperties(t, home, projectID, "Thing")
	if props["extra"] == nil {
		t.Fatalf("expected same-pack upgrade extra property, got %v", props)
	}
}

func TestCLIInstalled_BlueprintTypeConflicts_NoConflictInstallUnchanged(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	rl.Describe("A blueprint with no existing type names installs normally")

	home, projectID := setupCLIConflictProject(t, "e2e-bp-clean")
	cleanDir := filepath.Join(home, "clean-bp")
	writeBlueprintPackYAML(t, cleanDir, "clean.yaml", "clean-pack", "1.0.0", conflictThingPropertiesYAML)

	out := mustRunCLIInDirWithHome(t, "", home, "blueprints", "install", cleanDir, "--project", projectID)
	rl.CLI("memory blueprints install "+cleanDir+" --project "+projectID, out)

	props := cliCompiledTypeProperties(t, home, projectID, "Thing")
	if props["description"] == nil || props["owner"] == nil {
		t.Fatalf("expected clean install properties, got %v", props)
	}
}
