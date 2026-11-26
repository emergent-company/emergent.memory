package main

import (
	"strings"
)

// --- object prose -------------------------------------------------------------
//
// An object's page leads with its prose: a lead paragraph and the long-form body
// rendered as markdown. The prose comes from the object's own properties, so a
// type needs no new field to benefit — it only has to name one of the
// conventional keys.
//
// The precedence is deliberate: the lead is the field an author wrote *for* a
// summary, and the body is the field they wrote *for* long-form text. A type that
// declares both (summary + content) gets a lead and a body with no duplication; a
// type that declares only one gets the first paragraph as the lead and the rest
// as the body.

// objectArticleProse is the readable form of an object's prose properties.
type objectArticleProse struct {
	// Lead is the opening paragraph as plain text ("" when the object has no
	// prose at all).
	Lead string
	// BodyHTML is the long-form field rendered as sanitized markdown.
	BodyHTML string
	// LeadKey and BodyKey name the properties the prose consumed. The details
	// rail skips them: repeating the page's own text in the rail makes the page
	// twice as long and teaches the reader nothing.
	LeadKey string
	BodyKey string
}

// objectArticleLeadKeys and objectArticleBodyKeys are the two precedence lists.
// They are separate because the same key must not produce both a lead and a body:
// a summary is a lead, content is a body.
//
// description appears in both lists on purpose: it is the most common prose field
// in a schema that declares nothing else, so an object whose only prose is
// description gets its first paragraph as the lead and the rest as the body (the
// duplicate opening is removed below), while an object that also has a summary
// keeps the summary as the lead and reads the description as the body. It is last
// in the body list so an explicit long-form field always wins.
var (
	objectArticleLeadKeys = []string{"summary", "description", "excerpt", "abstract"}
	objectArticleBodyKeys = []string{"content", "body", "notes", "description"}
)

// objectArticleFor resolves an object's prose into the article's parts.
//
// The lead is the opening paragraph of the lead property; when the object has no
// lead property, the opening paragraph of its body property is promoted instead.
// Whatever the lead property holds beyond that opening paragraph is body text, so
// an object whose only prose field is `summary` (or `excerpt`, `abstract`) keeps
// its second paragraph rather than losing it to the lead's first line. A wiki page
// that repeats its opening line in larger type reads like a bug, so a body that
// opens with the lead's own paragraph has that paragraph removed.
func objectArticleFor(obj GraphObject, propDefs []objectPropertyDef) objectArticleProse {
	leadKey, leadRaw, _ := firstProseProperty(obj.Properties, objectArticleLeadKeys)
	// An explicit `format: markdown` property is the schema's statement of which
	// field holds the document, so it beats the conventional key vocabulary. The
	// heuristics still apply when the schema declares none, which keeps objects
	// whose types predate the format working unchanged.
	bodyKey, bodyRaw, _ := firstMarkdownBodyProperty(obj.Properties, propDefs, leadKey)
	if bodyKey == "" {
		bodyKey, bodyRaw, _ = firstProseProperty(obj.Properties, objectArticleBodyKeys)
	}

	if leadRaw == "" && bodyRaw == "" {
		return objectArticleProse{}
	}

	// One property cannot supply both halves: when both lists resolve to the same
	// stored key (description is in both), that property is the whole document and
	// the body comes from what follows its opening paragraph.
	if leadRaw != "" && bodyKey == leadKey {
		bodyRaw = ""
	}

	leadSource := leadRaw
	if strings.TrimSpace(leadSource) == "" {
		leadSource, bodyRaw = bodyRaw, ""
	}

	head, leadRest := splitFirstParagraph(leadSource)
	prose := objectArticleProse{LeadKey: leadKey, BodyKey: bodyKey, Lead: flattenProseParagraph(head)}

	// The same document resolved under two keys (a summary and a content field
	// holding the same text) must not be rendered twice.
	if strings.TrimSpace(bodyRaw) == strings.TrimSpace(leadSource) {
		bodyRaw = ""
	}
	if leadRest != "" {
		bodyRaw = strings.TrimSpace(leadRest + "\n\n" + bodyRaw)
	}
	if bodyRaw != "" {
		if bHead, bRest := splitFirstParagraph(bodyRaw); strings.TrimSpace(bHead) == strings.TrimSpace(head) || flattenProseParagraph(bHead) == prose.Lead {
			bodyRaw = bRest
		}
	}

	prose.BodyHTML = renderMarkdown(bodyRaw)
	return prose
}

// firstMarkdownBodyProperty returns the first declared `format: markdown`
// property that holds prose, in the schema's deterministic name order, skipping
// the property already consumed as the lead. Matching tolerates the case and
// separator spellings prosePropertyNamed accepts.
func firstMarkdownBodyProperty(props map[string]any, propDefs []objectPropertyDef, leadKey string) (key, value string, ok bool) {
	for _, def := range propDefs {
		if !def.Markdown() || (leadKey != "" && strings.EqualFold(def.Name, leadKey)) {
			continue
		}
		name, s, found := prosePropertyNamed(props, def.Name)
		if !found || strings.TrimSpace(s) == "" {
			continue
		}
		return name, s, true
	}
	return "", "", false
}

// firstProseProperty returns the first non-empty string property in keys, in
// order, matched case- and separator-insensitively, along with the property key
// it resolved to (empty when none matched).
func firstProseProperty(props map[string]any, keys []string) (key, value string, ok bool) {
	for _, want := range keys {
		name, s, found := prosePropertyNamed(props, want)
		if !found || strings.TrimSpace(s) == "" {
			continue
		}
		return name, s, true
	}
	return "", "", false
}

// splitFirstParagraph splits markdown into its opening paragraph and the rest.
// A body with no blank line is a single paragraph, so the rest is empty and the
// page shows the lead alone rather than the same text twice.
func splitFirstParagraph(md string) (first, rest string) {
	md = strings.ReplaceAll(md, "\r\n", "\n")
	trimmed := strings.TrimLeft(md, "\n")
	if i := strings.Index(trimmed, "\n\n"); i >= 0 {
		return strings.TrimSpace(trimmed[:i]), strings.TrimSpace(trimmed[i+2:])
	}
	return strings.TrimSpace(trimmed), ""
}
