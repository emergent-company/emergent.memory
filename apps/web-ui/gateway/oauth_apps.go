package main

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/labstack/echo/v4"
)

// oauthBackendFrom returns the OAuth slice of the configured Memory backend.
func (s *Server) oauthBackendFrom() (oauthBackend, bool) {
	ob, ok := s.memory.(oauthBackend)
	return ob, ok
}

// uiOAuthAuthorize renders the consent page (GET /oauth/authorize). The route
// is session-gated by authDispatch; an anonymous visitor is sent to
// /auth/login with a return_to back to this URL.
func (s *Server) uiOAuthAuthorize(c echo.Context) error {
	ob, ok := s.oauthBackendFrom()
	if !ok {
		return oauthErrorPage(c, http.StatusNotImplemented, "Connections are unavailable", "This Memory server does not support app connections.")
	}
	q := c.Request().URL.Query()
	clientID := q.Get("client_id")
	redirectURI := q.Get("redirect_uri")
	if clientID == "" || redirectURI == "" {
		return oauthErrorPage(c, http.StatusBadRequest, "Invalid authorization request", "client_id and redirect_uri are required.")
	}
	ctxResp, err := ob.OAuthAuthorizeContext(c.Request().Context(), clientID, redirectURI)
	if err != nil {
		return oauthErrorPage(c, http.StatusBadRequest, "Invalid authorization request", userFacingMemoryError(err))
	}
	if q.Get("response_type") != "code" {
		return oauthErrorPage(c, http.StatusBadRequest, "Invalid authorization request", "Only response_type=code is supported.")
	}
	method := q.Get("code_challenge_method")
	if method == "" {
		method = "S256"
	}
	if method != "S256" {
		return oauthErrorPage(c, http.StatusBadRequest, "Invalid authorization request", "code_challenge_method must be S256.")
	}
	if q.Get("code_challenge") == "" {
		return oauthErrorPage(c, http.StatusBadRequest, "Invalid authorization request", "A PKCE code_challenge is required.")
	}

	// Grant the supported subset of the requested scope. OAuth permits a partial
	// grant (RFC 6749 §3.3), and MCP clients routinely ask for scopes this server
	// does not understand (offline_access, vendor-specific ones); rejecting the
	// whole request would make the consent page unusable for them. The
	// all-supported fallback applies only when the client asked for no scope at
	// all: a request that named only unsupported scopes must not widen to
	// everything.
	requested := strings.Fields(q.Get("scope"))
	scopes := clampScopes(requested, ctxResp.ScopesSupported)
	if len(requested) == 0 {
		scopes = ctxResp.ScopesSupported
	}
	if len(scopes) == 0 {
		return oauthErrorPage(c, http.StatusBadRequest, "Invalid authorization request", "None of the requested scopes are supported by this server.")
	}
	scope := strings.Join(scopes, " ")

	data := oauthConsentData{
		ClientID:            ctxResp.ClientID,
		ClientName:          ctxResp.ClientName,
		RedirectURI:         ctxResp.RedirectURI,
		State:               q.Get("state"),
		CodeChallenge:       q.Get("code_challenge"),
		CodeChallengeMethod: method,
		Scope:               scope,
		Scopes:              scopes,
		Projects:            ctxResp.Projects,
	}
	return s.page(c, pageTitle("Authorize "+data.ClientName), OAuthConsentPage(data))
}

