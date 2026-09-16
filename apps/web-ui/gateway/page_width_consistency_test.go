package main

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pageContainerClass is the class layout.Container renders for
// layout.ContainerLG: centered, the shared responsive gutter, and max-w-6xl.
// Every page's outermost element must carry exactly this.
const pageContainerClass = "mx-auto p-6 lg:p-8 max-w-6xl"

// TestPageWidthConsistency guards the page content-width rule: every page
// renders inside layout.Container(layout.ContainerLG, …), never the narrower
// ContainerMD, and never its own ad-hoc centered wrapper. Mixed widths made
// pages like /agents (max-w-6xl) and /objects (max-w-4xl) visibly inconsistent.
//
// Only hand-written *.templ sources are scanned — generated *_templ.go files
// mirror them and are skipped by the glob.
//
// Out of scope on purpose: modal/dialog/popover widths and inner
// text/card widths (max-w-xs/sm/md/lg/2xl) and any non-centered width. The
// rule is about *page* content width, i.e. a centered `mx-auto … max-w-…`
// wrapper, which must come from layout.Container rather than be hand-rolled.
//
// max-w-6xl is guarded alongside the narrower widths *because* it is exactly
// layout.ContainerLG's width: a hand-rolled `mx-auto … max-w-6xl` duplicates
// the container instead of reusing it and silently drifts if ContainerLG ever
// changes (the composer/banner did precisely this before review). A bare
// max-w-6xl without mx-auto — e.g. a `modal-box max-w-6xl` — is not a page
// wrapper and is not flagged.
//
// A line that genuinely needs its own centered wrapper opts out with a
// `page-width-exempt: <reason>` comment so the exception is explicit.
func TestPageWidthConsistency(t *testing.T) {
	files, err := filepath.Glob("*.templ")
	if err != nil {
		t.Fatalf("glob *.templ: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no *.templ sources found in gateway root")
	}

	guardedWidths := []string{"max-w-3xl", "max-w-4xl", "max-w-5xl", "max-w-6xl", "max-w-7xl"}
	for _, file := range files {
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		sc := bufio.NewScanner(bytes.NewReader(b))
		for lineNo := 1; sc.Scan(); lineNo++ {
			line := sc.Text()
			if strings.Contains(line, "layout.ContainerMD") {
				t.Errorf("%s:%d uses layout.ContainerMD — every page must use layout.ContainerLG for a consistent width", file, lineNo)
			}
			if !strings.Contains(line, "mx-auto") || strings.Contains(line, "page-width-exempt:") {
				continue
			}
			for _, w := range guardedWidths {
				if strings.Contains(line, w) {
					t.Errorf("%s:%d hardcodes a page-level %q wrapper instead of layout.Container: %s", file, lineNo, w, strings.TrimSpace(line))
				}
			}
		}
		if err := sc.Err(); err != nil {
			t.Fatalf("scan %s: %v", file, err)
		}
	}
}

// firstClass returns the value of the first class attribute in html. For a page
// component that is layout.Container's own class, since the container is the
// outermost element the component renders.
func firstClass(html string) string {
	const prefix = `class="`
	i := strings.Index(html, prefix)
	if i < 0 {
		return ""
	}
	rest := html[i+len(prefix):]
	j := strings.IndexByte(rest, '"')
	if j < 0 {
		return ""
	}
	return rest[:j]
}

// TestAgentsAndObjectsSharePageContainer covers the reported bug directly:
// /agents and /objects must render the same outermost page container class
// (width + gutter), so the two pages no longer disagree on content width.
func TestAgentsAndObjectsSharePageContainer(t *testing.T) {
	agentsHTML := renderHTML(t, AgentsPage(
		[]AgentDefinitionSummary{{ID: "a1", Name: "diane"}},
		nil, nil, nil,
	))
	objectsHTML := renderHTML(t, ObjectsPage(
		[]GraphObject{{ID: "o1", Type: "person"}},
		[]string{"person"}, "", nil, "", nil, nil,
	))

	agentsClass := firstClass(agentsHTML)
	objectsClass := firstClass(objectsHTML)

	if agentsClass != pageContainerClass {
		t.Errorf("/agents page container = %q, want %q", agentsClass, pageContainerClass)
	}
	if objectsClass != pageContainerClass {
		t.Errorf("/objects page container = %q, want %q", objectsClass, pageContainerClass)
	}
	if agentsClass != objectsClass {
		t.Errorf("page container width differs: /agents=%q /objects=%q", agentsClass, objectsClass)
	}
}

// TestChatWorkspaceKeepsScrollFillAndSharedContainer covers the riskiest part
// of the width unification: the chat workspace's transcript column must use the
// shared ContainerLG wrapper (same width/gutter as every other page) without
// losing its full-height flex wrapper (min-h-full + justify-end keeps a short
// transcript resting on the composer) or the htmx targets chat.js drives.
func TestChatWorkspaceKeepsScrollFillAndSharedContainer(t *testing.T) {
	html := renderHTML(t, ChatPage(
		[]AgentDefinitionSummary{{ID: "a1", Name: "diane"}},
		nil, nil, nil, "", "", "", nil, false, nil,
	))

	const scrollFill = `class="flex min-h-full flex-col justify-end"`
	if !strings.Contains(html, scrollFill) {
		t.Errorf("chat transcript lost its scroll-fill wrapper (%s)", scrollFill)
	}
	if !strings.Contains(html, `"`+pageContainerClass+`"`) {
		t.Errorf("chat transcript column must use the shared container %q", pageContainerClass)
	}
	if strings.Contains(html, "max-w-3xl") {
		t.Error("chat page must not hardcode the narrower max-w-3xl width")
	}
	for _, id := range []string{`id="chat-log"`, `id="chat-messages"`, `id="chat-form"`, `id="chat-input"`} {
		if !strings.Contains(html, id) {
			t.Errorf("chat workspace target %s missing after the wrapper change", id)
		}
	}

	// The composer reuses the very same Container as the transcript column
	// rather than repeating its width/gutter classes, so the two cannot drift:
	// the form itself carries no width/padding and its input sits inside a real
	// layout.Container.
	const formTag = `<form id="chat-form" hx-boost="false">`
	formStart := strings.Index(html, formTag)
	if formStart < 0 {
		t.Error("chat composer form must carry no width/padding of its own (want the bare form tag)")
	} else {
		textareaStart := strings.Index(html[formStart:], "<textarea")
		if textareaStart < 0 {
			t.Fatal("chat composer textarea missing")
		}
		composerHead := html[formStart : formStart+textareaStart]
		if !strings.Contains(composerHead, `class="`+pageContainerClass+`"`) {
			t.Errorf("chat composer input must sit inside the shared container %q", pageContainerClass)
		}
	}

	// Same for the model-warning banner: its width/gutter come from the shared
	// container, not from the alert's own classes.
	if i := strings.Index(html, `id="chat-model-warning"`); i >= 0 {
		before := html[:i]
		j := strings.LastIndex(before, `<div class="`)
		if j < 0 || !strings.Contains(before[j:], pageContainerClass) {
			t.Error("chat model-warning banner must sit inside the shared container")
		}
	}
}
