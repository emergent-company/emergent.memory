package migrationguard

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// migrationFilenameRE matches the migration filename convention
// <version>_<lower_snake_name>.sql, where <version> is zero-padded (leading
// zero). Non-conforming names — README.md, embed.go, an unpadded version like
// 176_foo.sql, or an uppercase suffix like 00176_Foo.sql — are not migrations.
var migrationFilenameRE = regexp.MustCompile(`^(0\d+)_[a-z0-9_]+\.sql$`)

// exemptDirectiveRE matches a line carrying the escape-hatch directive.
// Format: -- out-of-order-migration-allowed: <reason>. Matching is
// case-insensitive and requires a non-empty reason.
var exemptDirectiveRE = regexp.MustCompile(`(?i)^\s*--\s*out-of-order-migration-allowed\s*:\s*(.*?)\s*$`)

// ExemptDirective is the documented escape hatch. An added migration file whose
// contents contain a line matching this directive (case-insensitive) WITH a
// non-empty reason is exempt from the order rule.
// Format:  -- out-of-order-migration-allowed: <reason>
const ExemptDirective = "out-of-order-migration-allowed"

// VersionFromFilename extracts the migration version from a migration filename.
// The name may be a bare basename or a (root-relative) path; only the basename
// is inspected. ok is false when the name does not match the convention.
func VersionFromFilename(name string) (int64, bool) {
	m := migrationFilenameRE.FindStringSubmatch(path.Base(name))
	if m == nil {
		return 0, false
	}
	v, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// MaxVersion returns the highest version among the given filenames. Non-migration
// filenames are ignored. ok is false when no migration filenames are present.
func MaxVersion(names []string) (int64, bool) {
	var max int64
	ok := false
	for _, name := range names {
		v, valid := VersionFromFilename(name)
		if !valid {
			continue
		}
		if !ok || v > max {
			max = v
			ok = true
		}
	}
	return max, ok
}

// ExemptReason returns the reason text when the content carries the directive
// with a non-empty reason. ok is false otherwise.
func ExemptReason(content string) (string, bool) {
	for _, line := range strings.Split(content, "\n") {
		m := exemptDirectiveRE.FindStringSubmatch(strings.TrimSuffix(line, "\r"))
		if m == nil {
			continue
		}
		reason := strings.TrimSpace(m[1])
		if reason != "" {
			return reason, true
		}
	}
	return "", false
}

// AddedMigration describes a migration file added by the PR.
type AddedMigration struct {
	Path    string
	Version int64
	Exempt  bool
	Reason  string
}

// Violation is one added migration below the base maximum.
type Violation struct {
	Path    string
	Version int64
	BaseMax int64
}

// FindViolations returns added migrations whose version < baseMax and which are
// not exempt. baseMax<=0 means "no base migrations" and yields no violations.
// Results are sorted by Version then Path.
func FindViolations(added []AddedMigration, baseMax int64) []Violation {
	if baseMax <= 0 {
		return nil
	}
	var violations []Violation
	for _, m := range added {
		if m.Exempt {
			continue
		}
		if m.Version < baseMax {
			violations = append(violations, Violation{
				Path:    m.Path,
				Version: m.Version,
				BaseMax: baseMax,
			})
		}
	}
	sort.Slice(violations, func(i, j int) bool {
		if violations[i].Version != violations[j].Version {
			return violations[i].Version < violations[j].Version
		}
		return violations[i].Path < violations[j].Path
	})
	return violations
}

// NextFreeVersion returns the lowest migration version that is free both on the
// base branch (whose maximum is baseMax) and within the pull request's own
// additions. It is baseMax+1 unless an added migration already occupies that
// number or higher, in which case the suggestion is advanced past every added
// version. This keeps the remediation's "next free version" accurate when a
// single pull request adds several migrations (e.g. baseMax 176 with added
// 00175 and 00177 → 178, not 177 which the pull request already uses).
func NextFreeVersion(baseMax int64, added []AddedMigration) int64 {
	next := baseMax + 1
	for _, m := range added {
		if m.Version >= next {
			next = m.Version + 1
		}
	}
	return next
}

// FormatViolationError renders the exact CI failure message. It includes the base
// maximum version, each offending file path + version, the remediation (renumber
// above baseMax to nextFree, via `git mv`), and the documented escape hatch. It
// returns "" when violations is empty.
func FormatViolationError(violations []Violation, baseMax, nextFree int64) string {
	if len(violations) == 0 {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "out-of-order migration detected: base branch already has migrations up to version %d\n", baseMax)
	for _, v := range violations {
		fmt.Fprintf(&b, "  - %s (version %d)\n", v.Path, v.Version)
	}
	fmt.Fprintf(&b, "\nRemediation: renumber the added migration(s) ABOVE the base maximum.\n")
	fmt.Fprintf(&b, "The next free version is %d. Rename the file, e.g.:\n", nextFree)
	fmt.Fprintf(&b, "  git mv <offending-file> %d_<same-name>.sql\n", nextFree)
	fmt.Fprintf(&b, "\nIf you are deliberately filling a gap (this is legitimate and safe only\n")
	fmt.Fprintf(&b, "when the lower version has NOT been applied anywhere), you may exempt the\n")
	fmt.Fprintf(&b, "file by adding the following line to the added migration:\n")
	fmt.Fprintf(&b, "  -- %s: <reason>\n", ExemptDirective)

	return b.String()
}
