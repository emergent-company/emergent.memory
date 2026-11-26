package main

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

// The objects browser's search box answers two different questions, and they
// are not the same operation:
//
//   - a relevance query ("boiler service") is ranked by the search endpoints;
//   - an object id is a *lookup* — the reader already knows which object they
//     want, and there is exactly one right answer.
//
// Forwarding an id to a relevance search answered the first question instead of
// the second: the query text has no lexical match and a meaningless embedding,
// so the fused ranking returned the nearest neighbours (two dozen unrelated
// objects, scores 0.10–0.23). This file separates the two: `id:<uuid>` (and a
// bare UUID, which can only be an id) resolves the object exactly, and every
// other input stays a relevance search.

// objectSearchTarget is what the search box asked for.
type objectSearchTarget struct {
	// Text is the relevance query forwarded to the search endpoints.
	Text string
	// ID is the canonical uuid to resolve exactly; empty for a text search.
	ID string
	// InvalidID is the payload of an explicit `id:` request that is not a uuid.
	// It is kept so the page can say what was wrong rather than fall back to a
	// relevance search for text the reader did not mean as one.
	InvalidID string
}

// parseObjectSearchQuery classifies the search box's input.
//
// `id:<uuid>` is the documented form: it says what the reader means, so a
// malformed value can be reported instead of silently searched. A bare uuid is
// accepted too — it cannot be meaningful query text, and treating one as a
// query is exactly the bug this fixes. Anything else is a relevance query,
// untouched.
func parseObjectSearchQuery(raw string) objectSearchTarget {
	text := strings.TrimSpace(raw)
	payload := text
	explicit := false
	if len(text) >= len("id:") && strings.EqualFold(text[:len("id:")], "id:") {
		payload = strings.TrimSpace(text[len("id:"):])
		explicit = true
	}
	if id, err := uuid.Parse(payload); err == nil {
		// Normalize: uuid.Parse accepts upper case, braces and the urn form, and
		// the API resolves the canonical lower-case hyphenated id.
		return objectSearchTarget{ID: id.String()}
	}
	if explicit {
		return objectSearchTarget{InvalidID: payload}
	}
	return objectSearchTarget{Text: text}
}

// objectSearchLookup is the page's record of an exact-id search: the id that
// was asked for, and — when nothing resolved — why, so the empty state can name
// the reason instead of leaving the reader with a blank result.
type objectSearchLookup struct {
	// ID is the canonical id the lookup asked for. Empty when the request was a
	// malformed `id:` value, where there is no id to name.
	ID string
	// Reason explains a miss: empty when the object was found.
	Reason string
	// Detail is the right-hand note of the results summary when the object was
	// found — it says which visible filters the exact lookup did not apply.
	Detail string
}

// objectSearchIDLookup resolves one id to exactly one object.
//
// The lookup is deliberately unconditional: an id names one object, so the
// browser's type and branch filters cannot turn a known id into "no results".
// They are visible controls, though, so the summary says they were not applied
// rather than pretending the result is inside the current filter.
//
// A miss is not an error: it returns no results plus the reason, which the page
// renders as an explained empty state. Only a real backend failure is returned
// as an error.
func (s *Server) objectSearchIDLookup(ctx context.Context, target objectSearchTarget, typeFilter, branchID string) ([]ObjectSearchResult, *objectSearchLookup, error) {
	if target.InvalidID != "" {
		return nil, &objectSearchLookup{
			Reason: "An object id is a UUID — for example d612b1f9-73b0-4d71-88d8-2e590a5943b5. Paste the whole id, or drop the id: prefix to search the text instead.",
		}, nil
	}

	obj, err := s.memory.GetGraphObject(ctx, target.ID)
	if err != nil {
		// A malformed or unknown id is a miss, not a page failure: the API
		// answers 404 for an id it cannot resolve, and 400 when the id is not a
		// uuid at all.
		if isMemoryStatus(err, http.StatusNotFound) || isMemoryStatus(err, http.StatusBadRequest) {
			return nil, &objectSearchLookup{
				ID:     target.ID,
				Reason: "No object with this id exists in this project. Check that you pasted the whole id — or clear the search to browse instead.",
			}, nil
		}
		return nil, nil, err
	}

	return []ObjectSearchResult{{Object: *obj}}, &objectSearchLookup{
		ID:     target.ID,
		Detail: objectSearchLookupDetail(typeFilter, branchID),
	}, nil
}

// objectSearchLookupDetail names the filters an exact lookup bypassed, so a
// result that sits outside them is explained rather than surprising. It is
// empty when no filter is set: there is nothing to explain.
func objectSearchLookupDetail(typeFilter, branchID string) string {
	switch {
	case typeFilter != "" && branchID != "":
		return "type and branch filters not applied"
	case typeFilter != "":
		return "type filter not applied"
	case branchID != "":
		return "branch filter not applied"
	default:
		return ""
	}
}

// The relevance floor for unified hits is two rules, because they answer two
// different questions.
//
// objectsSearchMatchScore is a NOISE floor, not a relevance claim: a fused score
// is a blend the server does not calibrate across queries, so the same number
// means different things for a pasted id and for a real keyword.
//
// It is set from measurement, and the measurement forbids anything higher. In
// this project a query that matched nothing (a pasted UUID, answered by the
// vector leg's nearest neighbours) returned 24 objects scoring 0.10–0.23, while
// a query that did match ("boiler" — the key of a real Obligation) topped out at
// 0.22. There is no threshold that separates those two, so any floor above ~0.10
// hides real matches to hide junk. What is left is the bottom of the junk band:
// hits at or below it are no better than an unrelated neighbour, and a query
// made only of such hits says "no strong matches" instead of listing them.
//
// objectsSearchTailRatio does the trimming that is actually defensible: once
// something matched, hits far below the best hit add rows without adding
// answers. It is relative because a fused score is only comparable within one
// query.
//
// Neither rule drops data. Everything held back is counted and offered behind a
// "show all" link, so a wrong floor costs a click rather than a lost object. Only
// unified search is split: full-text and hybrid hits come from endpoints with
// their own score scales, where these numbers mean nothing.
//
// The real fix is server-side: the vector leg always returns its nearest
// neighbours and nothing marks them as weak, so the top hit of a nonsense query
// scores like the top hit of a good one.
const (
	objectsSearchMatchScore = 0.10
	objectsSearchTailRatio  = 0.35
)

// objectSearchResultsShown returns the hits worth rendering as results. With
// showAll every hit is kept; otherwise a hit is held back when it is below the
// absolute floor, or below objectsSearchTailRatio of the query's best hit. The
// caller counts whatever was left out and offers it behind a link.
func objectSearchResultsShown(results []ObjectSearchResult, showAll bool) []ObjectSearchResult {
	if showAll || len(results) == 0 {
		return results
	}
	top := results[0].Score
	for _, r := range results[1:] {
		if r.Score > top {
			top = r.Score
		}
	}
	floor := float32(objectsSearchMatchScore)
	if tail := top * objectsSearchTailRatio; tail > floor {
		floor = tail
	}
	shown := make([]ObjectSearchResult, 0, len(results))
	for _, r := range results {
		if r.Score < floor {
			continue
		}
		shown = append(shown, r)
	}
	return shown
}

// objectSearchShowsAll reports whether the request asked for the held-back hits
// to be shown ("?all=1", also accepting the true-ish spellings a hand-written
// or bookmarked URL may carry).
func objectSearchShowsAll(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "all":
		return true
	default:
		return false
	}
}
