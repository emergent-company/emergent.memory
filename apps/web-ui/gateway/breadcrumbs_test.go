package main

import (
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/emergent-company/go-daisy/components/nav"
	"golang.org/x/net/html"
)

// TestCrumbsActiveStripsTrailingHref pins the invariant behind every detail
// trail: the current page is active styling but never a link. nav.Crumbs gives
// the final item an href only when a caller passes the page's own URL, and
// leaving it clickable while marked current renders an "active link" that no
// other trail has.
func TestCrumbsActiveStripsTrailingHref(t *testing.T) {
	got := crumbsActive(nav.Crumbs("Agents", "/agents", "diane", "/agents/a1"))
	if len(got) != 2 {
		t.Fatalf("crumb count = %d, want 2", len(got))
	}
	if got[0].Href != "/agents" {
		t.Errorf("ancestor href = %q, want /agents", got[0].Href)
	}
	if !got[1].Active {
		t.Error("trailing crumb is not active")
	}
	if got[1].Href != "" {
		t.Errorf("trailing crumb href = %q, want empty (current page is not a link)", got[1].Href)
	}

	// A label-only trailing item is unaffected apart from being marked active.
	got = crumbsActive(nav.Crumbs("Skills", "/skills", "New skill"))
	if got[1].Href != "" || !got[1].Active {
		t.Errorf("label-only trailing crumb = %+v, want active without href", got[1])
	}
}

// TestOrgCrumbs covers both shapes: the org landing (org is current) and org
// sub-pages (org links to its landing, final leaf is current).
func TestOrgCrumbs(t *testing.T) {
	landing := orgCrumbs("org_1", "Acme")
	if len(landing) != 2 {
		t.Fatalf("landing crumb count = %d, want 2", len(landing))
	}
	if landing[0].Label != "Organizations" || landing[0].Href != "/orgs" {
		t.Errorf("landing root = %+v, want Organizations -> /orgs", landing[0])
	}
	if landing[1].Label != "Acme" || !landing[1].Active || landing[1].Href != "" {
		t.Errorf("landing current = %+v, want active Acme without href", landing[1])
	}

	members := orgCrumbs("org_1", "Acme", "Members")
	if len(members) != 3 {
		t.Fatalf("members crumb count = %d, want 3", len(members))
	}
	if members[1].Label != "Acme" || members[1].Href != "/orgs/org_1" {
		t.Errorf("org ancestor = %+v, want Acme -> /orgs/org_1", members[1])
	}
	if members[2].Label != "Members" || !members[2].Active || members[2].Href != "" {
		t.Errorf("members current = %+v, want active Members without href", members[2])
	}

	danger := orgCrumbs("org_1", "Acme", "Settings", "/orgs/org_1/settings", "Danger zone")
	if len(danger) != 4 {
		t.Fatalf("danger crumb count = %d, want 4", len(danger))
	}
	if danger[2].Label != "Settings" || danger[2].Href != "/orgs/org_1/settings" {
		t.Errorf("settings ancestor = %+v, want Settings -> /orgs/org_1/settings", danger[2])
	}
	if danger[3].Label != "Danger zone" || !danger[3].Active || danger[3].Href != "" {
		t.Errorf("danger current = %+v, want active Danger zone without href", danger[3])
	}
}

// TestRunCrumbTrailEndsAtSchedule pins the run page's object-origin shape: the
// trail stops at the parent schedule, the run identity stays in the page
// header/metadata, and no crumb claims the current page.
func TestRunCrumbTrailEndsAtSchedule(t *testing.T) {
	got := runCrumbTrail(&ScheduledAgentRun{AgentName: "nightly digest", AgentID: "sched_1"})
	if len(got) != 2 {
		t.Fatalf("crumb count = %d, want 2", len(got))
	}
	if got[0].Label != "Schedules" || got[0].Href != "/schedules" {
		t.Errorf("root = %+v, want Schedules -> /schedules", got[0])
	}
	if got[1].Label != "nightly digest" || got[1].Href != "/schedules/sched_1" {
		t.Errorf("schedule = %+v, want linked schedule", got[1])
	}
	for i, c := range got {
		if c.Active {
			t.Errorf("crumb %d (%q) is active; the run is the current page and is not a crumb", i, c.Label)
		}
	}

	// Without a schedule name the trail still stops at the schedules index.
	got = runCrumbTrail(&ScheduledAgentRun{})
	if len(got) != 1 || got[0].Label != "Schedules" {
		t.Fatalf("empty run trail = %+v, want just Schedules", got)
	}
}