// uiOAuthAuthorizeDecision posts the consent decision and redirects back to the
// requesting app with either a one-time code or an access_denied error.
func (s *Server) uiOAuthAuthorizeDecision(c echo.Context) error {
	ob, ok := s.oauthBackendFrom()
	if !ok {
		return oauthErrorPage(c, http.StatusNotImplemented, "Connections are unavailable", "This Memory server does not support app connections.")
	}
	clientID := c.FormValue("client_id")
	redirectURI := c.FormValue("redirect_uri")
	state := c.FormValue("state")
	decision := c.FormValue("decision")
	challenge := c.FormValue("code_challenge")
	method := c.FormValue("code_challenge_method")
	if method == "" {
		method = "S256"
	}
	if clientID == "" || redirectURI == "" {
		return oauthErrorPage(c, http.StatusBadRequest, "Invalid authorization request", "client_id and redirect_uri are required.")
	}

	// Re-validate client + redirect URI before any redirect so a tampered POST
	// cannot turn this endpoint into an open redirect.
	ctxResp, err := ob.OAuthAuthorizeContext(c.Request().Context(), clientID, redirectURI)
	if err != nil {
		return oauthErrorPage(c, http.StatusBadRequest, "Invalid authorization request", userFacingMemoryError(err))
	}

	if decision == "deny" {
		u, err := url.Parse(ctxResp.RedirectURI)
		if err != nil {
			return oauthErrorPage(c, http.StatusBadRequest, "Invalid redirect URI", "The app's redirect URI is invalid.")
		}
		q := u.Query()
		q.Set("error", "access_denied")
		q.Set("error_description", "The user declined the request.")
		if state != "" {
			q.Set("state", state)
		}
		u.RawQuery = q.Encode()
		return c.Redirect(http.StatusFound, u.String())
	}

	// Same partial grant as the GET: keep the supported scopes, drop the rest,
	// and only fall back to all supported scopes when the form carried none.
	requested := strings.Fields(c.FormValue("scope"))
	scopes := clampScopes(requested, ctxResp.ScopesSupported)
	if len(requested) == 0 {
		scopes = ctxResp.ScopesSupported
	}
	if len(scopes) == 0 {
		return oauthErrorPage(c, http.StatusBadRequest, "Invalid authorization request", "None of the requested scopes are supported by this server.")
	}
	projectID := c.FormValue("project_id")
	if projectID == "" {
		return oauthErrorPage(c, http.StatusBadRequest, "Choose a project", "Select a project before authorizing the app.")
	}
	if challenge == "" || method != "S256" {
		return oauthErrorPage(c, http.StatusBadRequest, "Invalid authorization request", "A PKCE S256 code_challenge is required.")
	}

	res, err := ob.OAuthAuthorize(c.Request().Context(), OAuthAuthorizeRequest{
		ClientID:            clientID,
		RedirectURI:         ctxResp.RedirectURI,
		ProjectID:           projectID,
		Scopes:              scopes,
		State:               state,
		CodeChallenge:       challenge,
		CodeChallengeMethod: method,
	})
	if err != nil {
		return oauthErrorPage(c, http.StatusBadRequest, "Authorization failed", userFacingMemoryError(err))
	}
	return c.Redirect(http.StatusFound, res.RedirectURL)
}

// clampScopes keeps the supported scopes from a client's request, preserving
// order and de-duplicating. Anything the server does not understand is dropped
// rather than rejected.
func clampScopes(want, supported []string) []string {
	allowed := make(map[string]bool, len(supported))
	for _, s := range supported {
		allowed[s] = true
	}
	out := make([]string, 0, len(want))
	seen := make(map[string]bool, len(want))
	for _, s := range want {
		if !allowed[s] || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// uiConnectedApps lists the signed-in user's grants across projects.
func (s *Server) uiConnectedApps(c echo.Context) error {
	ob, ok := s.oauthBackendFrom()
	if !ok {
		return s.page(c, pageTitle("Connected apps"), ConnectedAppsPage(connectedAppsData{LoadErr: "This Memory server does not support connected apps."}))
	}
	data := connectedAppsData{ProvidersMissing: s.projectHasNoProviders(c.Request().Context())}
	if c.QueryParam("revoked") != "" {
		data.Flash = "Access revoked."
	}
	if err := flashError(c); err != nil {
		data.FlashErr = err.Error()
	}
	grants, err := ob.ListOAuthGrants(c.Request().Context())
	if err != nil {
		data.LoadErr = userFacingMemoryError(err)
	} else {
		data.Grants = grants
	}
	return s.page(c, pageTitle("Connected apps"), ConnectedAppsPage(data))
}

// uiRevokeConnectedApp revokes one grant and redirects back to the list.
func (s *Server) uiRevokeConnectedApp(c echo.Context) error {
	ob, ok := s.oauthBackendFrom()
	if !ok {
		return redirectWithError(c, "/settings/connected-apps", errors.New("connected apps are unavailable"))
	}
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		return redirectWithError(c, "/settings/connected-apps", errors.New("missing grant id"))
	}
	if err := ob.RevokeOAuthGrant(c.Request().Context(), id); err != nil {
		return redirectWithError(c, "/settings/connected-apps", userFacingError(err))
	}
	return c.Redirect(http.StatusSeeOther, "/settings/connected-apps?revoked=1")
}

// oauthErrorPage renders a small standalone error page for authorize failures
// (where there is no trusted redirect URI to send the user back to).
func oauthErrorPage(c echo.Context, status int, title, msg string) error {
	body := "<!doctype html><html><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width,initial-scale=1\">" +
		"<title>" + htmlEscape(title) + "</title>" +
		"<style>body{font-family:ui-sans-serif,system-ui,sans-serif;max-width:32rem;margin:12vh auto;padding:0 1.5rem;color:#e7e9ee;background:#171921}" +
		"h1{font-size:1.25rem;margin:0 0 .75rem}p{color:#a7adba;line-height:1.6}a{color:#d8b26a}</style></head><body>" +
		"<h1>" + htmlEscape(title) + "</h1><p>" + htmlEscape(msg) + "</p>" +
		"<p><a href=\"/settings/connected-apps\">Connected apps</a> · <a href=\"/\">Back to Memory</a></p></body></html>"
	return c.HTML(status, body)
}
