package useractivity

import "time"

// MemberActivityRow is one row of the raw activity aggregate used to build
// MemberActivityDTO. It is not the table model: the daily table is keyed by
// day while these rows are grouped per member over a window.
type MemberActivityRow struct {
	UserID       string     `bun:"user_id"`
	Email        string     `bun:"email"`
	DisplayName  *string    `bun:"display_name"`
	FirstName    *string    `bun:"first_name"`
	LastName     *string    `bun:"last_name"`
	Role         string     `bun:"role"`
	JoinedAt     time.Time  `bun:"joined_at"`
	LastSeenAt   *time.Time `bun:"last_seen_at"`
	LastActionAt *time.Time `bun:"last_action_at"`
	RequestCount int64      `bun:"request_count"`
	ActionCount  int64      `bun:"action_count"`
	ActiveDays   int64      `bun:"active_days"`
}

// MemberActivityDTO is one member's activity summary over the requested window.
type MemberActivityDTO struct {
	UserID       string     `json:"userId"`
	Email        string     `json:"email"`
	DisplayName  *string    `json:"displayName,omitempty"`
	FirstName    *string    `json:"firstName,omitempty"`
	LastName     *string    `json:"lastName,omitempty"`
	Role         string     `json:"role"`
	JoinedAt     time.Time  `json:"joinedAt"`
	LastActiveAt *time.Time `json:"lastActiveAt,omitempty"`
	LastActionAt *time.Time `json:"lastActionAt,omitempty"`
	RequestCount int64      `json:"requestCount"`
	ActionCount  int64      `json:"actionCount"`
	ActiveDays   int64      `json:"activeDays"`
	Tier         string     `json:"activityTier"`
	Score        int        `json:"activityScore"`
}

// ActivityResponse is the ranked member activity list for an org or project.
type ActivityResponse struct {
	RangeDays int                 `json:"rangeDays"`
	Members   []MemberActivityDTO `json:"members"`
}

// MyActivityResponse is the authenticated user's own activity summary. It
// never exposes another member's data.
type MyActivityResponse struct {
	RangeDays    int        `json:"rangeDays"`
	LastActiveAt *time.Time `json:"lastActiveAt,omitempty"`
	RequestCount int64      `json:"requestCount"`
	ActionCount  int64      `json:"actionCount"`
	ActiveDays   int64      `json:"activeDays"`
	Tier         string     `json:"activityTier"`
	Score        int        `json:"activityScore"`
}
