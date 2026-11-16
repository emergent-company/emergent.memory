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
	CodeMarketRejected        = "market_rejected"
	CodeMarketRateLimited     = "market_rate_limited"
	CodeMarketMalformed       = "market_malformed_response"
	CodeInstallNotInstallable = "install_not_installable"
	CodeCompat                = "compat_unsatisfied"
	CodeUndeclaredTools       = "undeclared_tools"

	// In-product market client (register/verify/browse/claim/authorize) codes.
	// They are deliberately distinct from the install-flow codes above: the
	// in-product path never downloads a bundle, and telling a buyer that an
	// entitlement is already held is not the same failure as a bad install code.
	CodeRegistrationFailed   = "market_registration_failed"
	CodeTrustInvalid         = "market_trust_invalid"
	CodeClaimFailed          = "market_claim_failed"
	CodeEntitlementHeld      = "entitlement_already_held"
	CodeEntitlementInactive  = "entitlement_inactive"
	CodeBatchTooLarge        = "market_batch_too_large"
	CodeBatchExpired         = "market_batch_expired"
	CodeBatchExhausted       = "market_batch_exhausted"
	CodeAssertionInvalid     = "market_assertion_invalid"
	CodeUsageRejected        = "market_usage_rejected"
	CodeUsageRequestUnknown  = "usage_request_unknown"
	CodeUsageRequestRepeated = "usage_request_repeated"
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
// market: the request never got an answer, or the market answered 5xx. It is the
// only code that means "the market could not answer now", and it is what drives
// the retry/backoff and usage-replay paths.
func ErrMarketUnreachable(detail string) *apperror.Error {
	if detail == "" {
		detail = "market is unreachable"
	}
	return apperror.New(http.StatusBadGateway, CodeMarketUnreach, detail)
}

// ErrMarketRejected reports that the market answered and refused the request:
// a 4xx other than 408/425 (transport-shaped) and 429 (throttling, with its own
// code). A 400/401/403 is ours to fix, so it must not be dressed up as an egress
// failure or queued for retry as if the market had been down.
func ErrMarketRejected(operation string, status int) *apperror.Error {
	if operation == "" {
		operation = "request"
	}
	return apperror.New(http.StatusBadGateway, CodeMarketRejected,
		fmt.Sprintf("market rejected the %s with HTTP %d", operation, status))
}

// ErrMarketMalformedResponse reports a 2xx whose body the instance cannot use:
// invalid JSON, or a document missing the field the operation needs. It is a
// market bug, not an outage: it must stay visible and distinct from
// ErrMarketUnreachable, which drives the outage/fallback path and usage replay.
// It has its own code so dashboards can separate a broken producer from a market
// that is down, and it is deliberately not retryable on the network cadence.
func ErrMarketMalformedResponse(detail string) *apperror.Error {
	if detail == "" {
		detail = "market returned a malformed response"
	}
	return apperror.New(http.StatusBadGateway, CodeMarketMalformed, detail)
}

