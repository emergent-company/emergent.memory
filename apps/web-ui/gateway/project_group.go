package main

// projectGroup is one org's projects for the switcher. OrgID is the org's id;
// OrgName "" and OrgID "" together denote the "Other" (unknown/org-less) group.
type projectGroup struct {
	OrgID    string
	OrgName  string
	Projects []ProjectRef
}

// groupProjectsByOrg returns projects grouped by org in the order orgs are
// provided. A project whose OrgID is empty or not present in orgs lands in a
// trailing "Other" group (OrgID == "", OrgName == "") so no project ever
// disappears from the picker. Orgs with no projects yield a group with no rows
// (the picker renders the org header so project-less organizations stay
// reachable). Empty input yields nil. Order within a group preserves the input
// project order.
func groupProjectsByOrg(projects []ProjectRef, orgs []Org) []projectGroup {
	if len(projects) == 0 {
		return nil
	}
	known := make(map[string]bool, len(orgs))
	for _, o := range orgs {
		known[o.ID] = true
	}

	var out []projectGroup
	for _, o := range orgs {
		var ps []ProjectRef
		for _, p := range projects {
			if p.OrgID == o.ID {
				ps = append(ps, p)
			}
		}
		out = append(out, projectGroup{OrgID: o.ID, OrgName: o.Name, Projects: ps})
	}

	var others []ProjectRef
	for _, p := range projects {
		if p.OrgID == "" || !known[p.OrgID] {
			others = append(others, p)
		}
	}
	if len(others) > 0 {
		out = append(out, projectGroup{Projects: others})
	}

	if len(out) == 0 {
		return nil
	}
	return out
}

// projectGroupLabel is the header text for a group: the org's name, or
// "Other" for the trailing unknown/org-less group.
func projectGroupLabel(g projectGroup) string {
	if g.OrgName != "" {
		return g.OrgName
	}
	return "Other"
}

// orgNameFor resolves an org id to its display name, or "" when the id is
// empty or not present in orgs.
func orgNameFor(orgs []Org, orgID string) string {
	for _, o := range orgs {
		if o.ID == orgID {
			return o.Name
		}
	}
	return ""
}
