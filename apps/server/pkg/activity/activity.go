// Package activity holds the presentation-independent rules for member
// activity indicators: the recency tier shown next to a member and the
// bounded score used to rank members inside a window.
//
// The tier answers "how recently was this person here?", the score answers
// "how much did they do?". They are deliberately separate: a single action
// five minutes ago is Active but low-scoring, while a burst of work two weeks
// ago is Occasional but high-scoring.
package activity

import (
	"math"
	"time"
)

// Tier values reported by the activity API and rendered by the UI.
const (
	TierOnline     = "online"
	TierActive     = "active"
	TierOccasional = "occasional"
	TierDormant    = "dormant"
	TierNever      = "never"
)

// ActiveWindow and OccasionalWindow are the recency cut-offs for the
// non-live tiers. They are the only place the thresholds live.
const (
	ActiveWindow     = 7 * 24 * time.Hour
	OccasionalWindow = 30 * 24 * time.Hour
)

// TierFor classifies a member by recency. online is the live presence signal
// (a heartbeat inside the presence window); it wins over any persisted
// timestamp because presence is strictly fresher. A nil lastSeen with no live
// connection means the member has never been observed.
func TierFor(lastSeen *time.Time, online bool, now time.Time) string {
	if online {
		return TierOnline
	}
	if lastSeen == nil || lastSeen.IsZero() {
		return TierNever
	}
	age := now.Sub(*lastSeen)
	switch {
	case age <= ActiveWindow:
		return TierActive
	case age <= OccasionalWindow:
		return TierOccasional
	default:
		return TierDormant
	}
}

// Score converts a window's aggregate counts into a bounded 0..100 ranking
// value. Actions weigh more than raw requests (a request may be a page load,
// an action is a write/chat/model call), and active days add a retention
// bonus so a steady contributor outranks a single burst.
//
// The curve is saturating rather than linear so one power user cannot flatten
// everybody else to zero.
func Score(requests, actions, activeDays int64) int {
	if requests < 0 {
		requests = 0
	}
	if actions < 0 {
		actions = 0
	}
	if activeDays < 0 {
		activeDays = 0
	}
	points := float64(requests) + 3*float64(actions) + 5*float64(activeDays)
	if points <= 0 {
		return 0
	}
	score := 100 * (1 - math.Exp(-points/100))
	if score > 100 {
		score = 100
	}
	return int(math.Round(score))
}
