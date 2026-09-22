// Package ftsquery builds resilient PostgreSQL full-text search queries.
package ftsquery

import (
	"strings"
	"unicode"
)

// Relax returns a fallback form of query for retrying a full-text search that
// returned no rows, and reports whether the fallback is worth running.
//
// PostgreSQL's websearch_to_tsquery turns a hyphenated run ("1997-06-13-44",
// "COVID-19") into a phrase and ANDs it with the remaining terms. In this schema
// that phrase can never match:
//
//   - a composite key such as "lov/1997-06-13-44" is indexed as a single opaque
//     lexeme (the default parser treats "x/y-z" as a file/host token), so its
//     components are absent from the tsvector; and
//   - tsvector positions saturate at the maximum (16383) for large objects, so
//     even when the components are present a phrase cannot be satisfied.
//
// Because the clause is ANDed, one unsatisfiable term silently zeroes an
// otherwise valid search:
//
//	"aksjeloven lov 1997-06-13-44" -> 0 rows
//	"aksjeloven lov"               -> 247 rows, the intended document ranked first
//
// Relax tokenises the query on non-alphanumeric boundaries and keeps only runs
// containing at least one letter, which both removes the phrase and drops purely
// numeric terms (dates, paragraph numbers, document ids) that carry little
// discriminative value next to the text terms.
//
// ok is false when there is nothing to relax: no terms survive, or the result is
// identical to the query it would replace, in which case a retry is pointless.
// Callers must only use the result after the strict query returned no rows, so
// precision is never traded away for a query that already worked.
func Relax(query string) (string, bool) {
	var (
		kept    []string
		current []rune
	)

	flush := func() {
		if len(current) == 0 {
			return
		}
		if run := string(current); hasLetter(run) {
			kept = append(kept, run)
		}
		current = current[:0]
	}

	for _, r := range query {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			current = append(current, r)
			continue
		}
		flush()
	}
	flush()

	if len(kept) == 0 {
		return "", false
	}

	relaxed := strings.Join(kept, " ")
	if relaxed == collapse(query) {
		return "", false
	}
	return relaxed, true
}

// collapse trims s and reduces internal whitespace runs to a single space, so a
// query that differs from its relaxed form only by whitespace is not retried.
func collapse(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// hasLetter reports whether s contains at least one letter.
func hasLetter(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}
