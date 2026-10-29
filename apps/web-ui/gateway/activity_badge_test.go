package main

import "testing"

func TestMarkOnlineOrg(t *testing.T) {
	members := []OrgMemberDto{
		{ID: "u1", ActivityTier: activityTierActive},
		{ID: "u2", ActivityTier: activityTierDormant},
	}
	markOnlineOrg(members, map[string]string{"u2": "2026-07-12T12:00:00Z"})

	if members[0].ActivityTier != activityTierActive {
		t.Fatalf("offline member tier changed to %q", members[0].ActivityTier)
	}
	if members[1].ActivityTier != activityTierOnline {
		t.Fatalf("online member tier = %q, want online", members[1].ActivityTier)
	}
	if members[1].LastActiveAt == nil || *members[1].LastActiveAt != "2026-07-12T12:00:00Z" {
		t.Fatalf("online member lastActiveAt = %v", members[1].LastActiveAt)
	}
}

func TestMarkOnlineProject(t *testing.T) {
	members := []ProjectMemberDto{
		{ID: "u1", ActivityTier: activityTierOccasional},
		{ID: "u2"},
	}
	markOnlineProject(members, map[string]string{"u1": "2026-07-12T12:00:00Z"})

	if members[0].ActivityTier != activityTierOnline {
		t.Fatalf("online member tier = %q, want online", members[0].ActivityTier)
	}
	if members[1].ActivityTier != "" {
		t.Fatalf("unseen member tier = %q, want empty", members[1].ActivityTier)
	}
}

func TestMarkOnlineNilMap(t *testing.T) {
	members := []ProjectMemberDto{{ID: "u1", ActivityTier: activityTierActive}}
	markOnlineProject(members, nil)
	if members[0].ActivityTier != activityTierActive {
		t.Fatalf("nil presence map changed tier to %q", members[0].ActivityTier)
	}
}
