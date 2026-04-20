package create

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Context: ctx-<screen-name>
func ContextKey(name string) string {
	return "ctx-" + slugify(name)
}

// UIComponent: ui-<name-slug>
func UIComponentKey(name string) string {
	return "ui-" + slugify(name)
}

// Helper: hook-<name-slug> (strips use/Use prefix)
func HelperKey(name string) string {
	slug := slugify(name)
	slug = strings.TrimPrefix(slug, "use-")
	return "hook-" + slug
}

// Action: act-<domain>-<verb>-<resource> (max 4 tokens after prefix)
func ActionKey(domain, name string) string {
	domainSlug := slugify(domain)
	nameSlug := camelToSlug(name)
	nameParts := strings.Split(nameSlug, "-")
	if len(nameParts) > 3 {
		nameParts = nameParts[:3]
	}
	return "act-" + domainSlug + "-" + strings.Join(nameParts, "-")
}

// APIEndpoint: ep-<verb>-<resource> (kebab handler, drop domain if unique)
func APIEndpointKey(domain, handler string) string {
	handlerSlug := slugify(camelToSlug(handler))
	if domain != "" {
		domainPrefix := slugify(domain) + "-"
		if strings.HasPrefix(handlerSlug, domainPrefix) {
			handlerSlug = strings.TrimPrefix(handlerSlug, domainPrefix)
		}
	}
	return "ep-" + handlerSlug
}

// SourceFile: sf-<app>-<path-no-src-no-ext>
func SourceFileKey(path string) string {
	p := filepath.ToSlash(path)
	p = strings.TrimPrefix(p, "apps/")
	p = strings.TrimPrefix(p, "libs/")
	p = strings.ReplaceAll(p, "/src/", "/")
	ext := filepath.Ext(p)
	p = strings.TrimSuffix(p, ext)

	key := strings.NewReplacer("/", "-", ".", "-", "_", "-").Replace(p)
	key = strings.ToLower(key)
	for strings.Contains(key, "--") {
		key = strings.ReplaceAll(key, "--", "-")
	}
	return "sf-" + strings.Trim(key, "-")
}

// Domain: dom-<slug>
func DomainKey(slug string) string {
	return "dom-" + slugify(slug)
}

// Scenario: scn-<slug>
func ScenarioKey(name string) string {
	return "scn-" + slugify(name)
}

// ScenarioStep: step-<scn-abbrev-3tokens>-<N>
func ScenarioStepKey(scenarioKey string, order int) string {
	// scn-create-meeting -> step-create-meeting-1
	prefix := strings.TrimPrefix(scenarioKey, "scn-")
	parts := strings.Split(prefix, "-")
	if len(parts) > 3 {
		parts = parts[:3]
	}
	return fmt.Sprintf("step-%s-%d", strings.Join(parts, "-"), order)
}

func slugify(s string) string {
	// split camelCase before lowercasing
	s = camelToSlug(s)
	s = strings.ToLower(s)
	s = strings.NewReplacer(" ", "-", "_", "-", ".", "-").Replace(s)
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return strings.Trim(s, "-")
}

func camelToSlug(s string) string {
	// insert hyphen before each uppercase letter that follows a lowercase letter or digit
	var result []rune
	runes := []rune(s)
	for i, r := range runes {
		if i > 0 && r >= 'A' && r <= 'Z' {
			prev := runes[i-1]
			if (prev >= 'a' && prev <= 'z') || (prev >= '0' && prev <= '9') {
				result = append(result, '-')
			}
		}
		result = append(result, r)
	}
	return strings.ToLower(string(result))
}
