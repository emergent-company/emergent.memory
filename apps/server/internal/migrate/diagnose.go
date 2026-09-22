package migrate

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// MissingMigration is a single migration file that goose detected as missing
// before the database's current version.
type MissingMigration struct {
	Version  int64
	Filename string
}

// MissingMigrationsError is the rich diagnostic returned when goose reports a
// gap of missing migrations before the database's current version.
type MissingMigrationsError struct {
	Count          int
	CurrentVersion int64
	Missing        []MissingMigration
	Err            error
}

// Error renders a human-readable diagnostic that names the gap, lists every
// missing migration, gives an explicit remediation path, and then appends the
// raw goose error.
func (e *MissingMigrationsError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "found %d missing migration(s) before current version %d:\n", e.Count, e.CurrentVersion)
	for _, m := range e.Missing {
		fmt.Fprintf(&b, "\tversion %d: %s\n", m.Version, m.Filename)
	}
	b.WriteString("\nRemediation:\n")
	b.WriteString("  To apply the missing migrations out of order, allow missing migrations:\n")
	b.WriteString("    emergent-migrate -c up -allow-missing\n")
	b.WriteString("    (or: goose ... up -allow-missing)\n")
	b.WriteString("  Alternatively, if the gap is obsolete (the missing migrations were\n")
	b.WriteString("  intentionally removed), renumber new migrations above the current max\n")
	b.WriteString("  version instead of reusing the lower numbers.\n")
	if e.Err != nil {
		fmt.Fprintf(&b, "\noriginal error: %v", e.Err)
	}
	return b.String()
}

// Unwrap returns the underlying goose error.
func (e *MissingMigrationsError) Unwrap() error {
	return e.Err
}

// missingHeaderRe matches goose's gap header, anchored to the start of a line
// so it is not confused with an entry line. The header looks like:
//
//	error: found 2 missing migrations before current version 172:
var missingHeaderRe = regexp.MustCompile(`(?m)^.*found (\d+) missing migrations before current version (\d+):`)

// missingEntryRe matches each tab-indented missing entry, anchored to the start
// of a line. An entry has the form "\tversion 170: 00170_embedding_indexes_hnsw.sql".
var missingEntryRe = regexp.MustCompile(`(?m)^\tversion (\d+): (\S+)$`)

// DiagnoseError inspects err and, when it matches goose's "missing migrations
// before current version" gap error, returns a rich *MissingMigrationsError.
// Otherwise it returns err unchanged. Wrapped errors (e.g. via
// fmt.Errorf("...: %w", err)) are matched through err.Error().
func DiagnoseError(err error) error {
	if err == nil {
		return nil
	}

	msg := err.Error()
	header := missingHeaderRe.FindStringSubmatch(msg)
	if header == nil {
		return err
	}

	count, convErr := strconv.Atoi(header[1])
	if convErr != nil {
		return err
	}
	currentVersion, convErr := strconv.ParseInt(header[2], 10, 64)
	if convErr != nil {
		return err
	}

	entries := missingEntryRe.FindAllStringSubmatch(msg, -1)
	missing := make([]MissingMigration, 0, len(entries))
	for _, entry := range entries {
		version, convErr := strconv.ParseInt(entry[1], 10, 64)
		if convErr != nil {
			continue
		}
		missing = append(missing, MissingMigration{
			Version:  version,
			Filename: entry[2],
		})
	}

	return &MissingMigrationsError{
		Count:          count,
		CurrentVersion: currentVersion,
		Missing:        missing,
		Err:            err,
	}
}
