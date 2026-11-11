package main

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/emergent-company/go-daisy/render"
	"github.com/labstack/echo/v4"
)

const (
	deploySessionCookie = "memory_deploy_session"
	deployStateCookie   = "memory_deploy_state"
	deployCookiePath    = "/deploy"
)

type deployToken struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type deployState struct {
	State     string    `json:"state"`
	Verifier  string    `json:"verifier"`
	ExpiresAt time.Time `json:"expires_at"`
}

type deployWorkspace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type deployPortalData struct {
	Configured bool
	SignedIn   bool
	Error      string
	Email      string
	Workspaces []deployWorkspace
	ExpiresAt  time.Time
}

func (s *Server) deployPortalEnabled() bool {
	return s.cfg.DeployPortalEnabled
}

func (s *Server) deployPortalConfigured() bool {
	return s.cfg.RailwayOAuthClientID != "" && s.cfg.RailwayOAuthClientSecret != "" &&
		s.cfg.RailwayOAuthRedirectURI != "" && len(s.cfg.DeploySessionSecret) >= 32
}

func (s *Server) deployCookieSecure() bool {
	return strings.HasPrefix(s.cfg.PublicBaseURL, "https://")
}

func (s *Server) deploySeal(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	key := sha256.Sum256([]byte(s.cfg.DeploySessionSecret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, raw, nil)
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

func (s *Server) deployOpen(value string, v any) error {
	sealed, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return err
	}
	key := sha256.Sum256([]byte(s.cfg.DeploySessionSecret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	if len(sealed) < gcm.NonceSize() {
		return errors.New("short sealed value")
	}
	raw, err := gcm.Open(nil, sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():], nil)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, v)
}

func (s *Server) deployGetToken(c echo.Context) (*deployToken, error) {
	cookie, err := c.Cookie(deploySessionCookie)
	if err != nil || cookie.Value == "" {
		return nil, nil
	}
	var tok deployToken
	if err := s.deployOpen(cookie.Value, &tok); err != nil {
		return nil, err
	}
	if tok.ExpiresAt.After(time.Now().Add(30 * time.Second)) {
		return &tok, nil
	}
	if tok.RefreshToken == "" {
		return nil, errors.New("expired")
	}
	refreshed, err := s.deployRefresh(c.Request().Context(), tok.RefreshToken)
	if err != nil {
		return nil, err
	}
	s.deploySetSession(c, refreshed)
	return refreshed, nil
}

func (s *Server) deploySetSession(c echo.Context, tok *deployToken) {
	sealed, err := s.deploySeal(tok)
	if err != nil {
		return
	}
	maxAge := int(time.Until(tok.ExpiresAt).Seconds())
	if maxAge < 60 {
		maxAge = 60
	}
	c.SetCookie(&http.Cookie{
		Name:     deploySessionCookie,
		Value:    sealed,
		Path:     deployCookiePath,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   s.deployCookieSecure(),
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) deployClearSession(c echo.Context) {
	c.SetCookie(&http.Cookie{
		Name:     deploySessionCookie,
		Value:    "",
		Path:     deployCookiePath,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.deployCookieSecure(),
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) deployOAuthStart(c echo.Context) error {
	if !s.deployPortalEnabled() || !s.deployPortalConfigured() {
		return echo.ErrNotFound
	}
	stateBytes := make([]byte, 32)
	verifierBytes := make([]byte, 32)
	if _, err := rand.Read(stateBytes); err != nil {
		return err
	}
	if _, err := rand.Read(verifierBytes); err != nil {
		return err
	}
	state := base64.RawURLEncoding.EncodeToString(stateBytes)
	verifier := base64.RawURLEncoding.EncodeToString(verifierBytes)
	challenge := sha256.Sum256([]byte(verifier))
	sealed, err := s.deploySeal(deployState{
		State:     state,
		Verifier:  verifier,
		ExpiresAt: time.Now().Add(10 * time.Minute),
	})
	if err != nil {
		return err
	}
	c.SetCookie(&http.Cookie{
		Name:     deployStateCookie,
		Value:    sealed,
		Path:     deployCookiePath,
		MaxAge:   600,
		HttpOnly: true,
		Secure:   s.deployCookieSecure(),
		SameSite: http.SameSiteLaxMode,
	})

	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", s.cfg.RailwayOAuthClientID)
	q.Set("redirect_uri", s.cfg.RailwayOAuthRedirectURI)
	q.Set("scope", strings.Join(s.cfg.RailwayOAuthScopes, " "))
	q.Set("state", state)
	q.Set("code_challenge", base64.RawURLEncoding.EncodeToString(challenge[:]))
	q.Set("code_challenge_method", "S256")
	q.Set("prompt", "consent")
	return c.Redirect(http.StatusFound, s.cfg.RailwayOAuthAuthorizeURL+"?"+q.Encode())
}

func (s *Server) deployOAuthCallback(c echo.Context) error {
	if !s.deployPortalEnabled() || !s.deployPortalConfigured() {
		return echo.ErrNotFound
	}
	if errCode := c.QueryParam("error"); errCode != "" {
		c.SetCookie(&http.Cookie{Name: deployStateCookie, Value: "", Path: deployCookiePath, MaxAge: -1, HttpOnly: true, Secure: s.deployCookieSecure(), SameSite: http.SameSiteLaxMode})
		return c.Redirect(http.StatusFound, "/deploy?error="+url.QueryEscape(errCode))
	}
	code := c.QueryParam("code")
	if code == "" {
		return c.Redirect(http.StatusFound, "/deploy?error=missing_code")
	}
	cookie, err := c.Cookie(deployStateCookie)
	if err != nil || cookie.Value == "" {
		return c.Redirect(http.StatusFound, "/deploy?error=missing_state")
	}
	var st deployState
	if err := s.deployOpen(cookie.Value, &st); err != nil || st.ExpiresAt.Before(time.Now()) {
		return c.Redirect(http.StatusFound, "/deploy?error=invalid_state")
	}
	if c.QueryParam("state") != st.State {
		return c.Redirect(http.StatusFound, "/deploy?error=state_mismatch")
	}
	// One-shot state: clear before the exchange.
	c.SetCookie(&http.Cookie{Name: deployStateCookie, Value: "", Path: deployCookiePath, MaxAge: -1, HttpOnly: true, Secure: s.deployCookieSecure(), SameSite: http.SameSiteLaxMode})

	tok, err := s.deployExchange(c.Request().Context(), code, st.Verifier)
	if err != nil {
		return c.Redirect(http.StatusFound, "/deploy?error=exchange_failed")
	}
	s.deploySetSession(c, tok)
	return c.Redirect(http.StatusFound, "/deploy")
}

func (s *Server) deployExchange(ctx context.Context, code, verifier string) (*deployToken, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", s.cfg.RailwayOAuthRedirectURI)
	form.Set("client_id", s.cfg.RailwayOAuthClientID)
	form.Set("client_secret", s.cfg.RailwayOAuthClientSecret)
	form.Set("code_verifier", verifier)
	return s.deployTokenRequest(ctx, form)
}

func (s *Server) deployRefresh(ctx context.Context, refresh string) (*deployToken, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refresh)
	form.Set("client_id", s.cfg.RailwayOAuthClientID)
	form.Set("client_secret", s.cfg.RailwayOAuthClientSecret)
	return s.deployTokenRequest(ctx, form)
}

func (s *Server) deployTokenRequest(ctx context.Context, form url.Values) (*deployToken, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.RailwayOAuthTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("railway token endpoint returned %d", resp.StatusCode)
	}
	var raw struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	if raw.AccessToken == "" {
		return nil, errors.New("railway token response has no access_token")
	}
	expires := time.Now().Add(time.Duration(raw.ExpiresIn) * time.Second)
	if raw.ExpiresIn <= 0 {
		expires = time.Now().Add(time.Hour)
	}
	return &deployToken{AccessToken: raw.AccessToken, RefreshToken: raw.RefreshToken, ExpiresAt: expires}, nil
}

func (s *Server) deployWorkspaces(ctx context.Context, accessToken string) ([]deployWorkspace, error) {
	payload := map[string]any{"query": "query { me { workspaces { id name } } }"}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.RailwayGraphQLURL, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, errors.New("railway token rejected")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("railway graphql returned %d", resp.StatusCode)
	}
	var out struct {
		Data struct {
			Me struct {
				Workspaces []deployWorkspace `json:"workspaces"`
			} `json:"me"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	if len(out.Errors) > 0 {
		return nil, errors.New(out.Errors[0].Message)
	}
	return out.Data.Me.Workspaces, nil
}

func (s *Server) deployOAuthLogout(c echo.Context) error {
	s.deployClearSession(c)
	return c.Redirect(http.StatusFound, "/deploy")
}

func (s *Server) deployPortalPage(c echo.Context) error {
	if !s.deployPortalEnabled() {
		return echo.ErrNotFound
	}
	data := deployPortalData{Configured: s.deployPortalConfigured()}
	if q := c.QueryParam("error"); q != "" {
		data.Error = "Railway sign-in did not complete (" + q + "). Please try again."
	}
	tok, err := s.deployGetToken(c)
	if err != nil {
		s.deployClearSession(c)
	}
	if tok != nil {
		workspaces, wErr := s.deployWorkspaces(c.Request().Context(), tok.AccessToken)
		if wErr != nil {
			s.deployClearSession(c)
			data.Error = "Railway session could not be read. Please connect again."
		} else {
			data.SignedIn = true
			data.Workspaces = workspaces
			data.ExpiresAt = tok.ExpiresAt
		}
	}
	render.RenderPage(c.Response().Writer, c.Request(), DeployPortalPage(data))
	return nil
}
