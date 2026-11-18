package toolargscan

import (
	"os"
	"path/filepath"
	"testing"
)

// TestScanCatchesTheShapesTheLineGrepMissed pins the reason this package
// exists: the old test matched one line at a time, so a validator written with
// errors.New or split across lines passed the scan and collapsed at runtime
// (issue #668 review).
func TestScanCatchesTheShapesTheLineGrepMissed(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "validators.go", `package fixture

func one() error {
	return fmt.Errorf("create_project: %q is required", "name")
}

func two() error {
	return errors.New("missing required parameter: entity_id")
}

func three() error {
	return fmt.Errorf(
		"update entity: invalid entity_id: %q",
		"abc",
	)
}

func four() error {
	return fmt.Errorf("query failed: %w", err)
}
`)

	findings, err := ScanDir(dir, Exemptions{})
	if err != nil {
		t.Fatalf("ScanDir: %v", err)
	}

	got := make(map[int]bool, len(findings))
	for _, f := range findings {
		got[f.Line] = true
	}
	for _, line := range []int{4, 8, 12} {
		if !got[line] {
			t.Errorf("line %d: validation-shaped call was not reported (findings: %+v)", line, findings)
		}
	}
	if len(findings) != 3 {
		t.Errorf("findings = %+v, want exactly the three validation-shaped calls", findings)
	}
}

// TestScanHonoursExemptions keeps a reviewed exemption from silently widening:
// a file exemption skips the file, a message exemption skips just that message,
// and both are explicit.
func TestScanHonoursExemptions(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "cache.go", `package fixture

func key() error {
	return fmt.Errorf("canonical json: unsupported argument type %T", v)
}
`)
	write(t, dir, "provider.go", `package fixture

func platform() error {
	return fmt.Errorf("connection testing is not supported for trigger sources")
}

func real() error {
	return fmt.Errorf("repo is required")
}
`)

	findings, err := ScanDir(dir, Exemptions{
		Files: map[string]string{"cache.go": "serialises a cache key, never reaches a caller"},
		Sites: map[Site]string{
			{File: "provider.go", Message: "connection testing is not supported for trigger sources"}: "a platform fact, not the caller's argument",
		},
	})
	if err != nil {
		t.Fatalf("ScanDir: %v", err)
	}

	if len(findings) != 1 {
		t.Fatalf("findings = %+v, want exactly the unexempted validator", findings)
	}
	if findings[0].File != "provider.go" || findings[0].Line != 8 {
		t.Fatalf("finding = %+v, want provider.go:8", findings[0])
	}
}

// TestSiteExemptionDoesNotLeakToANewSite is the #930 acceptance test. The
// exemption keys used to be message substrings, so a future caller-facing error
// that merely contained an exempted phrase was silently exempt from the scan —
// the scan stopped being a gate for exactly the class it exists to catch. A
// site key pins the file and the exact literal, so neither a longer message
// that starts with the phrase nor a copy in another file can inherit it.
func TestSiteExemptionDoesNotLeakToANewSite(t *testing.T) {
	dir := t.TempDir()
	// The reviewed, exempted call.
	write(t, dir, "probe.go", `package fixture

func existing() error {
	return fmt.Errorf("object not found")
}
`)
	// A new caller-facing error that contains the exempted phrase.
	write(t, dir, "newcall.go", `package fixture

func regression() error {
	return fmt.Errorf("object not found: %q", id)
}
`)
	// The same literal in a file the exemption was not written for.
	write(t, dir, "copy.go", `package fixture

func copied() error {
	return fmt.Errorf("object not found")
}
`)

	findings, err := ScanDir(dir, Exemptions{Sites: map[Site]string{
		{File: "probe.go", Message: "object not found"}: "scope probe",
	}})
	if err != nil {
		t.Fatalf("ScanDir: %v", err)
	}

	got := make(map[string]int, len(findings))
	for _, f := range findings {
		got[f.File] = f.Line
	}
	if len(findings) != 2 {
		t.Fatalf("findings = %+v, want exactly the two unexempted sites", findings)
	}
	if got["newcall.go"] != 4 {
		t.Errorf("a message that only contains the exempted phrase must still be reported: %+v", findings)
	}
	if got["copy.go"] != 4 {
		t.Errorf("an exempted literal in another file must still be reported: %+v", findings)
	}
	if _, ok := got["probe.go"]; ok {
		t.Errorf("the reviewed site must stay exempt: %+v", findings)
	}
}

// TestFirstStringLiteralReadsTheReviewedMessage guards the key extraction: an
// exemption names the literal the call passes, so a multi-line call or one with
// an escaped quote must still produce the message a reviewer wrote down.
func TestFirstStringLiteralReadsTheReviewedMessage(t *testing.T) {
	tests := []struct {
		args string
		want string
		ok   bool
	}{
		{`"missing required parameter: entity_id"`, "missing required parameter: entity_id", true},
		{"\n\t\t\"invalid \\\"thing\\\" value\",\n\t\tname,\n", `invalid "thing" value`, true},
		{"`raw message: not found`", "raw message: not found", true},
		{"msg", "", false},
	}
	for _, tt := range tests {
		got, ok := firstStringLiteral(tt.args)
		if ok != tt.ok || got != tt.want {
			t.Errorf("firstStringLiteral(%q) = (%q, %v), want (%q, %v)", tt.args, got, ok, tt.want, tt.ok)
		}
	}
}

// TestScanDoesNotEndACallOnAParenthesisInAMessage guards the balanced-paren
// reader: a ')' inside the message must not truncate the argument list before
// the keyword that follows it.
func TestScanDoesNotEndACallOnAParenthesisInAMessage(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "paren.go", `package fixture

func one() error {
	return fmt.Errorf("bad (truncated) value: invalid %s", v)
}
`)

	findings, err := ScanDir(dir, Exemptions{})
	if err != nil {
		t.Fatalf("ScanDir: %v", err)
	}
	if len(findings) != 1 || findings[0].Line != 4 {
		t.Fatalf("findings = %+v, want paren.go:4", findings)
	}
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}
