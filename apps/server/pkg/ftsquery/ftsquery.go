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
//
// ok is also false when the query carries websearch_to_tsquery operator syntax
// that relaxation would strip or invert (a phrase, a negation, or a boolean
// OR). Relax drops every non-alphanumeric, so it would silently remove a phrase
// or OR and would turn a negation into a positive term, surfacing documents the
// user explicitly excluded. A retry under those conditions is unsafe.
func Relax(query string) (string, bool) {
	if hasOperatorSyntax(query) {
		return "", false
	}

	kept := terms(query)
	if len(kept) == 0 {
		return "", false
	}

	relaxed := strings.Join(kept, " ")
	if relaxed == collapse(query) {
		return "", false
	}
	return relaxed, true
}

// Disjoin returns an OR-form of query for retrying a full-text search whose
// AND semantics matched nothing, and reports whether the fallback is worth
// running.
//
// websearch_to_tsquery ANDs every non-stopword term, so a natural multi-term
// query over a corpus where no single document contains every term returns
// zero rows even though each term is individually well represented. Disjoin
// joins the surviving terms with `|`, which a caller feeds to to_tsquery (NOT
// websearch_to_tsquery) to match any document containing at least one term.
// ts_rank_cd over that OR query still rewards documents that cover more of the
// terms, so recall is restored without collapsing ranking to "any one term".
//
// ok is false when there is nothing to disjoin: fewer than two terms survive
// (a single-term query is already an OR of one term, and its strict AND match
// already runs first), or the query carries websearch_to_tsquery operator
// syntax (a phrase, negation, or explicit OR) that an OR-disjunction would
// strip or invert. Callers must only use the result after the strict query
// returned no rows, so precision is never traded away for a query that already
// worked.
func Disjoin(query string) (string, bool) {
	if hasOperatorSyntax(query) {
		return "", false
	}

	kept := terms(query)
	if len(kept) < 2 {
		return "", false
	}
	return strings.Join(kept, " | "), true
}

// terms tokenizes query on non-alphanumeric boundaries, keeping only runs that
// contain at least one letter. Numeric runs (dates, paragraph numbers,
// document ids) carry little discriminative value next to text terms and are
// dropped by callers that need letter-bearing terms.
func terms(query string) []string {
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

	return kept
}

// hasOperatorSyntax reports whether query carries websearch_to_tsquery operator
// syntax that Relax would strip or invert: a double quote (phrase syntax), a
// negation operator ("foo -bar"), or a boolean OR ("or"/"OR", or "|").
func hasOperatorSyntax(query string) bool {
	runes := []rune(query)
	for i, r := range runes {
		switch r {
		case '"', '|':
			return true
		case '-':
			// A hyphen is negation only when it begins a whitespace-delimited
			// token (preceded by start-of-string or whitespace) and is
			// immediately followed by a letter or digit. Hyphens inside a token
			// ("state-of-the-art", "1997-06-13-44") are not negation.
			if i > 0 && !unicode.IsSpace(runes[i-1]) {
				continue
			}
			if i+1 < len(runes) {
				next := runes[i+1]
				if unicode.IsLetter(next) || unicode.IsDigit(next) {
					return true
				}
			}
		}
	}

	for _, tok := range strings.Fields(query) {
		if strings.EqualFold(tok, "or") {
			return true
		}
	}
	return false
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
