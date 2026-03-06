package docs

import "time"

type Frontmatter struct {
	ID          string   `yaml:"id" json:"id"`
	Title       string   `yaml:"title" json:"title"`
	Category    string   `yaml:"category" json:"category"`
	Tags        []string `yaml:"tags" json:"tags"`
	Description string   `yaml:"description" json:"description"`
	LastUpdated string   `yaml:"lastUpdated" json:"lastUpdated"`
	ReadTime    int      `yaml:"readTime" json:"readTime"`
	Related     []string `yaml:"related" json:"related"`
}

type Document struct {
	ID          string    `json:"id"`
	Slug        string    `json:"slug"`
	Title       string    `json:"title"`
	Category    string    `json:"category"`
	Path        string    `json:"path"`
	Description string    `json:"description"`
	Tags        []string  `json:"tags"`
	LastUpdated string    `json:"lastUpdated"`
	ReadTime    int       `json:"readTime"`
	Related     []string  `json:"related"`
	Content     string    `json:"content"`
	ParsedAt    time.Time `json:"parsedAt"`
}

type DocumentMeta struct {
	ID          string   `json:"id"`
	Slug        string   `json:"slug"`
	Title       string   `json:"title"`
	Category    string   `json:"category"`
	Path        string   `json:"path"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	LastUpdated string   `json:"lastUpdated"`
	ReadTime    int      `json:"readTime"`
	Related     []string `json:"related"`
}

type DocumentList struct {
	Documents []DocumentMeta `json:"documents"`
	Total     int            `json:"total"`
}

type CategoryInfo struct {
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description" yaml:"description"`
	Icon        string `json:"icon" yaml:"icon"`
}

type CategoriesResponse struct {
	Categories []CategoryInfo `json:"categories"`
	Total      int            `json:"total"`
}
