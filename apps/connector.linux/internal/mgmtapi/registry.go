package mgmtapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/mcpregistry"
)

// RegistrySource supplies registry browse data from Memory. It is nil when the
// connector has no project connection; the registry routes then report
// "registry unavailable" rather than failing opaquely.
type RegistrySource interface {
	SearchRegistry(ctx context.Context, query string, limit int, cursor string) (*mcpregistry.RegistrySearchResult, error)
	GetRegistryServer(ctx context.Context, name string) (*mcpregistry.RegistryServer, error)
}

// SetRegistrySource installs the registry browse source (a project-scoped
// Memory client). Passing nil disables the registry routes.
func (s *Server) SetRegistrySource(src RegistrySource) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.registry = src
}

// registrySource returns the installed source under the server lock.
func (s *Server) registrySource() RegistrySource {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.registry
}

// registryServerResponse is the per-server lookup payload: the raw entry plus
// the suggested local config (or null when unsupported) and the declared
// env/header metadata and warnings for the client's prefill form.
type registryServerResponse struct {
	Server    mcpregistry.RegistryServer   `json:"server"`
	Suggested *serverRequest               `json:"suggested"`
	EnvVars   []mcpregistry.RegistryEnvVar `json:"envVars"`
	Headers   []mcpregistry.RegistryHeader `json:"headers"`
	Warnings  []string                     `json:"warnings"`
}

// handleRegistrySearch proxies a registry search to Memory.
func (s *Server) handleRegistrySearch(w http.ResponseWriter, r *http.Request) {
	src := s.registrySource()
	if src == nil {
		writeError(w, http.StatusServiceUnavailable, "registry unavailable: no Memory connection configured")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	result, err := src.SearchRegistry(r.Context(), r.URL.Query().Get("q"), limit, r.URL.Query().Get("cursor"))
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if result == nil {
		result = &mcpregistry.RegistrySearchResult{Servers: []mcpregistry.RegistryServer{}}
	}
	if result.Servers == nil {
		result.Servers = []mcpregistry.RegistryServer{}
	}
	writeJSON(w, http.StatusOK, result)
}

// handleRegistryServer proxies a registry lookup to Memory and returns the entry
// plus a suggested local config derived by the connector. The name arrives
// URL-decoded (the Mac app percent-encodes the slash); it is re-escaped by the
// source's Memory call.
func (s *Server) handleRegistryServer(w http.ResponseWriter, r *http.Request) {
	src := s.registrySource()
	if src == nil {
		writeError(w, http.StatusServiceUnavailable, "registry unavailable: no Memory connection configured")
		return
	}
	name := r.PathValue("name")

	entry, err := src.GetRegistryServer(r.Context(), name)
	if err != nil {
		if errors.Is(err, mcpregistry.ErrNotFound) {
			writeError(w, http.StatusNotFound, fmt.Sprintf("registry server %q not found", name))
			return
		}
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if entry == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("registry server %q not found", name))
		return
	}

	resp := registryServerResponse{
		Server:   *entry,
		EnvVars:  nonNilEnvVars(entry.EnvVars),
		Headers:  nonNilHeaders(entry.Headers),
		Warnings: []string{},
	}

	cfg, sug, err := mcpregistry.SuggestConfig(*entry, "")
	switch {
	case errors.Is(err, mcpregistry.ErrUnsupported):
		// No suggestion; leave Suggested nil. The entry + metadata still render.
	case err != nil:
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	default:
		resp.Suggested = &serverRequest{
			Name:          cfg.Name,
			Transport:     cfg.Transport,
			Enabled:       cfg.Enabled,
			Command:       cfg.Command,
			Args:          nonNilStrings(cfg.Args),
			Env:           cfg.Env,
			URL:           cfg.URL,
			Headers:       cfg.Headers,
			DisabledTools: []string{},
		}
		resp.Warnings = nonNilStrings(sug.Warnings)
	}

	writeJSON(w, http.StatusOK, resp)
}

func nonNilEnvVars(v []mcpregistry.RegistryEnvVar) []mcpregistry.RegistryEnvVar {
	if v == nil {
		return []mcpregistry.RegistryEnvVar{}
	}
	return v
}

func nonNilHeaders(v []mcpregistry.RegistryHeader) []mcpregistry.RegistryHeader {
	if v == nil {
		return []mcpregistry.RegistryHeader{}
	}
	return v
}

func nonNilStrings(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}