// TestMemberCrumbsCurrentIsNotLink covers the members section and invite page:
// the trailing item is active and non-navigable on both surfaces.
func TestMemberCrumbsCurrentIsNotLink(t *testing.T) {
	for _, base := range []string{"/settings/members", "/members"} {
		got := memberDetailCrumbs(base)
		last := got[len(got)-1]
		if last.Label != "Members" || !last.Active || last.Href != "" {
			t.Errorf("memberDetailCrumbs(%q) last = %+v, want active Members without href", base, last)
		}
	}

	got := memberInviteCrumbs("/settings/members")
	last := got[len(got)-1]
	if last.Label != "Invite a member" || !last.Active || last.Href != "" {
		t.Errorf("memberInviteCrumbs last = %+v, want active Invite a member without href", last)
	}
}

// TestConvertedPagesRenderDetailTrails covers the pages that used the list-page
// header (or an incomplete trail) before the breadcrumb sweep. Each renders a
// trail from its section root to a single active, non-navigable current item.
func TestConvertedPagesRenderDetailTrails(t *testing.T) {
	cases := []struct {
		name     string
		page     templ.Component
		rootHref string
		current  string
	}{
		{"backup detail", backupDetailHeader(&Backup{ProjectName: "Acme"}), "/backups", "Acme"},
		{"notification settings", NotificationPreferencesPage(notificationPrefsView{}), "/inbox", "Notification settings"},
		{"connect a data source", DataSourceNewPage(nil, nil, nil, nil, nil, nil, nil), "/data-sources", "Connect a data source"},
		{"blueprint model map", BlueprintImportModelMapPage(BlueprintModelMapData{}), "/blueprints", "Review blueprint models"},
		{"credentials", CredentialsPage(dsCredentialsPageData{}), "/settings", "Credentials"},
		{"org landing", OrgLandingPage(orgPageData{Org: &Org{ID: "org_1", Name: "Acme"}}), "/orgs", "Acme"},
		{"org members", OrgMembersPage(orgMembersPageData{Org: &Org{ID: "org_1", Name: "Acme"}}), "/orgs", "Members"},
		{"org invite", OrgInvitePage(orgInvitePageData{Org: &Org{ID: "org_1", Name: "Acme"}}), "/orgs", "Invite a member"},
		{"org danger zone", OrgSettingsDangerZonePage(orgSettingsDangerZonePageData{Org: &Org{ID: "org_1", Name: "Acme"}}), "/orgs", "Danger zone"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			items := breadcrumbItems(t, renderHTML(t, tc.page))
			if len(items) < 2 {
				t.Fatalf("breadcrumb item count = %d, want at least 2", len(items))
			}

			root := findElementByTag(items[0], "a")
			if root == nil {
				t.Fatalf("root crumb is not a link: %s", breadcrumbText(items[0]))
			}
			if got := attrValue(root, "href"); got != tc.rootHref {
				t.Errorf("root href = %q, want %q", got, tc.rootHref)
			}

			last := items[len(items)-1]
			if findElementByTag(last, "a") != nil {
				t.Errorf("current crumb %q is a link; the current page must not be navigable", breadcrumbText(last))
			}
			if got := breadcrumbText(last); got != tc.current {
				t.Errorf("current crumb = %q, want %q", got, tc.current)
			}
			if cls := attrValue(last, "class"); !strings.Contains(cls, "font-medium") {
				t.Errorf("current crumb class = %q, want active styling", cls)
			}
		})
	}
}

func breadcrumbItems(t *testing.T, rendered string) []*html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(rendered))
	if err != nil {
		t.Fatalf("parse rendered page: %v", err)
	}
	crumbs := findElementByClass(doc, "breadcrumbs")
	if crumbs == nil {
		t.Fatal("no .breadcrumbs element in rendered page")
	}
	list := findElementByTag(crumbs, "ul")
	if list == nil {
		t.Fatal("no breadcrumb list")
	}
	var items []*html.Node
	for n := list.FirstChild; n != nil; n = n.NextSibling {
		if n.Type == html.ElementNode && n.Data == "li" {
			items = append(items, n)
		}
	}
	return items
}

func breadcrumbText(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.TextNode {
			b.WriteString(node.Data)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)
	return strings.TrimSpace(b.String())
}

func attrValue(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}
