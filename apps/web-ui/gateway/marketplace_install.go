package main

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/labstack/echo/v4"
)

// Named instance-side error codes from the install-flow contract. They mirror
// the server's market error codes (apps/server/domain/market/errors.go) and are
// the `data-error-code` the review page renders.
const (
	marketErrIssuerUntrusted = "market_issuer_untrusted"
	marketErrInstallCode     = "install_code_invalid"
	marketErrLicenseInvalid  = "license_invalid"
	marketErrDigestMismatch  = "digest_mismatch"
	marketErrMarketUnreach   = "market_unreachable"
	marketErrCompat          = "compat_unsatisfied"
	marketErrUndeclaredTools = "undeclared_tools"
)

// marketInstallErrorMessages maps a contract error code to the message the
// review page shows. Named codes get stable copy; an unknown code falls back to
// the backend's own message, then to a generic line.
var marketInstallErrorMessages = map[string]string{
	marketErrIssuerUntrusted: "This marketplace is not trusted by this instance. Nothing was downloaded.",
	marketErrInstallCode:     "This install link is invalid, expired, or already used. Start the install again from the marketplace.",
	marketErrLicenseInvalid:  "The install licence is invalid, so nothing was installed.",
	marketErrDigestMismatch:  "The downloaded package does not match its licensed digest. Nothing was installed.",
	marketErrMarketUnreach:   "The marketplace could not be reached. Try again in a moment.",
	marketErrCompat:          "This listing is not compatible with this Memory version.",
	marketErrUndeclaredTools: "The blueprint declares tools the listing does not licence for install delivery.",
}

// marketInstallErrorCode returns the contract error code carried by a backend
// error. A transport failure (no memory HTTP error) is reported as
// market_unreachable.
func marketInstallErrorCode(err error) string {
	var he *memoryHTTPError
	if errors.As(err, &he) && he.Code != "" {
		return he.Code
	}
	return marketErrMarketUnreach
}

// marketInstallErrorMessage resolves the copy for one error code.
func marketInstallErrorMessage(code string, err error) string {
	if msg, ok := marketInstallErrorMessages[code]; ok {
		return msg
	}
	if msg := memoryHTTPErrorMessage(err); msg != "" {
		return msg
	}
	return "The install could not be completed."
}

// marketTrustedIssuer normalizes market and reports whether it is in the
// operator's MEMORY_MARKET_TRUSTED_ISSUERS allowlist. This check runs before
// any outbound call, so an untrusted origin can never make the gateway or the
// server fetch it.
func (s *Server) marketTrustedIssuer(market string) (string, bool) {
	normalized, err := normalizeMarketIssuer(market)
	if err != nil {
		return "", false
	}
	for _, issuer := range s.cfg.MarketTrustedIssuers {
		if issuer == normalized {
			return normalized, true
		}
	}
	return "", false
}

// marketBackLink chooses the "back to the marketplace" destination: the
// configured primary market when there is one, else the requested market only
// when it is trusted. Empty means the page omits the link.
func (s *Server) marketBackLink(market string) string {
	if s.cfg.MarketURL != "" {
		return s.cfg.MarketURL
	}
	if _, ok := s.marketTrustedIssuer(market); ok {
		return market
	}
	return ""
}

// marketInstallReviewData builds the review page data from a resolve result.
func marketInstallReviewData(market, code, link string, res *MarketResolveResult) MarketInstallPageData {
	data := MarketInstallPageData{Market: market, Code: code, MarketLink: link}
	if res != nil {
		data.Listing = res.Listing
		data.Capabilities = res.Capabilities
		data.Compat = res.Compat
		data.AlreadyInstalled = res.AlreadyInstalled
		data.HasReview = true
	}
	return data
}

// marketInstallErrorData builds a named-error page payload.
func marketInstallErrorData(market, code, link, errorCode string) MarketInstallPageData {
	return MarketInstallPageData{
		Market:     market,
		Code:       code,
		MarketLink: link,
		ErrorCode:  errorCode,
		ErrorMsg:   marketInstallErrorMessage(errorCode, nil),
	}
}

