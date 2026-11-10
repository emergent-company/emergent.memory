package sources

import (
	"fmt"
	"strings"
	"time"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// gmailFilter is the structured mailbox-scope filter. Every group is optional;
// groups are ANDed and values within a group are ORed when compiled to a Gmail
// search expression.
type gmailFilter struct {
	Categories      []string `json:"categories,omitempty"`
	Labels          []string `json:"labels,omitempty"`
	ExcludeLabels   []string `json:"excludeLabels,omitempty"`
	Senders         []string `json:"senders,omitempty"`
	ExcludeSenders  []string `json:"excludeSenders,omitempty"`
	Domains         []string `json:"domains,omitempty"`
	ExcludeDomains  []string `json:"excludeDomains,omitempty"`
	SubjectContains []string `json:"subjectContains,omitempty"`
	HasAttachment   bool     `json:"hasAttachment,omitempty"`
	UnreadOnly      bool     `json:"unreadOnly,omitempty"`
	StarredOnly     bool     `json:"starredOnly,omitempty"`
	After           string   `json:"after,omitempty"`
	Before          string   `json:"before,omitempty"`
	Query           string   `json:"query,omitempty"`
}

// gmailCategoryNames maps the user-facing category names to Gmail's label ids.
var gmailCategoryNames = map[string]string{
	"primary":    "CATEGORY_PERSONAL",
	"social":     "CATEGORY_SOCIAL",
	"promotions": "CATEGORY_PROMOTIONS",
	"updates":    "CATEGORY_UPDATES",
	"forums":     "CATEGORY_FORUMS",
}

// parseGmailFilter decodes the nested `filter` config object. Unknown keys are
// ignored by decoding into the typed shape through a small explicit reader.
func parseGmailFilter(v any) (gmailFilter, error) {
	var f gmailFilter
	if v == nil {
		return f, nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return f, apperror.NewValidation("config.filter must be an object")
	}
	var err error
	if f.Categories, err = filterStringList(m, "categories"); err != nil {
		return f, err
	}
	for _, cat := range f.Categories {
		if _, ok := gmailCategoryNames[strings.ToLower(cat)]; !ok {
			return f, apperror.NewValidation("config.filter.categories contains unknown category: " + cat)
		}
	}
	if f.Labels, err = filterStringList(m, "labels"); err != nil {
		return f, err
	}
	if f.ExcludeLabels, err = filterStringList(m, "excludeLabels"); err != nil {
		return f, err
	}
	if f.Senders, err = filterStringList(m, "senders"); err != nil {
		return f, err
	}
	if f.ExcludeSenders, err = filterStringList(m, "excludeSenders"); err != nil {
		return f, err
	}
	if f.Domains, err = filterStringList(m, "domains"); err != nil {
		return f, err
	}
	if f.ExcludeDomains, err = filterStringList(m, "excludeDomains"); err != nil {
		return f, err
	}
	if f.SubjectContains, err = filterStringList(m, "subjectContains"); err != nil {
		return f, err
	}
	if f.HasAttachment, err = filterBool(m, "hasAttachment"); err != nil {
		return f, err
	}
	if f.UnreadOnly, err = filterBool(m, "unreadOnly"); err != nil {
		return f, err
	}
	if f.StarredOnly, err = filterBool(m, "starredOnly"); err != nil {
		return f, err
	}
	if f.After, err = filterDate(m, "after"); err != nil {
		return f, err
	}
	if f.Before, err = filterDate(m, "before"); err != nil {
		return f, err
	}
	if f.Query, err = filterString(m, "query"); err != nil {
		return f, err
	}
	return f, nil
}

func (f gmailFilter) empty() bool {
	return len(f.Categories) == 0 && len(f.Labels) == 0 && len(f.ExcludeLabels) == 0 &&
		len(f.Senders) == 0 && len(f.ExcludeSenders) == 0 && len(f.Domains) == 0 &&
		len(f.ExcludeDomains) == 0 && len(f.SubjectContains) == 0 && !f.HasAttachment &&
		!f.UnreadOnly && !f.StarredOnly && f.After == "" && f.Before == "" && strings.TrimSpace(f.Query) == ""
}

// compile folds the filter into one Gmail search expression.
func (f gmailFilter) compile() (string, error) {
	if f.empty() {
		return "", nil
	}
	parts := make([]string, 0, 12)
	if len(f.Categories) > 0 {
		// Gmail's category: operator accepts one category; OR the selected ones.
		terms := make([]string, 0, len(f.Categories))
		for _, cat := range f.Categories {
			terms = append(terms, "category:"+strings.ToLower(strings.TrimSpace(cat)))
		}
		parts = append(parts, "("+strings.Join(terms, " OR ")+")")
	}
	for _, l := range f.Labels {
		parts = append(parts, "label:"+gmailQuote(l))
	}
	for _, l := range f.ExcludeLabels {
		parts = append(parts, "-label:"+gmailQuote(l))
	}
	if len(f.Senders) > 0 {
		parts = append(parts, "from:("+gmailOrList(f.Senders)+")")
	}
	if len(f.ExcludeSenders) > 0 {
		parts = append(parts, "-from:("+gmailOrList(f.ExcludeSenders)+")")
	}
	if len(f.Domains) > 0 {
		parts = append(parts, "from:("+gmailOrList(f.Domains)+")")
	}
	if len(f.ExcludeDomains) > 0 {
		parts = append(parts, "-from:("+gmailOrList(f.ExcludeDomains)+")")
	}
	if len(f.SubjectContains) > 0 {
		parts = append(parts, "subject:("+gmailOrList(f.SubjectContains)+")")
	}
	if f.HasAttachment {
		parts = append(parts, "has:attachment")
	}
	if f.UnreadOnly {
		parts = append(parts, "is:unread")
	}
	if f.StarredOnly {
		parts = append(parts, "is:starred")
	}
	if f.After != "" {
		parts = append(parts, "after:"+gmailDateTerm(f.After))
	}
	if f.Before != "" {
		parts = append(parts, "before:"+gmailDateTerm(f.Before))
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		parts = append(parts, q)
	}
	return strings.Join(parts, " "), nil
}

func gmailOrList(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			quoted = append(quoted, gmailQuote(s))
		}
	}
	return strings.Join(quoted, " OR ")
}

