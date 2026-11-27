package main

import (
	"context"
	"net/http"
	"net/url"
)

// --- built-in catalog client (GET/POST /api/blueprints/catalog*) ---

// CatalogUI is a type's declared icon/color accent.
type CatalogUI struct {
	Icon  string `json:"icon"`
	Color string `json:"color"`
}

func (u *CatalogUI) icon() string {
	if u == nil {
		return ""
	}
	return u.Icon
}

func (u *CatalogUI) color() string {
	if u == nil {
		return ""
	}
	return u.Color
}

// CatalogSummary is one card in the catalog browser.
type CatalogSummary struct {
	Name        string     `json:"name"`
	Label       string     `json:"label"`
	Description string     `json:"description"`
	Category    string     `json:"category"`
	Parent      string     `json:"parent"`
	Labels      []string   `json:"labels"`
	UI          *CatalogUI `json:"ui"`
}

// CatalogCategoryCount is one browse facet.
type CatalogCategoryCount struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

// CatalogList is one catalog browse page.
type CatalogList struct {
	Version    string                 `json:"version"`
	Total      int                    `json:"total"`
	Limit      int                    `json:"limit"`
	Offset     int                    `json:"offset"`
	Categories []CatalogCategoryCount `json:"categories"`
	Types      []CatalogSummary       `json:"types"`
}

// CatalogProperty is one catalog type property definition.
type CatalogProperty struct {
	Type        string   `json:"type"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Enum        []string `json:"enum"`
}

// CatalogType is the full type returned by the detail endpoint.
type CatalogType struct {
	Name        string                     `json:"name"`
	Label       string                     `json:"label"`
	Description string                     `json:"description"`
	Category    string                     `json:"category"`
	Parent      string                     `json:"parent"`
	Labels      []string                   `json:"labels"`
	UI          *CatalogUI                 `json:"ui"`
	Properties  map[string]CatalogProperty `json:"properties"`
	Required    []string                   `json:"required"`
}

// CatalogRelationship is one candidate edge touching a catalog type.
type CatalogRelationship struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Description string `json:"description"`
	SourceType  string `json:"sourceType"`
	TargetType  string `json:"targetType"`
}

// CatalogTypeDetail is one type with hierarchy and related edges.
type CatalogTypeDetail struct {
	Type                 CatalogType           `json:"type"`
	Ancestors            []string              `json:"ancestors"`
	Children             []string              `json:"children"`
	RelatedRelationships []CatalogRelationship `json:"relatedRelationships"`
}

// CatalogInstallResult mirrors the install endpoint's response.
type CatalogInstallResult struct {
	PackID        string   `json:"packId"`
	Created       bool     `json:"created"`
	Types         []string `json:"types"`
	Relationships []string `json:"relationships"`
	Deferred      []string `json:"deferred"`
	Removed       []string `json:"removed"`
	Merged        []string `json:"merged"`
	Unknown       []string `json:"unknown"`
	Problems      []string `json:"problems"`
}

// ListCatalog browses the built-in catalog.
func (m *MemoryClient) ListCatalog(ctx context.Context, q, category, parent string) (*CatalogList, error) {
	vals := url.Values{}
	if q != "" {
		vals.Set("q", q)
	}
	if category != "" {
		vals.Set("category", category)
	}
	if parent != "" {
		vals.Set("parent", parent)
	}
	path := "/api/blueprints/catalog"
	if enc := vals.Encode(); enc != "" {
		path += "?" + enc
	}
	var out CatalogList
	if err := m.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetCatalogType fetches one catalog type's detail.
func (m *MemoryClient) GetCatalogType(ctx context.Context, name string) (*CatalogTypeDetail, error) {
	var out CatalogTypeDetail
	path := "/api/blueprints/catalog/type?name=" + url.QueryEscape(name)
	if err := m.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// InstallCatalog installs catalog types into the current project. extend names
// requested types the caller accepts merging into a type another pack already
// provides; the server refuses such a name otherwise.
func (m *MemoryClient) InstallCatalog(ctx context.Context, types []string, replace bool, extend []string) (*CatalogInstallResult, error) {
	var out CatalogInstallResult
	body := map[string]any{"types": types, "replace": replace}
	if len(extend) > 0 {
		body["extend"] = extend
	}
	if err := m.do(ctx, http.MethodPost, "/api/blueprints/catalog/install", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
