package main

import (
	ui "github.com/emergent-company/go-daisy/components/ui"
)

// Activity tier vocabulary shared with the server's pkg/activity package. The
// gateway keeps its own copy of the strings because it must render tiers it
// receives over HTTP; the set is closed and asserted by the handler tests.
const (
	activityTierOnline     = "online"
	activityTierActive     = "active"
	activityTierOccasional = "occasional"
	activityTierDormant    = "dormant"
	activityTierNever      = "never"
)

// activityTierLabel is the human label shown in the member activity badge.
func activityTierLabel(tier string) string {
	switch tier {
	case activityTierOnline:
		return "Online"
	case activityTierActive:
		return "Active"
	case activityTierOccasional:
		return "Occasional"
	case activityTierDormant:
		return "Dormant"
	default:
		return "Never"
	}
}

// activityTierIntent maps a tier to a badge intent: live/recent is positive,
// occasional is a warning, dormant/never is neutral.
func activityTierIntent(tier string) ui.BadgeIntent {
	switch tier {
	case activityTierOnline, activityTierActive:
		return ui.BadgeSuccess
	case activityTierOccasional:
		return ui.BadgeWarning
	default:
		return ui.BadgeNeutral
	}
}

// activityRel renders a member's last-seen relative time, or "Never" when the
// member has no recorded activity.
func activityRel(lastActiveAt *string) string {
	if lastActiveAt == nil || *lastActiveAt == "" {
		return "Never"
	}
	return relTime(*lastActiveAt)
}

// markOnlineOrg overlays live presence onto org members: a member with a fresh
// SSE heartbeat renders as Online with the heartbeat time as last-seen,
// regardless of the persisted tier.
func markOnlineOrg(members []OrgMemberDto, online map[string]string) {
	if len(online) == 0 {
		return
	}
	for i := range members {
		ts, ok := online[members[i].ID]
		if !ok {
			continue
		}
		members[i].ActivityTier = activityTierOnline
		members[i].LastActiveAt = &ts
	}
}

// markOnlineProject is markOnlineOrg for project members.
func markOnlineProject(members []ProjectMemberDto, online map[string]string) {
	if len(online) == 0 {
		return
	}
	for i := range members {
		ts, ok := online[members[i].ID]
		if !ok {
			continue
		}
		members[i].ActivityTier = activityTierOnline
		members[i].LastActiveAt = &ts
	}
}
