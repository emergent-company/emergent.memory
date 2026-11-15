package market

import (
	"fmt"
	"net/http"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// Named instance-side error codes from the install-flow contract.
const (
	CodeIssuerUntrusted       = "market_issuer_untrusted"
	CodeInstallCode           = "install_code_invalid"
	CodeLicenseInvalid        = "license_invalid"
	CodeDigestMismatch        = "digest_mismatch"
	CodeMarketUnreach         = "market_unreachable"
	CodeMarketRateLimited     = "market_rate_limited"
	CodeInstallNotInstallable = "install_not_installable"
	CodeCompat                = "compat_unsatisfied"
	CodeUndeclaredTools       = "undeclared_tools"
)

// ErrIssuerUntrusted refuses an install whose market origin is not in the
// operator's allowlist. It is returned BEFORE any outbound request.
func ErrIssuerUntrusted(issuer string) *apperror.Error {
	return apperror.New(http.StatusForbidden, CodeIssuerUntrusted,
		fmt.Sprintf("market issuer %q is not trusted by this instance", issuer))
}

// ErrInstallCodeInvalid covers an unknown, expired, reused or otherwise
// unusable install code.
func ErrInstallCodeInvalid(detail string) *apperror.Error {
	if detail == "" {
		detail = "install code is invalid or expired"
	}
	return apperror.New(http.StatusBadRequest, CodeInstallCode, detail)
}

// ErrMarketRateLimited reports that the market is throttling installs. It is
// distinct from an invalid code: retrying later may succeed.
func ErrMarketRateLimited(detail string) *apperror.Error {
	if detail == "" {
		detail = "market is rate limiting installs; try again shortly"
	}
	return apperror.New(http.StatusTooManyRequests, CodeMarketRateLimited, detail)
}

// ErrInstallNotInstallable reports that the listing exists but cannot be
// installed here (for example it is withdrawn or not install-delivery). It is
// distinct from an unusable code.
func ErrInstallNotInstallable(detail string) *apperror.Error {
	if detail == "" {
		detail = "listing is not installable on this instance"
	}
	return apperror.New(http.StatusConflict, CodeInstallNotInstallable, detail)
}

// ErrLicenseInvalid covers a license that fails signature, issuer, audience,
// expiry or digest validation.
func ErrLicenseInvalid(detail string) *apperror.Error {
	if detail == "" {
		detail = "install license is invalid"
	}
	return apperror.New(http.StatusBadRequest, CodeLicenseInvalid, detail)
}

// ErrDigestMismatch reports that downloaded bytes do not match the licensed
// archive digest.
func ErrDigestMismatch(expected, actual string) *apperror.Error {
	msg := "downloaded archive does not match the licensed digest"
	if expected != "" && actual != "" {
		msg = fmt.Sprintf("downloaded archive digest mismatch: expected %s, got %s", expected, actual)
	}
	return apperror.New(http.StatusBadRequest, CodeDigestMismatch, msg)
}

// ErrMarketUnreachable reports a transport or upstream failure talking to the
// market.
func ErrMarketUnreachable(detail string) *apperror.Error {
	if detail == "" {
		detail = "market is unreachable"
	}
	return apperror.New(http.StatusBadGateway, CodeMarketUnreach, detail)
}

// ErrCompatUnsatisfied reports a listing whose compat.memory range does not
// include the running instance version.
func ErrCompatUnsatisfied(constraint, version string) *apperror.Error {
	return apperror.New(http.StatusConflict, CodeCompat,
		fmt.Sprintf("listing requires memory %s; this instance is %s", constraint, version))
}

// ErrUndeclaredTools refuses an install whose blueprint declares tools the
// listing did not licence for install delivery.
func ErrUndeclaredTools(tools []string) *apperror.Error {
	msg := "blueprint declares tools the listing does not licence for install delivery"
	if len(tools) > 0 {
		msg = fmt.Sprintf("%s: %v", msg, tools)
	}
	return apperror.New(http.StatusConflict, CodeUndeclaredTools, msg)
}

// ErrorCode returns the apperror code carried by err, or "".
func ErrorCode(err error) string {
	if appErr, ok := err.(*apperror.Error); ok {
		return appErr.Code
	}
	return ""
}
