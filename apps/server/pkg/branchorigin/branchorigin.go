// Package branchorigin holds the closed vocabulary for why a branch exists and
// the normalisation every creation path applies to it.
//
// Origin is metadata — it labels a branch in the UI, lets a reader filter by
// kind, and lets cleanup find the branches a deleted source left behind (see
// #246). It is NOT a permission: every branch is read the same way (#265).
//
// Attribution is server-derived. A request body must never be able to claim it
// (the DTOs carry it as an untagged field), because a caller that could set
// origin_id could make its branch look like another source's, and cleanup
// deletes on that column.
package branchorigin

import (
	"errors"
	"strings"

	"github.com/google/uuid"
)

// The four stored origins. The database enforces the same set with a CHECK
// constraint (migration 00269_branch_origin.sql); the values here must not
// drift from it.
const (
	// Extraction marks a per-job staging branch written by the extraction
	// worker. ID is the extraction job, Label the source document's filename.
	Extraction = "extraction"
	// Agent marks a branch created by an agent run. ID is the run.
	Agent = "agent"
	// Manual marks a branch created by a person through the API or the UI. It
	// is the default for any caller that does not attribute itself.
	Manual = "manual"
	// System marks a branch created by a scheduled or blueprint-driven process.
	System = "system"
)

// MaxLabelRunes bounds OriginLabel. It is display-only text (a filename, an
// agent name), so truncation is safe where truncating an id would not be.
const MaxLabelRunes = 200

// ErrInvalidOrigin is returned for an origin outside the four stored values.
var ErrInvalidOrigin = errors.New("invalid origin: must be one of extraction, agent, manual, system")

// ErrInvalidID is returned when ID is not a UUID.
var ErrInvalidID = errors.New("invalid origin_id: must be a UUID")

// Attribution is who or what created a branch. A nil Attribution is valid and
// means "manual" — the default for a caller that does not attribute itself.
type Attribution struct {
	Origin string
	// ID is the creator's id: extraction job, agent run or user. Polymorphic on
	// purpose — a branch has exactly one creator.
	ID *string
	// ParsedID is ID in typed form (uuid.Nil when ID is nil). Resolve sets it so
	// a caller that needs a UUID does not re-parse an id it already validated.
	ParsedID uuid.UUID
	// Label is the creator as a person reads it: source document filename,
	// agent name, user name.
	Label *string
}

// Resolve normalises an attribution for storage: a nil or empty origin becomes
// Manual, surrounding whitespace is trimmed, a blank id/label becomes nil, the
// label is capped at MaxLabelRunes, and an unknown origin or a non-UUID id is
// rejected rather than stored. A typo must not hide a branch from a filter, and
// a malformed id must be a client error, not a database error.
func Resolve(a *Attribution) (*Attribution, error) {
	if a == nil {
		return &Attribution{Origin: Manual}, nil
	}
	origin := strings.TrimSpace(a.Origin)
	if origin == "" {
		origin = Manual
	}
	switch origin {
	case Extraction, Agent, Manual, System:
	default:
		return nil, ErrInvalidOrigin
	}

	id := trim(a.ID)
	var parsedID uuid.UUID
	if id != nil {
		parsed, err := uuid.Parse(*id)
		if err != nil {
			return nil, ErrInvalidID
		}
		canonical := parsed.String()
		id = &canonical
		parsedID = parsed
	}

	return &Attribution{
		Origin:   origin,
		ID:       id,
		ParsedID: parsedID,
		Label:    trimLabel(a.Label, MaxLabelRunes),
	}, nil
}

// Valid reports whether origin is one of the four stored values.
func Valid(origin string) bool {
	switch origin {
	case Extraction, Agent, Manual, System:
		return true
	}
	return false
}

// trim returns nil for a nil or blank value, so the column stores NULL rather
// than ”.
func trim(v *string) *string {
	if v == nil {
		return nil
	}
	s := strings.TrimSpace(*v)
	if s == "" {
		return nil
	}
	return &s
}

// trimLabel trims and truncates to max runes. It truncates by rune, so a
// multibyte filename cannot be cut mid-character.
func trimLabel(v *string, max int) *string {
	s := trim(v)
	if s == nil {
		return nil
	}
	runes := []rune(*s)
	if len(runes) <= max {
		return s
	}
	cut := string(runes[:max])
	return &cut
}
