package docs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

type Service struct {
	logger    *slog.Logger
	baseDir   string
	cache     map[string]*Document
	cacheMu   sync.RWMutex
	indexJSON []byte
}

func NewService(logger *slog.Logger) *Service {
	return &Service{
		logger:  logger,
		baseDir: "docs/public",
		cache:   make(map[string]*Document),
	}
}

func parseFrontmatter(content []byte) (*Frontmatter, string, error) {
	if !bytes.HasPrefix(content, []byte("---\n")) {
		return nil, string(content), nil
	}

	parts := bytes.SplitN(content[4:], []byte("\n---\n"), 2)
	if len(parts) != 2 {
		return nil, string(content), fmt.Errorf("invalid frontmatter: missing closing delimiter")
	}

	var fm Frontmatter
	if err := yaml.Unmarshal(parts[0], &fm); err != nil {
		return nil, string(content), fmt.Errorf("failed to parse frontmatter: %w", err)
	}

	return &fm, string(parts[1]), nil
}

func slugFromPath(path string) string {
	base := filepath.Base(path)
	slug := strings.TrimSuffix(base, filepath.Ext(base))
	return slug
}

func (s *Service) parseDocument(path string) (*Document, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	fm, markdownContent, err := parseFrontmatter(content)
	if err != nil {
		return nil, err
	}

	if fm == nil {
		return nil, fmt.Errorf("document missing frontmatter: %s", path)
	}

	doc := &Document{
		ID:          fm.ID,
		Slug:        slugFromPath(path),
		Title:       fm.Title,
		Category:    fm.Category,
		Path:        path,
		Description: fm.Description,
		Tags:        fm.Tags,
		LastUpdated: fm.LastUpdated,
		ReadTime:    fm.ReadTime,
		Related:     fm.Related,
		Content:     markdownContent,
		ParsedAt:    time.Now(),
	}

	return doc, nil
}

func (s *Service) GetDocument(slug string) (*Document, error) {
	s.cacheMu.RLock()
	if doc, ok := s.cache[slug]; ok {
		s.cacheMu.RUnlock()
		return doc, nil
	}
	s.cacheMu.RUnlock()

	var foundPath string
	err := filepath.Walk(s.baseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".md") {
			return nil
		}
		if slugFromPath(path) == slug {
			foundPath = path
			return filepath.SkipAll
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("error searching for document: %w", err)
	}

	if foundPath == "" {
		return nil, fmt.Errorf("document not found: %s", slug)
	}

	doc, err := s.parseDocument(foundPath)
	if err != nil {
		return nil, err
	}

	s.cacheMu.Lock()
	s.cache[slug] = doc
	s.cacheMu.Unlock()

	return doc, nil
}

func (s *Service) ListDocuments() ([]DocumentMeta, error) {
	var docs []DocumentMeta

	err := filepath.Walk(s.baseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".md") {
			return nil
		}

		doc, err := s.parseDocument(path)
		if err != nil {
			s.logger.Warn("failed to parse document", slog.String("path", path), slog.String("error", err.Error()))
			return nil
		}

		docs = append(docs, DocumentMeta{
			ID:          doc.ID,
			Slug:        doc.Slug,
			Title:       doc.Title,
			Category:    doc.Category,
			Path:        doc.Path,
			Description: doc.Description,
			Tags:        doc.Tags,
			LastUpdated: doc.LastUpdated,
			ReadTime:    doc.ReadTime,
			Related:     doc.Related,
		})

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to walk docs directory: %w", err)
	}

	return docs, nil
}

func (s *Service) GetCategories() ([]CategoryInfo, error) {
	indexPath := filepath.Join(s.baseDir, "index.json")
	content, err := os.ReadFile(indexPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read index.json: %w", err)
	}

	var index struct {
		Categories []CategoryInfo `json:"categories"`
	}
	if err := json.Unmarshal(content, &index); err != nil {
		return nil, fmt.Errorf("failed to parse index.json: %w", err)
	}

	return index.Categories, nil
}

func (s *Service) ClearCache() {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	s.cache = make(map[string]*Document)
	s.logger.Info("documentation cache cleared")
}