func gmailQuote(v string) string {
	s := strings.TrimSpace(v)
	if s == "" {
		return `""`
	}
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

func gmailDateTerm(v string) string {
	s := strings.TrimSpace(v)
	for _, layout := range []string{"2006-01-02", time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC().Format("2006/01/02")
		}
	}
	return s
}

func filterString(m map[string]any, key string) (string, error) {
	v, ok := m[key]
	if !ok || v == nil {
		return "", nil
	}
	s, ok := v.(string)
	if !ok {
		return "", apperror.NewValidation("config.filter." + key + " must be a string")
	}
	return strings.TrimSpace(s), nil
}

func filterStringList(m map[string]any, key string) ([]string, error) {
	v, ok := m[key]
	if !ok || v == nil {
		return nil, nil
	}
	out, err := stringList(v)
	if err != nil {
		return nil, apperror.NewValidation("config.filter." + key + " must be an array of non-empty strings")
	}
	return out, nil
}

func filterBool(m map[string]any, key string) (bool, error) {
	v, ok := m[key]
	if !ok || v == nil {
		return false, nil
	}
	b, ok := v.(bool)
	if !ok {
		return false, apperror.NewValidation("config.filter." + key + " must be a boolean")
	}
	return b, nil
}

func filterDate(m map[string]any, key string) (string, error) {
	s, err := filterString(m, key)
	if err != nil {
		return "", err
	}
	if s == "" {
		return "", nil
	}
	for _, layout := range []string{"2006-01-02", time.RFC3339} {
		if _, err := time.Parse(layout, s); err == nil {
			return s, nil
		}
	}
	return "", apperror.NewValidation("config.filter." + key + " must be a date (YYYY-MM-DD)")
}

func (f gmailFilter) String() string {
	q, err := f.compile()
	if err != nil {
		return fmt.Sprintf("invalid filter: %v", err)
	}
	return q
}

// FilterPreviewProvider is implemented by providers that can validate and
// preview an unsaved sync scope.
type FilterPreviewProvider interface {
	FilterPreview(cfg map[string]any) (string, error)
}

// FilterPreview returns the compiled Gmail query for an unsaved config.
func (p *gmailProvider) FilterPreview(cfg map[string]any) (string, error) {
	parsed, err := parseGmailConfig(cfg)
	if err != nil {
		return "", err
	}
	return parsed.Query, nil
}
