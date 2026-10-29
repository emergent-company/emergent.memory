package main

import (
	"sort"
	"strconv"
	"time"
)

// conversationGroup is one rail group: a root conversation plus the sub-chats
// spawned beneath it. The rail renders the group flat (root row, then nested
// rows) because the list is a single scroll container; grouping is a rendering
// decision, not a second list.
type conversationGroup struct {
	Root        Conversation
	Descendants []conversationDescendant
	// Expanded is true when the active conversation is the root or one of its
	// descendants, so opening a sub-chat reveals the branch that owns it.
	Expanded bool
	latest   time.Time
}

// conversationDescendant is one nested row. Depth is its distance from the
// group root (1 = direct sub-chat); the rail uses it for indentation and caps
// the visual depth so a deep spawn chain does not run off the rail.
type conversationDescendant struct {
	Conversation
	Depth int
}

// sessionRailRowOptions carries the optional group decorations for a row. The
// zero value renders an ordinary top-level conversation row, so sessionRailItem
// stays the stable call used by tests and ungrouped rows.
type sessionRailRowOptions struct {
	SubchatRootID    string
	SubchatDepth     int
	SubchatCount     int
	SubchatsExpanded bool
	Collapsed        bool
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func subchatToggleLabel(count int, expanded bool) string {
	noun := "sub-chats"
	if count == 1 {
		noun = "sub-chat"
	}
	if expanded {
		return "Hide " + strconv.Itoa(count) + " " + noun
	}
	return "Show " + strconv.Itoa(count) + " " + noun
}

// groupConversations folds a flat conversation page into root + descendants.
//
// The parent link is persisted by memory, but a conversation whose parent is
// absent from the page (deleted, archived, or simply outside the limit) is
// treated as a root instead of being dropped: a sub-chat must never become
// invisible because its parent is missing. Ordering uses the subtree's newest
// updated_at, because a spawned child is usually newer than the parent that
// stayed idle and the group should rise in the rail when the child runs.
func groupConversations(convs []Conversation, activeID string) []conversationGroup {
	if len(convs) == 0 {
		return nil
	}

	byID := make(map[string]Conversation, len(convs))
	for _, c := range convs {
		byID[c.ID] = c
	}

	type grouping struct {
		root        Conversation
		descendants []conversationDescendant
		latest      time.Time
	}
	groups := make(map[string]*grouping, len(convs))
	order := make([]string, 0, len(convs))

	for _, c := range convs {
		root, depth := conversationRoot(c, byID)
		g, ok := groups[root.ID]
		if !ok {
			g = &grouping{root: root, latest: parseRailTime(root.UpdatedAt)}
			groups[root.ID] = g
			order = append(order, root.ID)
		}
		if depth > 0 {
			g.descendants = append(g.descendants, conversationDescendant{Conversation: c, Depth: depth})
		}
		if t := parseRailTime(c.UpdatedAt); t.After(g.latest) {
			g.latest = t
		}
	}

	out := make([]conversationGroup, 0, len(order))
	for _, rootID := range order {
		g := groups[rootID]
		sort.SliceStable(g.descendants, func(i, j int) bool {
			ti, tj := parseRailTime(g.descendants[i].CreatedAt), parseRailTime(g.descendants[j].CreatedAt)
			if ti.Equal(tj) {
				return g.descendants[i].ID < g.descendants[j].ID
			}
			return ti.Before(tj)
		})
		expanded := activeID != "" && activeID == g.root.ID
		if !expanded {
			for _, d := range g.descendants {
				if d.ID == activeID {
					expanded = true
					break
				}
			}
		}
		out = append(out, conversationGroup{
			Root:        g.root,
			Descendants: g.descendants,
			Expanded:    expanded,
			latest:      g.latest,
		})
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].latest.Equal(out[j].latest) {
			return out[i].Root.ID < out[j].Root.ID
		}
		return out[i].latest.After(out[j].latest)
	})
	return out
}

// conversationRoot walks parent links to the top of the visible chain and
// returns that root plus the conversation's depth. A missing parent ends the
// walk (the current node becomes the root). A corrupt cycle is canonicalised to
// its smallest id, so every member agrees on one root instead of a↔b rendering
// as two groups that both contain each other.
func conversationRoot(c Conversation, byID map[string]Conversation) (Conversation, int) {
	path := []Conversation{c}
	seen := map[string]int{c.ID: 0}
	cur := c
	for cur.ParentConversationID != "" {
		parent, ok := byID[cur.ParentConversationID]
		if !ok {
			break
		}
		if idx, cycle := seen[parent.ID]; cycle {
			root := path[idx]
			rootIdx := idx
			for i := idx + 1; i < len(path); i++ {
				if path[i].ID < root.ID {
					root = path[i]
					rootIdx = i
				}
			}
			return root, rootIdx
		}
		seen[parent.ID] = len(path)
		path = append(path, parent)
		cur = parent
	}
	return cur, len(path) - 1
}

func parseRailTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	return time.Time{}
}