// renderMarketInstallPage renders the review/error page.
func (s *Server) renderMarketInstallPage(c echo.Context, data MarketInstallPageData) error {
	return s.page(c, pageTitle("Install from Marketplace"), MarketInstallPage(data))
}

// renderMarketInstallError renders an error page for a backend failure, keeping
// any review context the caller already resolved.
func (s *Server) renderMarketInstallError(c echo.Context, data MarketInstallPageData, err error) error {
	data.ErrorCode = marketInstallErrorCode(err)
	data.ErrorMsg = marketInstallErrorMessage(data.ErrorCode, err)
	return s.renderMarketInstallPage(c, data)
}

// uiMarketInstall is the review step: GET /marketplace/install?code=…&market=….
// It validates the market allowlist BEFORE any outbound call, refuses to
// install without an active project (the picker owns project selection), then
// resolves the code into a side-effect-free review payload.
func (s *Server) uiMarketInstall(c echo.Context) error {
	market := strings.TrimSpace(c.QueryParam("market"))
	code := strings.TrimSpace(c.QueryParam("code"))
	link := s.marketBackLink(market)

	normalized, trusted := s.marketTrustedIssuer(market)
	if !trusted {
		return s.renderMarketInstallPage(c, marketInstallErrorData(market, code, link, marketErrIssuerUntrusted))
	}
	// Missing active project: land on the project picker rather than resolving
	// against a deployment default or installing globally.
	projectID := activeProjectIDFromContext(c.Request().Context(), s.cfg.MemoryProjectID)
	if projectID == "" {
		return c.Redirect(http.StatusSeeOther, "/orgs")
	}
	if code == "" {
		return s.renderMarketInstallPage(c, marketInstallErrorData(market, code, link, marketErrInstallCode))
	}

	res, err := s.memory.ResolveMarketInstall(c.Request().Context(), normalized, code)
	if err != nil {
		return s.renderMarketInstallError(c, MarketInstallPageData{Market: market, Code: code, MarketLink: link}, err)
	}
	return s.renderMarketInstallPage(c, marketInstallReviewData(market, code, link, res))
}

// uiMarketInstallConfirm is the confirm step: POST /marketplace/install. It
// applies into the active project and redirects to the installed blueprint
// detail; a failure re-renders the review with a named error (re-resolving the
// listing best-effort so the user still sees what they were installing).
func (s *Server) uiMarketInstallConfirm(c echo.Context) error {
	market := strings.TrimSpace(c.FormValue("market"))
	code := strings.TrimSpace(c.FormValue("code"))
	link := s.marketBackLink(market)

	normalized, trusted := s.marketTrustedIssuer(market)
	if !trusted {
		return s.renderMarketInstallPage(c, marketInstallErrorData(market, code, link, marketErrIssuerUntrusted))
	}
	projectID := activeProjectIDFromContext(c.Request().Context(), s.cfg.MemoryProjectID)
	if projectID == "" {
		return c.Redirect(http.StatusSeeOther, "/orgs")
	}
	if code == "" {
		return s.renderMarketInstallPage(c, marketInstallErrorData(market, code, link, marketErrInstallCode))
	}

	ctx := c.Request().Context()
	res, err := s.memory.ApplyMarketInstall(ctx, normalized, code, projectID, nil)
	if err != nil {
		data := MarketInstallPageData{Market: market, Code: code, MarketLink: link}
		// Best-effort: a failed apply may still have a resolvable listing, and
		// showing the review beside the error is more useful than a bare alert.
		if review, rerr := s.memory.ResolveMarketInstall(ctx, normalized, code); rerr == nil {
			data = marketInstallReviewData(market, code, link, review)
		}
		return s.renderMarketInstallError(c, data, err)
	}
	if res == nil || strings.TrimSpace(res.BlueprintID) == "" {
		return s.renderMarketInstallError(c, MarketInstallPageData{Market: market, Code: code, MarketLink: link},
			errors.New("market install returned no blueprint"))
	}
	return c.Redirect(http.StatusSeeOther, "/blueprints/"+url.PathEscape(res.BlueprintID))
}