// isClientRejection reports whether status is the market answering "no" to a
// request it understood, as opposed to an outage. 408 (Request Timeout), 425
// (Too Early) and 429 (Too Many Requests) are excluded: a timeout and an
// "ask again shortly" are transport-shaped, and throttling has its own
// retryable code. Treating 425 as a rejection would drop a usage report that a
// retry would have accepted.
func isClientRejection(status int) bool {
	return status >= 400 && status < 500 &&
		status != http.StatusRequestTimeout &&
		status != http.StatusTooEarly &&
		status != http.StatusTooManyRequests
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

// ErrRegistrationFailed reports that the instance could not announce itself to
// the market.
func ErrRegistrationFailed(detail string) *apperror.Error {
	if detail == "" {
		detail = "market refused the instance registration"
	}
	return apperror.New(http.StatusBadGateway, CodeRegistrationFailed, detail)
}

// ErrTrustInvalid covers a listing trust document that fails signature,
// issuer, audience, expiry or subject validation.
func ErrTrustInvalid(detail string) *apperror.Error {
	if detail == "" {
		detail = "listing trust document is invalid"
	}
	return apperror.New(http.StatusBadRequest, CodeTrustInvalid, detail)
}

// ErrClaimFailed reports that the market refused an entitlement claim for a
// reason other than "already held".
func ErrClaimFailed(detail string) *apperror.Error {
	if detail == "" {
		detail = "market refused the entitlement claim"
	}
	return apperror.New(http.StatusBadGateway, CodeClaimFailed, detail)
}

// ErrEntitlementAlreadyHeld reports that this account/instance already holds
// the entitlement. The claim is idempotent in spirit, but the caller asked for
// a fresh one, so it is surfaced rather than silently swallowed.
func ErrEntitlementAlreadyHeld(listingSlug string) *apperror.Error {
	msg := "this account already holds an entitlement for the listing"
	if listingSlug != "" {
		msg = fmt.Sprintf("this account already holds an entitlement for %q", listingSlug)
	}
	return apperror.New(http.StatusConflict, CodeEntitlementHeld, msg)
}

// ErrEntitlementInactive reports that the market will not authorize calls for
// the entitlement (revoked, expired, or not held by this instance).
func ErrEntitlementInactive(detail string) *apperror.Error {
	if detail == "" {
		detail = "entitlement is not active for this instance"
	}
	return apperror.New(http.StatusForbidden, CodeEntitlementInactive, detail)
}

// ErrBatchTooLarge refuses an authorization request above the 20-call batch
// ceiling.
func ErrBatchTooLarge(calls, limit int) *apperror.Error {
	return apperror.New(http.StatusBadRequest, CodeBatchTooLarge,
		fmt.Sprintf("authorization batch requests %d calls; the market limit is %d", calls, limit))
}

// ErrBatchExpired reports that the market's authorization batch is outside its
// ≤60 s validity window, so no call may be attributed to it.
func ErrBatchExpired(detail string) *apperror.Error {
	if detail == "" {
		detail = "market authorization batch has expired"
	}
	return apperror.New(http.StatusBadRequest, CodeBatchExpired, detail)
}

// ErrBatchExhausted reports that every pre-issued request id in the cached
// batch has been used and the market cannot issue another batch right now.
func ErrBatchExhausted(detail string) *apperror.Error {
	if detail == "" {
		detail = "cached market authorization batch has no calls left"
	}
	return apperror.New(http.StatusConflict, CodeBatchExhausted, detail)
}

// ErrAssertionInvalid covers an entitlement assertion that fails signature,
// issuer, audience or expiry validation.
func ErrAssertionInvalid(detail string) *apperror.Error {
	if detail == "" {
		detail = "market assertion is invalid"
	}
	return apperror.New(http.StatusBadRequest, CodeAssertionInvalid, detail)
}

// ErrUsageRejected reports that the market refused a usage report.
func ErrUsageRejected(detail string) *apperror.Error {
	if detail == "" {
		detail = "market refused the usage report"
	}
	return apperror.New(http.StatusBadGateway, CodeUsageRejected, detail)
}

// ErrUsageRequestUnknown reports a usage report whose request_id the market
// never issued to this instance. The client refuses it before the outbound
// request, mirroring the market's fabricated-usage control.
func ErrUsageRequestUnknown(detail string) *apperror.Error {
	if detail == "" {
		detail = "usage request id was not issued by the market to this instance"
	}
	return apperror.New(http.StatusBadRequest, CodeUsageRequestUnknown, detail)
}

// ErrUsageRequestRepeated reports a second usage report for a request_id that
// was already reported or is already queued for replay.
func ErrUsageRequestRepeated(detail string) *apperror.Error {
	if detail == "" {
		detail = "usage request id has already been reported"
	}
	return apperror.New(http.StatusConflict, CodeUsageRequestRepeated, detail)
}

// ErrorCode returns the apperror code carried by err, or "".
func ErrorCode(err error) string {
	if appErr, ok := err.(*apperror.Error); ok {
		return appErr.Code
	}
	return ""
}
